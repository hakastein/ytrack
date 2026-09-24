package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two places the values a field allows arrive from, as field show asks for them.
const (
	bundleValues = "bundle(values(name,archived))"
	bundleUsers  = "bundle(aggregatedUsers(login))"
)

// fieldShowDefault is the fields= field show sends for a field whose type keeps its values under tail.
func fieldShowDefault(tail string) string {
	if tail == "" {
		return fieldListDefault
	}
	return fieldListDefault + "," + tail
}

// Every row of the catalogue of custom-field types against the field of DEV that has it: what the default asks
// the server for, and what comes back. Subtest names are the rows themselves and stay ASCII, since they become
// the paths of the cassettes.
func TestFieldShowAsksWhereTheValuesOfEachRowOfTheCatalogueLive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// field is the name the field goes by on DEV, and tail the place its type keeps its values.
		field   string
		tail    string
		printed string
	}{
		{
			name:  "enum single",
			field: "Type",
			tail:  bundleValues,
			printed: `field:
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
`,
		},
		{
			name:  "enum multiple",
			field: "Клиент",
			tail:  bundleValues,
			printed: `field:
  name: "Клиент"
  localizedName: null
  fieldType:
    valueType: "enum"
    isMultiValue: true
canBeEmpty: false
bundle:
  values:
    - {name: "ACME", archived: false}
    - {name: "АЛЬФА", archived: false}
    - {name: "Бета-Девелопмент", archived: false}
    - {name: "ГАММА", archived: false}
    - {name: "ООО \"РОМАШКА\"", archived: false}
    - {name: "NORTH WIND", archived: false}
`,
		},
		{
			name:  "state single",
			field: "State",
			tail:  bundleValues,
			printed: `field:
  name: "State"
  localizedName: "Состояние"
  fieldType:
    valueType: "state"
    isMultiValue: false
canBeEmpty: true
bundle:
  values:
    - {name: "In Progress", archived: false}
    - {name: "Done", archived: false}
    - {name: "Duplicate", archived: false}
    - {name: "Новая", archived: false}
    - {name: "Отклонена", archived: false}
    - {name: "Закрыта", archived: false}
    - {name: "Готова к работе", archived: false}
    - {name: "На проверке", archived: false}
    - {name: "На приёмке", archived: false}
    - {name: "Уточнение", archived: false}
    - {name: "Отложено", archived: false}
`,
		},
		{
			name:  "version single",
			field: "Релиз",
			tail:  bundleValues,
			printed: `field:
  name: "Релиз"
  localizedName: null
  fieldType:
    valueType: "version"
    isMultiValue: false
canBeEmpty: true
bundle:
  values:
    - {name: "2026.1", archived: false}
    - {name: "2026.2", archived: false}
`,
		},
		{
			name:  "version multiple",
			field: "Плановый спринт",
			tail:  bundleValues,
			printed: `field:
  name: "Плановый спринт"
  localizedName: null
  fieldType:
    valueType: "version"
    isMultiValue: true
canBeEmpty: true
bundle:
  values:
    - {name: "SPR-78", archived: true}
    - {name: "SPR-24", archived: true}
    - {name: "SPR-51", archived: true}
    - {name: "SPR-52", archived: true}
    - {name: "SPR-92", archived: false}
    - {name: "SPR-93", archived: false}
`,
		},
		{
			name:  "build single",
			field: "Fixed in build",
			tail:  bundleValues,
			printed: `field:
  name: "Fixed in build"
  localizedName: "Исправлено в сборке"
  fieldType:
    valueType: "build"
    isMultiValue: false
canBeEmpty: true
bundle:
  values:
    - {name: "13757", archived: false}
    - {name: "13874", archived: false}
`,
		},
		{
			name:  "build multiple",
			field: "Сборки",
			tail:  bundleValues,
			printed: `field:
  name: "Сборки"
  localizedName: null
  fieldType:
    valueType: "build"
    isMultiValue: true
canBeEmpty: true
bundle:
  values:
    - {name: "2026.1.1", archived: false}
    - {name: "2026.1.2", archived: false}
`,
		},
		{
			name:  "ownedField single",
			field: "Subsystem",
			tail:  bundleValues,
			printed: `field:
  name: "Subsystem"
  localizedName: "Подсистема"
  fieldType:
    valueType: "ownedField"
    isMultiValue: false
canBeEmpty: true
bundle:
  values:
    - {name: "Ядро", archived: false}
    - {name: "Отчёты", archived: false}
    - {name: "Интеграции", archived: false}
`,
		},
		{
			name:  "ownedField multiple",
			field: "Подсистемы",
			tail:  bundleValues,
			printed: `field:
  name: "Подсистемы"
  localizedName: null
  fieldType:
    valueType: "ownedField"
    isMultiValue: true
canBeEmpty: true
bundle:
  values:
    - {name: "Биллинг", archived: false}
    - {name: "Уведомления", archived: false}
`,
		},
		{
			name:  "user single",
			field: "Assignee",
			tail:  bundleUsers,
			printed: `field:
  name: "Assignee"
  localizedName: "Исполнитель"
  fieldType:
    valueType: "user"
    isMultiValue: false
canBeEmpty: true
bundle:
  aggregatedUsers:
    - {login: "admin"}
`,
		},
		{
			// An empty bundle allows everyone rather than no one, banned accounts included.
			name:  "user multiple",
			field: "Соисполнители",
			tail:  bundleUsers,
			printed: `field:
  name: "Соисполнители"
  localizedName: null
  fieldType:
    valueType: "user"
    isMultiValue: true
canBeEmpty: true
bundle:
  aggregatedUsers: []
`,
		},
		{
			name:  "group single",
			field: "Группа доступа",
			printed: `field:
  name: "Группа доступа"
  localizedName: null
  fieldType:
    valueType: "group"
    isMultiValue: false
canBeEmpty: true
`,
		},
		{
			name:  "group multiple",
			field: "Группы доступа",
			printed: `field:
  name: "Группы доступа"
  localizedName: null
  fieldType:
    valueType: "group"
    isMultiValue: true
canBeEmpty: true
`,
		},
		{
			name:  "period",
			field: "Оценка",
			printed: `field:
  name: "Оценка"
  localizedName: null
  fieldType:
    valueType: "period"
    isMultiValue: false
canBeEmpty: true
`,
		},
		{
			name:  "text",
			field: "Примечание",
			printed: `field:
  name: "Примечание"
  localizedName: null
  fieldType:
    valueType: "text"
    isMultiValue: false
canBeEmpty: true
`,
		},
		{
			name:  "date",
			field: "Плановая дата решения",
			printed: `field:
  name: "Плановая дата решения"
  localizedName: null
  fieldType:
    valueType: "date"
    isMultiValue: false
canBeEmpty: true
`,
		},
		{
			name:  "date and time",
			field: "Дата начала работы",
			printed: `field:
  name: "Дата начала работы"
  localizedName: null
  fieldType:
    valueType: "date and time"
    isMultiValue: false
canBeEmpty: true
`,
		},
		{
			name:  "integer",
			field: "Порядок реализации",
			printed: `field:
  name: "Порядок реализации"
  localizedName: null
  fieldType:
    valueType: "integer"
    isMultiValue: false
canBeEmpty: true
`,
		},
		{
			name:  "float",
			field: "Коэффициент",
			printed: `field:
  name: "Коэффициент"
  localizedName: null
  fieldType:
    valueType: "float"
    isMultiValue: false
canBeEmpty: true
`,
		},
		{
			name:  "string",
			field: "Внешний номер",
			printed: `field:
  name: "Внешний номер"
  localizedName: null
  fieldType:
    valueType: "string"
    isMultiValue: false
canBeEmpty: true
`,
		},
	}
	require.Len(t, tests, 20, "the catalogue has twenty rows and every one of them is a scenario")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, dev.env(), "field", "show", "DEV", tc.field)

			assert.Equal(t, outcome{stdout: tc.printed}, got)
			requireTheTwoRequests(t, dev, "DEV")
			assert.Equal(t, []string{metadataSent, fieldShowDefault(tc.tail)}, dev.sentFields())
		})
	}
}

