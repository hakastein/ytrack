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

// The expression ytrack writes for itself: every custom field of the project with the id it is addressed by.
const metadataSent = "customFields(id,field(name,localizedName,fieldType(valueType,isMultiValue)))"

// A refusal of a name names the request the metadata came from.
func metadataRequest(address, project string) string {
	return "GET " + address + "/api/admin/projects/" + project + "?fields=" + metadataSent
}

func fieldRequest(address, project, id, fields string) string {
	return "GET " + address + "/api/admin/projects/" + project + "/customFields/" + id + "?fields=" + fields
}

// What field show prints for Type of DEV, whatever of its names it was asked by.
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

// One record of the metadata of a project; a project that calls a field nothing of its own sends null.
func projectField(id, name, localized string) string {
	localizedName := "null"
	if localized != "" {
		localizedName = strconv.Quote(localized)
	}
	return fmt.Sprintf(`{"$type":"EnumProjectCustomField","id":%q,"field":{"$type":"CustomField","name":%q,`+
		`"localizedName":%s,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}`,
		id, name, localizedName)
}

func projectMetadata(fields ...string) string {
	return `{"$type":"Project","customFields":[` + strings.Join(fields, ",") + `]}`
}

// The answer to the second request: the field as the default asks for it, bundle and all, since an enum keeps
// the values it allows there and a key asked for that does not arrive is refused.
func oneField(name, localized string, canBeEmpty bool) string {
	localizedName := "null"
	if localized != "" {
		localizedName = strconv.Quote(localized)
	}
	return fmt.Sprintf(`{"$type":"EnumProjectCustomField","field":{"$type":"CustomField","name":%q,`+
		`"localizedName":%s,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}},`+
		`"canBeEmpty":%t,"bundle":{"$type":"EnumBundle","values":[]}}`, name, localizedName, canBeEmpty)
}

// serveTheProject answers the metadata of a project and hands the request for one field to field.
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

// noField stands for the second request a refusal over the metadata must not reach.
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
			// Which default the expression is read against is a function of the field, but the grammar is not,
			// so an expression that parses nowhere is refused before the metadata is read.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
		})
	}
}

// The default of one field is not the default of the next, so the help has to name the base and both places a
// field's values may arrive from.
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

// A field answers to either of its names in any letter case, and the name itself never leaves: it is the
// cyrillic тип that must be absent, since the latin type sits inside fieldType.
func TestFieldShowResolvesANameOfTheDevInstanceWhateverTheCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// asked is the name given on the command line.
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

// A field name belongs to a project, so the same name is a different field, or none, in another one.
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

	// DEV has the field and DEMO names nothing near it, so every name DEMO does have is offered instead.
	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", metadataRequest(dev.url, "DEMO")},
			{"project", "DEMO"},
			{"unknown", []any{unknownEntry("Причина отклонения", "Affected versions", "Assignee", "Fix versions",
				"Fixed in build", "Priority", "State", "Subsystem", "Type", "Затраченное время", "Оценка")}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
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
			// One edit from Тип, the name DEV translates Type into.
			{"unknown", []any{unknownEntry("Типп", "Type")}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, dev.sentPaths())
}

// A refusal over a name is a place to look next, not a listing of the project, so it holds five names at most
// and only the ones within two edits — nearest first, and by name where two are equally near.
func TestFieldShowOffersFiveOfTheNearestNamesAtMost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// held is the project the name is resolved against, and nearest what the refusal is to offer.
		held    []string
		nearest []any
	}{
		{
			name: "seven names one edit away and one two edits away",
			// The project sends them in an order of its own, so the five offered are the five the rule picks
			// rather than the five that happened to arrive first.
			held: []string{"Typl", "Typi", "Typf", "Ty", "Typk", "Typg", "Typj", "Typh"},
			// Ty is two edits away and stands before every Typ- by name, so it is the distance that puts it out.
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
			assert.Equal(t, want, requireRefusal(t, got))
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
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, dev.sentPaths())
}

// The member is sent the project with an empty list of custom fields, and a name cannot be resolved against
// nothing: the refusal names the right that is missing rather than the name.
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
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, dev.sentPaths())
}

// Срок is the name of one field and what the project calls another, so the two are told apart by the name
// itself and nothing else is. The name wins whatever letter case it is written in: a name is matched case
// aside, and the field a name names is never the field it merely translates.
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
		name string
		// asked is the name given on the command line, and metadata the project it is resolved against.
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
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

// pflag reads a leading dash as flags, so a field named that way is written after --; the name resolves like
// any other and is refused by the project, not by the grammar of the call.
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
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

// The generated client turns "." and ".." into another endpoint, so an id of any other shape is refused before
// it reaches a path.
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
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

// One record of the metadata of a project with every member written out, so a scenario can put anything under
// any of them.
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
		// held is what arrives under customFields.
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
			assert.Equal(t, want, requireRefusal(t, got))
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
	assert.Equal(t, want, requireRefusal(t, got))
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
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 2)
}

// The id was chosen off the metadata, so an answer that names another field says the project changed under
// the two requests and the field printed would not be the one asked for.
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
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Len(t, server.requests(), 2)
		})
	}
}

// The answer is held to the field the name resolved to whatever the caller asked to see, so the naming goes
// out beside the caller's expression and is printed only where the caller asked for it.
func TestFieldShowSendsTheNamingBesideWhatTheCallerAsksFor(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("180-1", "Type", "Тип"))
	server := serveTheProject(t, metadata, respondWith(http.StatusOK, oneField("Type", "Тип", false)))

	got := runWith(t, server.env(), "field", "show", "DEV", "Type", "--fields", "field(name)")

	assert.Equal(t, outcome{stdout: "field:\n  name: \"Type\"\n"}, got)
	assert.Equal(t, []string{metadataSent, "field(name,localizedName,fieldType(valueType,isMultiValue))"}, server.sentFields())
}
