package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const fieldListDefault = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"

// field list asks for the ordinal of its own accord, so every request of it carries the name.
const fieldListSent = fieldListDefault + ",ordinal"

// Every name ProjectCustomField and the schemas that extend it declare.
func fieldNames() []any {
	return []any{"$type", "bundle", "canBeEmpty", "condition", "defaultValues", "emptyFieldText", "field",
		"hasRunningJob", "id", "isPublic", "ordinal", "project"}
}

// A refusal names the request field list sends, with its fields= expression as it was written.
func fieldsRequest(address, project, fields string) string {
	return "GET " + address + "/api/admin/projects/" + project + "/customFields?fields=" + fields + "&$top=-1"
}

// The whole project arrives in one request whatever the limit is, so this is the query of every scenario.
func fieldsQueries(fields string) []url.Values {
	return []url.Values{{"fields": {fields}, "$top": {"-1"}}}
}

// fieldListing is the document field list prints, read back.
type fieldListing struct {
	Total     int              `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated bool             `yaml:"truncated"`
	Fields    []map[string]any `yaml:"fields"`
}

func requireFieldListing(t *testing.T, got outcome) fieldListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed fieldListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Fields, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

// printedNames is the name of each field printed, in the order they were printed in.
func printedNames(printed fieldListing) []string {
	var names []string
	for _, field := range printed.Fields {
		names = append(names, field["field"].(map[string]any)["name"].(string))
	}
	return names
}

func TestFieldRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no project", argv: []string{"field", "list"}},
		{name: "a word after the project", argv: []string{"field", "list", "DEV", "x"}},
		{name: "a project code the generated client would send elsewhere", argv: []string{"field", "list", ".."}},
		{name: "fields that are not an expression", argv: []string{"field", "list", "DEV", "--fields", "field("}},
		// $top and $skip count the fields of the array, which is not the order of the project.
		{name: "a limit", argv: []string{"field", "list", "DEV", "--limit", "5"}},
		{name: "a skip", argv: []string{"field", "list", "DEV", "--skip", "5"}},
		{name: "no command of the group", argv: []string{"field"}},
		{name: "an unknown command of the group", argv: []string{"field", "bogus"}},
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

func TestFieldRefusesAFlagGivenTwice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "fields of field list", argv: []string{"field", "list", "DEV", "--fields", "field(name)", "--fields", "ordinal"}},
		{name: "fields of field show", argv: []string{"field", "show", "DEV", "Type", "--fields", "field(name)", "--fields", "canBeEmpty"}},
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

func TestFieldListHelpNamesTheDefaults(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"field", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, fieldListDefault)
}

func TestFieldListPrintsTheFieldsOfTheDevInstanceByOrdinal(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "list", "DEV")

	printed := requireFieldListing(t, got)
	assert.Equal(t, 28, printed.Total)
	assert.Equal(t, 28, printed.Returned)
	// The array the server sends starts with State, which the project puts eighth.
	assert.Equal(t, []string{"Type", "Priority", "Категория", "Клиент", "Модуль системы", "Система", "Assignee",
		"State", "Причина отклонения", "Соисполнители", "Статус анализа", "Плановый спринт", "Порядок реализации",
		"Плановая дата решения", "Релиз", "Статус разработки", "Затраченное время", "Оценка", "Дата начала работы",
		"Внешний номер", "Subsystem", "Подсистемы", "Fixed in build", "Сборки", "Группа доступа",
		"Группы доступа", "Коэффициент", "Примечание"}, printedNames(printed))
	lines := strings.SplitAfter(got.stdout, "\n")
	for _, line := range []string{
		`  - {field: {name: "State", localizedName: "Состояние", fieldType: {valueType: "state", isMultiValue: false}}, canBeEmpty: true}`,
		`  - {field: {name: "Клиент", localizedName: null, fieldType: {valueType: "enum", isMultiValue: true}}, canBeEmpty: false}`,
		`  - {field: {name: "Assignee", localizedName: "Исполнитель", fieldType: {valueType: "user", isMultiValue: false}}, canBeEmpty: true}`,
		`  - {field: {name: "Примечание", localizedName: null, fieldType: {valueType: "text", isMultiValue: false}}, canBeEmpty: true}`,
	} {
		assert.Contains(t, lines, line+"\n")
	}
	// The values a field allows belong to field show; a page of bundles is not a list.
	assert.NotContains(t, got.stdout, "bundle")
	assert.Equal(t, fieldsQueries(fieldListSent), dev.sentQueries())
}

func TestFieldListPrintsTheFieldsOfASecondProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "list", "DOCS")

	const want = `total: 4
returned: 4
truncated: false
fields:
  - {field: {name: "State", localizedName: "Состояние", fieldType: {valueType: "state", isMultiValue: false}}, canBeEmpty: false}
  - {field: {name: "Priority", localizedName: "Приоритет", fieldType: {valueType: "enum", isMultiValue: false}}, canBeEmpty: false}
  - {field: {name: "Assignee", localizedName: "Исполнитель", fieldType: {valueType: "user", isMultiValue: false}}, canBeEmpty: true}
  - {field: {name: "Due Date", localizedName: "Срок", fieldType: {valueType: "date", isMultiValue: false}}, canBeEmpty: true}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, fieldsQueries(fieldListSent), dev.sentQueries())
}