// A user bundle keeps under values the users and groups it was built from, and the users the field allows are
// aggregatedUsers: the two answer different questions, so only the caller who names values is shown them.
func TestFieldShowPrintsTheValuesOfAUserBundleOnlyWhenTheCallerNamesThem(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "show", "DEV", "Assignee", "--fields", "bundle(values(name),aggregatedUsers(login))")

	const want = `bundle:
  values:
    - {name: "DEVELOPMENT Team"}
  aggregatedUsers:
    - {login: "admin"}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{metadataSent, "bundle(values(name),aggregatedUsers(login))," +
		"field(name,localizedName,fieldType(valueType,isMultiValue))"}, dev.sentFields())
}

// A + adds to the default of the field's own type, so a name asked of the bundle joins the ones already there
// rather than replacing them.
func TestFieldShowAddsToTheDefaultOfTheTypeOfTheField(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "field", "show", "DEV", "Type", "--fields", "+bundle(values(description))")

	const want = `field:
  name: "Type"
  localizedName: "Тип"
  fieldType:
    valueType: "enum"
    isMultiValue: false
canBeEmpty: false
bundle:
  values:
    - {name: "Bug", archived: false, description: null}
    - {name: "Epic", archived: false, description: null}
    - {name: "User Story", archived: false, description: null}
    - {name: "Task", archived: false, description: null}
    - {name: "Инцидент", archived: false, description: null}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{metadataSent, fieldListDefault + ",bundle(values(name,archived,description))"}, dev.sentFields())
}

// The catalogue has no multi-valued state field: where its values would live is the one thing ytrack cannot
// guess, and the server would answer the same type again.
const (
	stateOfMany = `{"$type":"StateProjectCustomField","id":"180-1","field":{"$type":"CustomField","name":"State",` +
		`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"state","isMultiValue":true}}}`
	answeredStateOfMany = `{"$type":"StateProjectCustomField","field":{"$type":"CustomField","name":"State",` +
		`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"state","isMultiValue":true}}}`
)

func TestFieldShowRefusesTheDefaultOfATypeOutsideTheCatalogue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no fields of the caller's own", argv: []string{"field", "show", "DEV", "State"}},
		{name: "fields added to the default", argv: []string{"field", "show", "DEV", "State", "--fields", "+canBeEmpty"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := projectMetadata(stateOfMany)
			server := serveTheProject(t, metadata, noField(t))

			got := runWith(t, server.env(), tc.argv...)

			want := refusal{
				code: "upstream_lied",
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

// Only a default has to know where the values of a field live, so a caller who wrote the whole expression is
// answered whatever the type is.
func TestFieldShowPrintsAFieldOfATypeOutsideTheCatalogueTheCallerAsksFor(t *testing.T) {
	t.Parallel()
	field := answer(http.StatusOK, answeredStateOfMany)
	server := serveTheProject(t, projectMetadata(stateOfMany), field)

	got := runWith(t, server.env(), "field", "show", "DEV", "State", "--fields", "field(name)")

	assert.Equal(t, outcome{stdout: "field:\n  name: \"State\"\n"}, got)
	assert.Len(t, server.requests(), 2)
}
