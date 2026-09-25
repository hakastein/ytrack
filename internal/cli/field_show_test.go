package cli_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const metadataSent = "customFields(id,field(name,localizedName,fieldType(valueType,isMultiValue)))"

func metadataRequest(address, project string) string {
	return "GET " + address + "/api/admin/projects/" + project + "?fields=" + metadataSent
}

func fieldRequest(address, project, id, fields string) string {
	return "GET " + address + "/api/admin/projects/" + project + "/customFields/" + id + "?fields=" + fields
}

const typeOfDEV = `field:
  name: "Type"
  localizedName: "Тип"
  fieldType:
    valueType: "enum"
    isMultiValue: false
canBeEmpty: false
bundle:
  values:
    - {name: "Bug", archived: false}
    - {name: "Epic", archived: false}
    - {name: "User Story", archived: false}
    - {name: "Task", archived: false}
    - {name: "Инцидент", archived: false}
`

func requireTheTwoRequests(t *testing.T, u *upstream, project string) {
	t.Helper()
	paths := u.sentPaths()
	require.Len(t, paths, 2, "paths: %v", paths)
	assert.Equal(t, "/api/admin/projects/"+project, paths[0])
	assert.Regexp(t, `^/api/admin/projects/`+project+`/customFields/[0-9]+-[0-9]+$`, paths[1])
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

func serveTheProject(t *testing.T, metadata string, field http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/customFields/") {
			field(w, r)
			return
		}
		respondWith(http.StatusOK, metadata)(w, r)
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
		{name: "no project and no name", argv: []string{"field", "show"}},
		{name: "no name", argv: []string{"field", "show", "DEV"}},
		{name: "a word after the name", argv: []string{"field", "show", "DEV", "a", "b"}},
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
		{
			name: "a name pflag reads as flags",
			argv: []string{"field", "show", "DEV", "-x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestFieldShowHelpNamesTheUsage(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"field", "show", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "ytrack field show <project> <field>")
}

func TestFieldShowPrintsAFieldOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: typeOfDEV}, got)
	requireTheTwoRequests(t, dev, "DEV")
	assert.Equal(t, []string{metadataSent, fieldShowDefault(bundleValues)}, dev.sentFields())
}

func TestFieldShowResolvesANameOfTheDevInstanceWhateverTheCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		asked string
	}{
		{name: "the localized name", asked: "тип"},
		{name: "the localized name in upper case", asked: "ТИП"},
		{name: "the name in upper case", asked: "TYPE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, dev.env(), "field", "show", "DEV", tc.asked)

			assert.Equal(t, outcome{stdout: typeOfDEV}, got)
			requireTheTwoRequests(t, dev, "DEV")
			for _, target := range dev.sentTargets() {
				assert.NotContains(t, strings.ToLower(target), "тип")
			}
		})
	}
}

func TestFieldShowResolvesALocalizedNameOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "show", "DEV", "исполнитель")

	const want = `field:
  name: "Assignee"
  localizedName: "Исполнитель"
  fieldType:
    valueType: "user"
    isMultiValue: false
canBeEmpty: true
bundle:
  aggregatedUsers:
    - {login: "admin"}
`
	assert.Equal(t, outcome{stdout: want}, got)
	requireTheTwoRequests(t, dev, "DEV")
}

func TestFieldShowPrintsAFieldOfASecondProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "show", "DEMO", "State")

	const want = `field:
  name: "State"
  localizedName: "Состояние"
  fieldType:
    valueType: "state"
    isMultiValue: false
canBeEmpty: false
bundle:
  values:
    - {name: "To do", archived: false}
    - {name: "In Progress", archived: false}
    - {name: "Done", archived: false}
`
	assert.Equal(t, outcome{stdout: want}, got)
	requireTheTwoRequests(t, dev, "DEMO")
}

func TestFieldShowRefusesAFieldASecondProjectOfTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "show", "DEMO", "Причина отклонения")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", metadataRequest(dev.url, "DEMO")},
			{"project", "DEMO"},
			{"unknown", []any{unknownEntry("Причина отклонения", "Affected versions", "Assignee", "Fix versions",
				"Fixed in build", "Priority", "State", "Subsystem", "Type", "Затраченное время", "Оценка")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEMO"}, dev.sentPaths())
}

func TestFieldShowRefusesANameOneFieldOfTheDevInstanceIsNear(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "show", "DEV", "Типп")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", metadataRequest(dev.url, "DEV")},
			{"project", "DEV"},
			{"unknown", []any{unknownEntry("Типп", "Type")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, dev.sentPaths())
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

			got := runWith(t, server.env(), "field", "show", "DEV", "Type")

			want := faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", metadataRequest(server.url, "DEV")},
					{"project", "DEV"},
					{"unknown", []any{unknownEntry("Type", tc.nearest...)}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestFieldShowRefusesTheProjectTheLimitedUserCannotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", metadataRequest(dev.url, "DEV")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id DEV not found"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, dev.sentPaths())
}

func TestFieldShowRefusesTheEmptyMetadataTheMemberIsSent(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", metadataRequest(dev.url, "DEV")},
			{"project", "DEV"},
			{"permission", "jetbrains.jetpass.project-read"},
			authFromEnv(),
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, dev.sentPaths())
}