// Nobody ever gave DEMO an order of its own, so every field of it carries ordinal 0 and the whole order
// printed is the tie-break: the array the server sent, which is the order the fields were attached in.
func TestFieldListPrintsTheFieldsOfOneOrdinalOfTheDevInstanceAsReceived(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "list", "DEMO")

	const want = `total: 10
returned: 10
truncated: false
fields:
  - {field: {name: "Priority", localizedName: "Приоритет", fieldType: {valueType: "enum", isMultiValue: false}}, canBeEmpty: false}
  - {field: {name: "Type", localizedName: "Тип", fieldType: {valueType: "enum", isMultiValue: false}}, canBeEmpty: false}
  - {field: {name: "State", localizedName: "Состояние", fieldType: {valueType: "state", isMultiValue: false}}, canBeEmpty: false}
  - {field: {name: "Subsystem", localizedName: "Подсистема", fieldType: {valueType: "ownedField", isMultiValue: false}}, canBeEmpty: true}
  - {field: {name: "Fix versions", localizedName: "Версии исправления", fieldType: {valueType: "version", isMultiValue: true}}, canBeEmpty: true}
  - {field: {name: "Affected versions", localizedName: "Затронутые версии", fieldType: {valueType: "version", isMultiValue: true}}, canBeEmpty: true}
  - {field: {name: "Fixed in build", localizedName: "Исправлено в сборке", fieldType: {valueType: "build", isMultiValue: false}}, canBeEmpty: true}
  - {field: {name: "Assignee", localizedName: "Исполнитель", fieldType: {valueType: "user", isMultiValue: false}}, canBeEmpty: true}
  - {field: {name: "Оценка", localizedName: null, fieldType: {valueType: "period", isMultiValue: false}}, canBeEmpty: true}
  - {field: {name: "Затраченное время", localizedName: null, fieldType: {valueType: "period", isMultiValue: false}}, canBeEmpty: true}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, fieldsQueries(fieldListSent), dev.sentQueries())
}

func TestFieldListRefusesAProjectTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "list", "NOPE")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", fieldsRequest(dev.url, "NOPE", fieldListSent)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id NOPE not found"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

func TestFieldListRefusesTheProjectTheLimitedUserCannotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "field", "list", "DEV")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", fieldsRequest(dev.url, "DEV", fieldListSent)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id DEV not found"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

// The member is sent 200 and an empty array instead of a refusal, and that is the whole difference between a
// project whose fields are hidden and a project that has none.
func TestFieldListRefusesTheEmptyListTheMemberIsSent(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "field", "list", "DEV")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", fieldsRequest(dev.url, "DEV", fieldListSent)},
			{"project", "DEV"},
			{"permission", "jetbrains.jetpass.project-read"},
			authFromEnv(),
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

func TestFieldListRefusesANameTheSchemasOfTheDevInstanceDoNotDeclare(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "list", "DEV", "--fields", "field(name),bogus")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", fieldsRequest(dev.url, "DEV", "field(name),bogus,ordinal")},
			{"fields", "field(name),bogus,ordinal"},
			{"unknown", []any{unknownEntry("bogus", fieldNames()...)}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

// A field the project shows only under a condition carries it, and a field shown always carries null there.
func TestFieldListAddsTheConditionOfTheDevInstanceToTheDefault(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "list", "DEV", "--fields", "+condition(field(field(name)),values(name))")

	printed := requireFieldListing(t, got)
	assert.Equal(t, 28, printed.Returned)
	lines := strings.SplitAfter(got.stdout, "\n")
	assert.Contains(t, lines, `  - {field: {name: "State", localizedName: "Состояние", fieldType: {valueType: "state", `+
		`isMultiValue: false}}, canBeEmpty: true, condition: null}`+"\n")
	assert.Contains(t, lines, `  - {field: {name: "Причина отклонения", localizedName: null, fieldType: `+
		`{valueType: "enum", isMultiValue: false}}, canBeEmpty: false, condition: {field: {field: {name: "State"}}, `+
		`values: [{name: "Отклонена"}]}}`+"\n")
	assert.Equal(t, fieldsQueries(fieldListDefault+",condition(field(field(name)),values(name)),ordinal"), dev.sentQueries())
}

// A record of three fields, the members in an order of the server's own and the ordinals out of it.
const shuffledFields = `[
	{"$type":"SimpleProjectCustomField","ordinal":3,"canBeEmpty":true,
	 "field":{"$type":"CustomField","fieldType":{"isMultiValue":false,"valueType":"string","$type":"FieldType"},"localizedName":null,"name":"Third"}},
	{"canBeEmpty":false,"$type":"EnumProjectCustomField","ordinal":1,
	 "field":{"name":"First","localizedName":"Первое","$type":"CustomField","fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":true}}},
	{"ordinal":2,"canBeEmpty":true,"$type":"StateProjectCustomField",
	 "field":{"fieldType":{"valueType":"state","isMultiValue":false,"$type":"FieldType"},"name":"Second","localizedName":null,"$type":"CustomField"}}
]`

func TestFieldListPrintsTheKeysAsAskedAndTheRecordsByOrdinal(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, shuffledFields))

	got := runWith(t, server.env(), "field", "list", "DEV")

	want := "total: 3\nreturned: 3\ntruncated: false\nfields:\n" +
		`  - {field: {name: "First", localizedName: "Первое", fieldType: {valueType: "enum", isMultiValue: true}}, canBeEmpty: false}` + "\n" +
		`  - {field: {name: "Second", localizedName: null, fieldType: {valueType: "state", isMultiValue: false}}, canBeEmpty: true}` + "\n" +
		`  - {field: {name: "Third", localizedName: null, fieldType: {valueType: "string", isMultiValue: false}}, canBeEmpty: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, fieldsQueries(fieldListSent), server.sentQueries())
}

