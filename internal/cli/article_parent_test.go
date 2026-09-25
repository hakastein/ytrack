package cli_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const articleToWriteFields = "id,idReadable,project(shortName)"

func articleToWriteRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + articleToWriteFields
}

func articleOfDEVToWrite(id, readable string) string {
	return `{"$type":"Article","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":"DEV"}}`
}

func filingUnderAParent(t *testing.T, read, creation http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			read(w, r)
			return
		}
		if !assert.Equal(t, http.MethodPost, r.Method, "a creation under a parent sends one GET and one POST") {
			return
		}
		creation(w, r)
	})
}

func TestArticleCreateRefusesAParentOfAnyOtherFormBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the id of an issue", argv: []string{"--parent", "DEV-1"}},
		{name: "an internal id", argv: []string{"--parent", "177-1"}},
		{name: "the marker in lower case", argv: []string{"--parent", "DEV-a-1"}},
		{name: "no id at all", argv: []string{"--parent", ""}},
		{name: "a parent twice", argv: []string{"--parent", "DEV-A-1", "--parent", "DEV-A-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"article", "create", "DEV", "--summary", "x"}, tc.argv...)...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleCreateRefusesAParentTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := filingUnderAParent(t, fake.JSON(http.StatusNotFound, said), noCreation(t))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-99999")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleToWriteRequest(server.URL, "DEV-A-99999")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Can't find article with id DEV-A-99999"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestArticleCreateRefusesAParentLeftEmptyInTheResponse(t *testing.T) {
	t.Parallel()
	const found = `{"$type":"Article","id":null,"idReadable":"DEV-A-1","project":{"$type":"Project","shortName":"DEV"}}`
	server := filingUnderAParent(t, fake.JSON(http.StatusOK, found), noCreation(t))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "Title", "--parent", "DEV-A-1")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleToWriteRequest(server.URL, "DEV-A-1")},
			{"upstream_status", 200},
			{"upstream_body", found},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}
