package cli_test

import (
	"cmp"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const projectWriteFields = "id,shortName,customFields(id,canBeEmpty,defaultValues(name)," +
	"condition($type,showForNullValue,field(id),values(name))," +
	"field(name,localizedName,fieldType(valueType,isMultiValue)))"

func withoutDefaults() []string {
	return []string{"PeriodProjectCustomField", "SimpleProjectCustomField", "TextProjectCustomField"}
}

type writableField struct {
	id           string
	kind         string
	name         string
	translate    string
	valueType    string
	isMultiValue bool
	canBeEmpty   bool
	defaults     []string
	condition    string
}

func (f writableField) sent() string {
	kind := cmp.Or(f.kind, "EnumProjectCustomField")
	translated := "null"
	if f.translate != "" {
		translated = strconv.Quote(f.translate)
	}
	parts := []string{
		`"$type":` + strconv.Quote(kind),
		`"id":` + strconv.Quote(cmp.Or(f.id, "180-1")),
		`"canBeEmpty":` + strconv.FormatBool(f.canBeEmpty),
		`"condition":` + cmp.Or(f.condition, "null"),
		`"field":{"$type":"CustomField","name":` + strconv.Quote(f.name) + `,"localizedName":` + translated + `,` +
			`"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(f.valueType) +
			`,"isMultiValue":` + strconv.FormatBool(f.isMultiValue) + `}}`,
	}
	if !slices.Contains(withoutDefaults(), kind) {
		parts = append(parts, `"defaultValues":`+bundleNames(f.defaults))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func bundleNames(names []string) string {
	sent := make([]string, 0, len(names))
	for _, name := range names {
		sent = append(sent, `{"$type":"EnumBundleElement","name":`+strconv.Quote(name)+`}`)
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func projectResponse(fields ...writableField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + strings.Join(sent, ",") + `]}`
}

func projectRequiringNothing() string {
	return projectResponse(writableField{id: "180-1", name: "Optional", valueType: "enum", canBeEmpty: true})
}

func createdIssueWith(readable, summary, descriptionJSON, fields string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `,"summary":` + asJSON(summary) +
		`,"customFields":` + fields + `,"description":` + descriptionJSON + `}`
}

func asJSON(text string) string {
	written, _ := json.Marshal(text)
	return string(written)
}

func creating(t *testing.T, metadata, creation http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creation(w, r)
			return
		}
		metadata(w, r)
	})
}

func writeMetadataRequest(address, code string) string {
	return "GET " + address + "/api/admin/projects/" + code + "?fields=" + projectWriteFields
}

func creationRequest(address, fields string) string {
	return "POST " + address + "/api/issues?fields=" + fields
}

func TestIssueCreateRefusesACallWithNoTitle(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "issue", "create", "DEV")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestIssueCreateReadsTheProjectAndFilesTheIssue(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(writableField{id: "180-1", kind: "SimpleProjectCustomField", name: "Field",
		valueType: "string", canBeEmpty: true})
	held := receivedFields(receivedField{name: "Field", valueType: "string", binding: "180-1", value: `"Third"`})
	server := creating(t, fake.JSON(http.StatusOK, metadata),
		fake.JSON(http.StatusOK, createdIssueWith("DEV-7", "First", `"Second\nline"`, held)))

	got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "First", "--description", "Second\nline",
		"--field", "Field=Third", "--fields", "idReadable,description")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\ndescription: |-\n  Second\n  line\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, server.Methods())
	assert.Equal(t, []string{
		"/api/admin/projects/DEV?fields=" + projectWriteFields,
		"/api/issues?fields=idReadable,description,summary," + customFieldsFields,
	}, server.Targets(t))
	assert.JSONEq(t, `{"project":{"id":"0-1"},"summary":"First","description":"Second\nline",`+
		`"customFields":[{"$type":"SimpleIssueCustomField","name":"Field","value":"Third"}]}`, server.Last(t).Body)
}

func TestIssueCreateNamesEveryRequiredFieldAtOnce(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(
		writableField{id: "180-1", name: "First", valueType: "enum"},
		writableField{id: "180-2", name: "Filled by the project", valueType: "enum", defaults: []string{"Early"}},
		writableField{id: "180-3", name: "Second", valueType: "enum", isMultiValue: true},
		writableField{id: "180-4", name: "Optional", valueType: "enum", isMultiValue: true, canBeEmpty: true},
	)
	server := creating(t, fake.JSON(http.StatusOK, metadata), fake.Unexpected(t))

	got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "x")

	want := faultDocument{
		code: "missing_required",
		details: []detail{
			{"request", writeMetadataRequest(server.URL, "DEV")},
			{"project", "DEV"},
			{"missing", []any{"First", "Second"}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}

func TestIssueCreateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	server := creating(t, fake.JSON(http.StatusOK, projectRequiringNothing()),
		fake.JSON(http.StatusOK, createdIssueWith("DEV-7", "upper", "null", "[]")))

	got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "Upper", "--fields", "idReadable")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", creationRequest(server.URL, "idReadable,summary")},
			{"issue", "DEV-7"},
			{"mismatch", []any{[]detail{{"field", "summary"}, {"expected", "Upper"}, {"actual", "upper"}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}

func TestIssueCreateRefusesMetadataOfTheProjectOfAnotherShape(t *testing.T) {
	t.Parallel()
	const project = `{"$type":"Project","id":"0-1","shortName":7,"customFields":[]}`
	server := creating(t, fake.JSON(http.StatusOK, project), fake.Unexpected(t))

	got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "x")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", writeMetadataRequest(server.URL, "DEV")},
			{"upstream_status", 200},
			{"upstream_body", project},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}