func TestFieldShowTakesANameOverTheTranslationOfAnotherField(t *testing.T) {
	t.Parallel()
	for _, asked := range []string{"Срок", "СРОК"} {
		t.Run(asked, func(t *testing.T) {
			t.Parallel()
			metadata := projectMetadata(projectField("180-1", "Срок", ""), projectField("180-2", "Due Date", "Срок"))
			server := serveTheProject(t, metadata, respondWith(http.StatusOK, oneField("Срок", "", true)))

			got := runWith(t, server.env(), "field", "show", "DEV", asked)

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
			assert.Equal(t, []string{"/api/admin/projects/DEV", "/api/admin/projects/DEV/customFields/180-1"}, server.sentPaths())
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

			got := runWith(t, server.env(), "field", "show", "DEV", tc.asked)

			want := faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", metadataRequest(server.url, "DEV")},
					{"project", "DEV"},
					{"unknown", []any{unknownEntry(tc.asked, tc.nearest...)}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestFieldShowTakesANameWithALeadingDashAfterTheDoubleDash(t *testing.T) {
	t.Parallel()
	server := serveTheProject(t, projectMetadata(projectField("180-1", "Type", "Тип")), noField(t))

	got := runWith(t, server.env(), "field", "show", "DEV", "--", "-x")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", metadataRequest(server.url, "DEV")},
			{"project", "DEV"},
			{"unknown", []any{unknownEntry("-x", "Type")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 1)
}

func TestFieldShowRefusesAnIdItCannotAddress(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("..", "Type", "Тип"))
	server := serveTheProject(t, metadata, noField(t))

	got := runWith(t, server.env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", metadataRequest(server.url, "DEV")},
			{"upstream_status", 200},
			{"upstream_body", metadata},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 1)
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

			got := runWith(t, server.env(), "field", "show", "DEV", "Type")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", metadataRequest(server.url, "DEV")},
					{"upstream_status", 200},
					{"upstream_body", metadata},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestFieldShowRefusesAFieldOfAShapeItCannotCompare(t *testing.T) {
	t.Parallel()
	const field = `{"$type":"EnumProjectCustomField","field":[],"canBeEmpty":false,` +
		`"bundle":{"$type":"EnumBundle","values":[]}}`
	metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
	server := serveTheProject(t, metadata, respondWith(http.StatusOK, field))

	got := runWith(t, server.env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", fieldRequest(server.url, "DEV", "180-1", fieldShowDefault(bundleValues))},
			{"upstream_status", 200},
			{"upstream_body", field},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 2)
}

func TestFieldShowRefusesAFieldGoneBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	const gone = `{"error":"Not Found","error_description":"Entity with id 180-1 not found"}`
	metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
	server := serveTheProject(t, metadata, respondWith(http.StatusNotFound, gone))

	got := runWith(t, server.env(), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", fieldRequest(server.url, "DEV", "180-1", fieldShowDefault(bundleValues))},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id 180-1 not found"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 2)
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
			server := serveTheProject(t, metadata, respondWith(http.StatusOK, tc.field))

			got := runWith(t, server.env(), "field", "show", "DEV", "Type")

			want := faultDocument{
				code: "upstream_failed",
				details: []detail{
					{"request", fieldRequest(server.url, "DEV", "180-1", fieldShowDefault(bundleValues))},
					{"upstream_status", 200},
					{"project", "DEV"},
					{"field", "Type"},
					{"upstream_body", tc.field},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 2)
		})
	}
}

func TestFieldShowSendsTheNamingBesideWhatTheCallerAsksFor(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
	server := serveTheProject(t, metadata, respondWith(http.StatusOK, oneField("Type", "Тип", false)))

	got := runWith(t, server.env(), "field", "show", "DEV", "Type", "--fields", "field(name)")

	assert.Equal(t, outcome{stdout: "field:\n  name: \"Type\"\n"}, got)
	assert.Equal(t, []string{metadataSent, "field(name,localizedName,fieldType(valueType,isMultiValue))"}, server.sentFields())
}
