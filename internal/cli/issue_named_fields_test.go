package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	cataloguePath   = "/api/admin/customFieldSettings/customFields"
	catalogueFields = "name,localizedName"
)

type cataloguedField struct {
	name      string
	translate string
}

func (f cataloguedField) sent() string {
	translated := "null"
	if f.translate != "" {
		translated = strconv.Quote(f.translate)
	}
	return `{"$type":"CustomField","name":` + strconv.Quote(f.name) + `,"localizedName":` + translated + `}`
}

func catalogueOf(fields ...cataloguedField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func devCatalogue() string {
	return catalogueOf(
		cataloguedField{name: "Priority", translate: "Приоритет"},
		cataloguedField{name: "Type", translate: "Тип"},
		cataloguedField{name: "State", translate: "Состояние"},
		cataloguedField{name: "Статус разработки"},
		cataloguedField{name: "Модуль системы"},
	)
}

func serveNamedFields(t *testing.T, catalogue string, issue http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, cataloguePath) {
			fake.JSON(http.StatusOK, catalogue)(w, r)
			return
		}
		issue(w, r)
	})
}

func catalogueRequest(address string) string {
	return "GET " + address + cataloguePath + "?fields=" + catalogueFields + "&$top=-1"
}

func TestIssueShowRefusesANameNoCustomFieldOfTheInstanceAnswersTo(t *testing.T) {
	t.Parallel()
	catalogue := catalogueOf(cataloguedField{name: "Named", translate: "Translated"}, cataloguedField{name: "Other"})
	server := serveNamedFields(t, catalogue, func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "the issue was asked for", "%s %s", r.Method, r.URL)
	})

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", `customFields("Nmed",Othr)`)

	assert.Equal(t, faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", catalogueRequest(server.URL)},
			{"fields", `customFields("Nmed",Othr)`},
			{"unknown", []any{
				[]detail{{"field", `customFields("Nmed")`}, {"nearest", []any{"Named"}}},
				[]detail{{"field", "customFields(Othr)"}, {"nearest", []any{"Other"}}},
			}},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{cataloguePath}, server.Paths())
}

func TestIssueShowRefusesTheNamesTheCatalogueIsClosedTo(t *testing.T) {
	t.Parallel()
	body := `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		require.True(t, strings.HasPrefix(r.URL.Path, cataloguePath), "the issue was asked for")
		fake.JSON(http.StatusForbidden, body)(w, r)
	})

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields(State)")

	assert.Equal(t, faultDocument{
		code: "denied",
		details: []detail{
			{"request", catalogueRequest(server.URL)},
			{"upstream_status", 403},
			{"upstream_error", "Forbidden"},
			{"upstream_message", "HTTP 403 Forbidden"},
			authFromEnv(),
		},
	}, requireFault(t, got))
	assert.Len(t, server.Requests(), 1)
}
