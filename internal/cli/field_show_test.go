package cli_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	metadataSent  = "customFields(id,field(name,localizedName,fieldType(valueType,isMultiValue)))"
	enumFieldSent = fieldListDefault + ",bundle(values(name,archived))"
)

const printedEnumType = `field:
  name: "Type"
  localizedName: "Kind"
  fieldType:
    valueType: "enum"
    isMultiValue: false
canBeEmpty: false
bundle:
  values: []
`

func metadataRequest(address, project string) string {
	return "GET " + address + "/api/admin/projects/" + project + "?fields=" + metadataSent
}

func fieldRequest(address, project, id, fields string) string {
	return "GET " + address + "/api/admin/projects/" + project + "/customFields/" + id + "?fields=" + fields
}

func localizedNameOrNull(localized string) string {
	if localized == "" {
		return "null"
	}
	return strconv.Quote(localized)
}

func projectField(id, name, localized string) string {
	return fmt.Sprintf(`{"$type":"EnumProjectCustomField","id":%q,"field":{"$type":"CustomField","name":%q,`+
		`"localizedName":%s,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}`,
		id, name, localizedNameOrNull(localized))
}

func projectMetadata(fields ...string) string {
	return `{"$type":"Project","customFields":[` + strings.Join(fields, ",") + `]}`
}

func oneField(name, localized string, canBeEmpty bool) string {
	return fmt.Sprintf(`{"$type":"EnumProjectCustomField","field":{"$type":"CustomField","name":%q,`+
		`"localizedName":%s,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}},`+
		`"canBeEmpty":%t,"bundle":{"$type":"EnumBundle","values":[]}}`, name, localizedNameOrNull(localized), canBeEmpty)
}

func serveTheProject(t *testing.T, metadata string, field http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/customFields/") {
			field(w, r)
			return
		}
		fake.JSON(http.StatusOK, metadata)(w, r)
	})
}

func noField(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request for one field reached the server", "%s %s", r.Method, r.URL)
	}
}

func TestFieldShowRefusesTheEmptyName(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "field", "show", "DEV", "")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestFieldShowPrintsTheFieldItsNameResolvesTo(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("180-1", "Type", "Kind"))
	server := serveTheProject(t, metadata, fake.JSON(http.StatusOK, oneField("Type", "Kind", false)))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: printedEnumType}, got)
	assert.Equal(t, []string{
		"/api/admin/projects/DEV?fields=" + metadataSent,
		"/api/admin/projects/DEV/customFields/180-1?fields=" + enumFieldSent,
	}, server.Targets())
}

func TestFieldShowRefusesANameOfNoField(t *testing.T) {
	t.Parallel()
	server := serveTheProject(t, projectMetadata(projectField("180-1", "Type", "Kind")), noField(t))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Nothing")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", metadataRequest(server.URL, "DEV")},
			{"project", "DEV"},
			{"unknown", []any{unknownEntry("Nothing", "Type")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}

func TestFieldShowRefusesAnIdItCannotAddress(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("..", "Type", "Kind"))
	server := serveTheProject(t, metadata, noField(t))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", metadataRequest(server.URL, "DEV")},
			{"upstream_status", 200},
			{"upstream_body", metadata},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 1)
}

func TestFieldShowRefusesAFieldGoneBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	const gone = `{"error":"Not Found","error_description":"Entity with id 180-1 not found"}`
	metadata := projectMetadata(projectField("180-1", "Type", "Kind"))
	server := serveTheProject(t, metadata, fake.JSON(http.StatusNotFound, gone))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", fieldRequest(server.URL, "DEV", "180-1", enumFieldSent)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id 180-1 not found"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 2)
}

func TestFieldShowRefusesAFieldRenamedBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	renamed := oneField("Renamed", "Kind", false)
	server := serveTheProject(t, projectMetadata(projectField("180-1", "Type", "Kind")), fake.JSON(http.StatusOK, renamed))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "upstream_failed",
		details: []detail{
			{"request", fieldRequest(server.URL, "DEV", "180-1", enumFieldSent)},
			{"upstream_status", 200},
			{"project", "DEV"},
			{"field", "Type"},
			{"upstream_body", renamed},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}

func TestFieldShowRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := serveTheProject(t, projectMetadata(), noField(t))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", metadataRequest(server.URL, "DEV")},
			{"project", "DEV"},
			{"permission", "jetbrains.jetpass.project-read"},
			authFromEnv(),
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}
