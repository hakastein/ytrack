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

const metadataSent = "customFields(id,field(name,localizedName,fieldType(valueType,isMultiValue)))"

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

func TestFieldShowRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{
			name: "the empty name",
			argv: []string{"field", "show", "DEV", ""},
		},
		{
			name: "a project code the generated client would send elsewhere",
			argv: []string{"field", "show", "..", "Type"},
		},
		{
			name: "an expression with nothing in it",
			argv: []string{"field", "show", "DEV", "Type", "--fields", ""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestFieldShowOffersFiveOfTheNearestNamesAtMost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		held    []string
		nearest []any
	}{
		{
			name:    "seven names one edit away and one two edits away",
			held:    []string{"Typl", "Typi", "Typf", "Ty", "Typk", "Typg", "Typj", "Typh"},
			nearest: []any{"Typf", "Typg", "Typh", "Typi", "Typj"},
		},
		{
			name:    "a name three edits away beside one that is nearer",
			held:    []string{"Typf", "Typxyz"},
			nearest: []any{"Typf"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var records []string
			for i, held := range tc.held {
				records = append(records, projectField(fmt.Sprintf("180-%d", i), held, ""))
			}
			server := serveTheProject(t, projectMetadata(records...), noField(t))

			got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

			want := faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", metadataRequest(server.URL, "DEV")},
					{"project", "DEV"},
					{"unknown", []any{unknownEntry("Type", tc.nearest...)}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.Requests(), 1)
		})
	}
}

func TestFieldShowTakesANameOverTheTranslationOfAnotherField(t *testing.T) {
	t.Parallel()
	for _, asked := range []string{"Срок", "СРОК"} {
		t.Run(asked, func(t *testing.T) {
			t.Parallel()
			metadata := projectMetadata(projectField("180-1", "Срок", ""), projectField("180-2", "Due Date", "Срок"))
			server := serveTheProject(t, metadata, fake.JSON(http.StatusOK, oneField("Срок", "", true)))

			got := runWith(t, server.Env(), "field", "show", "DEV", asked)

			const want = `field:
  name: "Срок"
  localizedName: null
  fieldType:
    valueType: "enum"
    isMultiValue: false
canBeEmpty: true
bundle:
  values: []
`
			assert.Equal(t, outcome{stdout: want}, got)
			assert.Equal(t, []string{"/api/admin/projects/DEV", "/api/admin/projects/DEV/customFields/180-1"}, server.Paths())
		})
	}
}

func TestFieldShowRefusesANameMoreThanOneFieldAnswersTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		asked    string
		metadata string
		nearest  []any
	}{
		{
			name:     "one name of two fields, in another case",
			asked:    "оценка",
			metadata: projectMetadata(projectField("180-1", "Оценка", ""), projectField("180-2", "ОЦЕНКА", "")),
			nearest:  []any{"ОЦЕНКА", "Оценка"},
		},
		{
			name:     "one translation of two fields",
			asked:    "статус",
			metadata: projectMetadata(projectField("180-1", "Development status", "Статус"), projectField("180-2", "Product status", "Статус")),
			nearest:  []any{"Development status", "Product status"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveTheProject(t, tc.metadata, noField(t))

			got := runWith(t, server.Env(), "field", "show", "DEV", tc.asked)

			want := faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", metadataRequest(server.URL, "DEV")},
					{"project", "DEV"},
					{"unknown", []any{unknownEntry(tc.asked, tc.nearest...)}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.Requests(), 1)
		})
	}
}

func TestFieldShowRefusesAnIdItCannotAddress(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("..", "Type", "Тип"))
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

func fieldRecordOf(id, field string) string {
	return `{"$type":"EnumProjectCustomField","id":` + id + `,"field":` + field + `}`
}

func namingOf(name, localizedName, kind string) string {
	return `{"$type":"CustomField","name":` + name + `,"localizedName":` + localizedName + `,"fieldType":` + kind + `}`
}