// unplacedFields is a project of fourteen fields where only two were ever given a place: the twelve between
// them all carry ordinal 0, and the server sends the two placed ones at either end. Twelve is not an arbitrary
// number — an unstable sort leaves a run of equal keys alone below thirteen elements, so a shorter record could
// not tell the two sorts apart.
func unplacedFields() string {
	records := []string{listedField(2, "Placed second")}
	for i := 1; i <= 12; i++ {
		records = append(records, listedField(0, fmt.Sprintf("Unplaced %02d", i)))
	}
	records = append(records, listedField(1, "Placed first"))
	return "[" + strings.Join(records, ",") + "]"
}

func listedField(place int, name string) string {
	return fmt.Sprintf(`{"$type":"SimpleProjectCustomField","ordinal":%d,"canBeEmpty":true,`+
		`"field":{"$type":"CustomField","name":%q,"localizedName":null,`+
		`"fieldType":{"$type":"FieldType","valueType":"string","isMultiValue":false}}}`, place, name)
}

// A project nobody has ordered gives every field the same ordinal, so the tie-break is the whole order the
// caller sees: fields of one ordinal are printed in the order the server sent them in.
func TestFieldListKeepsTheOrderTheServerSentFieldsOfOneOrdinalIn(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, unplacedFields()))

	got := runWith(t, server.env(), "field", "list", "DEV")

	var lines []string
	for i := 1; i <= 12; i++ {
		lines = append(lines, fmt.Sprintf("Unplaced %02d", i))
	}
	lines = append(lines, "Placed first", "Placed second")
	want := "total: 14\nreturned: 14\ntruncated: false\nfields:\n"
	for _, name := range lines {
		want += `  - {field: {name: "` + name + `", localizedName: null, fieldType: {valueType: "string", isMultiValue: false}}, canBeEmpty: true}` + "\n"
	}
	assert.Equal(t, outcome{stdout: want}, got)
}

// The caller who asks for the ordinal gets it printed, and the request still names it once.
func TestFieldListPrintsTheOrdinalOnlyWhenItIsAskedFor(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, shuffledFields))

	got := runWith(t, server.env(), "field", "list", "DEV", "--fields", "field(name),ordinal")

	want := "total: 3\nreturned: 3\ntruncated: false\nfields:\n" +
		`  - {field: {name: "First"}, ordinal: 1}` + "\n" +
		`  - {field: {name: "Second"}, ordinal: 2}` + "\n" +
		`  - {field: {name: "Third"}, ordinal: 3}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, fieldsQueries("field(name),ordinal"), server.sentQueries())
}

// The order is the tool's own doing, so an ordinal it cannot read leaves it nothing to print.
func TestFieldListRefusesAnOrdinalItCannotOrderBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ordinal string
	}{
		{name: "a string", ordinal: `"1"`},
		{name: "null", ordinal: "null"},
		{name: "a fraction", ordinal: "1.5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `[{"$type":"EnumProjectCustomField","canBeEmpty":true,"ordinal":` + tc.ordinal +
				`,"field":{"$type":"CustomField","name":"A","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]`
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "field", "list", "DEV")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", fieldsRequest(server.url, "DEV", fieldListSent)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
		})
	}
}

func TestFieldListRefusesAnOrdinalMissingFromTheResponse(t *testing.T) {
	t.Parallel()
	body := `[{"$type":"EnumProjectCustomField","canBeEmpty":true,` +
		`"field":{"$type":"CustomField","name":"A","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]`
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "field", "list", "DEV")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", fieldsRequest(server.url, "DEV", fieldListSent)},
			{"fields", fieldListSent},
			{"missing", []any{missingEntry("ordinal", "EnumProjectCustomField")}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
}