func typeOf(valueType, isMultiValue string) string {
	return `{"$type":"FieldType","valueType":` + valueType + `,"isMultiValue":` + isMultiValue + `}`
}

func TestFieldShowRefusesMetadataOfAShapeItCannotRead(t *testing.T) {
	t.Parallel()
	enum := typeOf(`"enum"`, `false`)
	naming := namingOf(`"Type"`, `null`, enum)
	tests := []struct {
		name string
		held string
	}{
		{name: "custom fields that are not an array", held: fieldRecordOf(`"180-1"`, naming)},
		{name: "a record that is not an object", held: `[[]]`},
		{name: "an id that is not text", held: `[` + fieldRecordOf(`5`, naming) + `]`},
		{name: "a naming that is not an object", held: `[` + fieldRecordOf(`"180-1"`, `null`) + `]`},
		{name: "a name that is not text", held: `[` + fieldRecordOf(`"180-1"`, namingOf(`5`, `null`, enum)) + `]`},
		{name: "a type that is not an object", held: `[` + fieldRecordOf(`"180-1"`, namingOf(`"Type"`, `null`, `[]`)) + `]`},
		{name: "a value type that is not text", held: `[` + fieldRecordOf(`"180-1"`, namingOf(`"Type"`, `null`, typeOf(`5`, `false`))) + `]`},
		{name: "a multiplicity that is not a bool", held: `[` + fieldRecordOf(`"180-1"`, namingOf(`"Type"`, `null`, typeOf(`"enum"`, `"no"`))) + `]`},
		{name: "a translation that is neither text nor null", held: `[` + fieldRecordOf(`"180-1"`, namingOf(`"Type"`, `5`, enum)) + `]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := `{"$type":"Project","customFields":` + tc.held + `}`
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
		})
	}
}

func TestFieldShowRefusesAFieldOfAShapeItCannotCompare(t *testing.T) {
	t.Parallel()
	const field = `{"$type":"EnumProjectCustomField","field":[],"canBeEmpty":false,` +
		`"bundle":{"$type":"EnumBundle","values":[]}}`
	metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
	server := serveTheProject(t, metadata, fake.JSON(http.StatusOK, field))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", fieldRequest(server.URL, "DEV", "180-1", fieldShowDefault(bundleValues))},
			{"upstream_status", 200},
			{"upstream_body", field},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 2)
}

func TestFieldShowRefusesAFieldGoneBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	const gone = `{"error":"Not Found","error_description":"Entity with id 180-1 not found"}`
	metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
	server := serveTheProject(t, metadata, fake.JSON(http.StatusNotFound, gone))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", fieldRequest(server.URL, "DEV", "180-1", fieldShowDefault(bundleValues))},
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
	tests := []struct {
		name  string
		field string
	}{
		{name: "another name", field: oneField("Kind", "Тип", false)},
		{name: "another translation", field: oneField("Type", "Вид", false)},
		{name: "no translation", field: oneField("Type", "", false)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
			server := serveTheProject(t, metadata, fake.JSON(http.StatusOK, tc.field))

			got := runWith(t, server.Env(), "field", "show", "DEV", "Type")

			want := faultDocument{
				code: "upstream_failed",
				details: []detail{
					{"request", fieldRequest(server.URL, "DEV", "180-1", fieldShowDefault(bundleValues))},
					{"upstream_status", 200},
					{"project", "DEV"},
					{"field", "Type"},
					{"upstream_body", tc.field},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.Requests(), 2)
		})
	}
}

func TestFieldShowSendsTheNamingBesideWhatTheCallerAsksFor(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
	server := serveTheProject(t, metadata, fake.JSON(http.StatusOK, oneField("Type", "Тип", false)))

	got := runWith(t, server.Env(), "field", "show", "DEV", "Type", "--fields", "field(name)")

	assert.Equal(t, outcome{stdout: "field:\n  name: \"Type\"\n"}, got)
	assert.Equal(t, []string{metadataSent, "field(name,localizedName,fieldType(valueType,isMultiValue))"}, server.Fields())
}
