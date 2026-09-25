package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type liveRow struct {
	row     string
	field   string
	given   []string
	sent    string
	value   string
	printed []string
	multi   bool
	block   bool
}

func liveRows() []liveRow {
	return []liveRow{
		{row: "enum 1", field: "Type", given: []string{"Task"}, sent: "SingleEnumIssueCustomField",
			value: `{"name":"Task"}`, printed: []string{"Task"}},
		{row: "enum *", field: "Клиент", given: []string{"ACME"}, sent: "MultiEnumIssueCustomField",
			value: `[{"name":"ACME"}]`, printed: []string{"ACME"}, multi: true},
		{row: "state 1", field: "State", given: []string{"Новая"}, sent: "StateIssueCustomField",
			value: `{"name":"Новая"}`, printed: []string{"Новая"}},
		{row: "version 1", field: "Релиз", given: []string{"2026.1"}, sent: "SingleVersionIssueCustomField",
			value: `{"name":"2026.1"}`, printed: []string{"2026.1"}},
		{row: "version *", field: "Плановый спринт", given: []string{"SPR-92"},
			sent: "MultiVersionIssueCustomField", value: `[{"name":"SPR-92"}]`,
			printed: []string{"SPR-92"}, multi: true},
		{row: "build 1", field: "Fixed in build", given: []string{"13757"}, sent: "SingleBuildIssueCustomField",
			value: `{"name":"13757"}`, printed: []string{"13757"}},
		{row: "build *", field: "Сборки", given: []string{"2026.1.1"}, sent: "MultiBuildIssueCustomField",
			value: `[{"name":"2026.1.1"}]`, printed: []string{"2026.1.1"}, multi: true},
		{row: "ownedField 1", field: "Subsystem", given: []string{"Ядро"}, sent: "SingleOwnedIssueCustomField",
			value: `{"name":"Ядро"}`, printed: []string{"Ядро"}},
		{row: "ownedField *", field: "Подсистемы", given: []string{"Биллинг"},
			sent: "MultiOwnedIssueCustomField", value: `[{"name":"Биллинг"}]`,
			printed: []string{"Биллинг"}, multi: true},
		{row: "user 1", field: "Assignee", given: []string{"admin"}, sent: "SingleUserIssueCustomField",
			value: `{"login":"admin"}`, printed: []string{"admin"}},
		{row: "user *", field: "Соисполнители", given: []string{"admin"}, sent: "MultiUserIssueCustomField",
			value: `[{"login":"admin"}]`, printed: []string{"admin"}, multi: true},
		{row: "group 1", field: "Группа доступа", given: []string{"DEVELOPMENT Team"},
			sent: "SingleGroupIssueCustomField", value: `{"name":"DEVELOPMENT Team"}`,
			printed: []string{"DEVELOPMENT Team"}},
		{row: "group *", field: "Группы доступа", given: []string{"Все пользователи"},
			sent: "MultiGroupIssueCustomField", value: `[{"name":"Все пользователи"}]`,
			printed: []string{"Все пользователи"}, multi: true},
		{row: "date", field: "Плановая дата решения", given: []string{"2026-09-16"},
			sent: "DateIssueCustomField", value: "1789560000000", printed: []string{"2026-09-16"}},
		{row: "date and time", field: "Дата начала работы", given: []string{"2026-08-31T03:00:00.123+03:00"},
			sent: "SimpleIssueCustomField", value: "1788134400123",
			printed: []string{"2026-08-31T00:00:00.123Z"}},
		{row: "integer", field: "Порядок реализации", given: []string{"42"}, sent: "SimpleIssueCustomField",
			value: "42", printed: []string{"42"}},
		{row: "float", field: "Коэффициент", given: []string{"1.5"}, sent: "SimpleIssueCustomField",
			value: "1.5", printed: []string{"1.5"}},
		{row: "string", field: "Внешний номер", given: []string{"EXT-1"}, sent: "SimpleIssueCustomField",
			value: `"EXT-1"`, printed: []string{"EXT-1"}},
		{row: "text", field: "Примечание", given: []string{"первая\nвторая"}, sent: "TextIssueCustomField",
			value: `{"text":"первая\nвторая"}`, printed: []string{"первая\nвторая"}, block: true},
		{row: "period", field: "Оценка", given: []string{"PT1H30M"}, sent: "PeriodIssueCustomField",
			value: `{"minutes":90}`, printed: []string{"PT1H30M"}},
	}
}

const rewrittenNote = "другой\r\nтекст"

type rewrittenRow struct {
	row     string
	field   string
	given   []string
	value   string
	printed []string
	multi   bool
}

func rewrittenRows() []rewrittenRow {
	return []rewrittenRow{
		{row: "enum 1", field: "Type", given: []string{"Bug"}, value: `{"name":"Bug"}`,
			printed: []string{"Bug"}},
		{row: "enum *", field: "Клиент", given: []string{"АЛЬФА", "ГАММА"},
			value: `[{"name":"АЛЬФА"},{"name":"ГАММА"}]`, printed: []string{"АЛЬФА", "ГАММА"}, multi: true},
		{row: "state 1", field: "State", given: []string{"In Progress"}, value: `{"name":"In Progress"}`,
			printed: []string{"In Progress"}},
		{row: "version 1", field: "Релиз", given: []string{"2026.2"}, value: `{"name":"2026.2"}`,
			printed: []string{"2026.2"}},
		{row: "version *", field: "Плановый спринт", given: []string{"SPR-93", "SPR-78"},
			value: `[{"name":"SPR-93"},{"name":"SPR-78"}]`, printed: []string{"SPR-78", "SPR-93"}, multi: true},
		{row: "build 1", field: "Fixed in build", given: []string{"13874"}, value: `{"name":"13874"}`,
			printed: []string{"13874"}},
		{row: "build *", field: "Сборки", given: []string{"2026.1.2"}, value: `[{"name":"2026.1.2"}]`,
			printed: []string{"2026.1.2"}, multi: true},
		{row: "ownedField 1", field: "Subsystem", given: []string{"Отчёты"}, value: `{"name":"Отчёты"}`,
			printed: []string{"Отчёты"}},
		{row: "ownedField *", field: "Подсистемы", given: []string{"Уведомления"},
			value: `[{"name":"Уведомления"}]`, printed: []string{"Уведомления"}, multi: true},
		{row: "user 1", field: "Assignee", given: []string{"admin"}, value: `{"login":"admin"}`,
			printed: []string{"admin"}},
		{row: "user *", field: "Соисполнители", given: []string{"dev.limited"},
			value: `[{"login":"dev.limited"}]`, printed: []string{"dev.limited"}, multi: true},
		{row: "group 1", field: "Группа доступа", given: []string{"Все пользователи"},
			value: `{"name":"Все пользователи"}`, printed: []string{"Все пользователи"}},
		{row: "group *", field: "Группы доступа", given: []string{"DEVELOPMENT Team", "Зарегистрированные пользователи"},
			value:   `[{"name":"DEVELOPMENT Team"},{"name":"Зарегистрированные пользователи"}]`,
			printed: []string{"DEVELOPMENT Team", "Зарегистрированные пользователи"}, multi: true},
		{row: "date", field: "Плановая дата решения", given: []string{"2026-09-17"}, value: "1789646400000",
			printed: []string{"2026-09-17"}},
		{row: "date and time", field: "Дата начала работы", given: []string{"2026-09-01T10:00:00Z"},
			value: "1788256800000", printed: []string{"2026-09-01T10:00:00Z"}},
		{row: "integer", field: "Порядок реализации", given: []string{"-1"}, value: "-1",
			printed: []string{"-1"}},
		{row: "float", field: "Коэффициент", given: []string{"0.1"}, value: "0.1", printed: []string{"0.1"}},
		{row: "string", field: "Внешний номер", given: []string{"EXT-2"}, value: `"EXT-2"`,
			printed: []string{"EXT-2"}},
		{row: "text", field: "Примечание", given: []string{rewrittenNote},
			value: `{"text":` + asJSON(rewrittenNote) + `}`, printed: []string{rewrittenNote}},
		{row: "period", field: "Оценка", given: []string{"PT0M"}, value: `{"minutes":0}`,
			printed: []string{"PT0M"}},
	}
}

type sentElement struct {
	Type  string          `json:"$type"`
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

func sentElements(t *testing.T, body string) map[string]sentElement {
	t.Helper()
	var sent struct {
		CustomFields []sentElement `json:"customFields"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &sent))
	elements := make(map[string]sentElement, len(sent.CustomFields))
	for _, element := range sent.CustomFields {
		elements[element.Name] = element
	}
	return elements
}

func creationOfEveryRow(t *testing.T, rows []liveRow) []string {
	t.Helper()
	argv := []string{"issue", "create", "DEV", "--summary", contractTitle(t),
		"--field", "Категория=Развитие технологий",
		"--field", "Модуль системы=Инфраструктура. DevOps",
		"--field", "Priority=Low",
	}
	for _, row := range rows {
		for _, value := range row.given {
			argv = append(argv, "--field", row.field+"="+value)
		}
	}
	return argv
}

func TestIssueCreateFillsEveryRowOfTheTableOfTypes(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	rows := liveRows()

	got := runWith(t, dev.env(), creationOfEveryRow(t, rows)...)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	filed := requireMapping(t, "stdout", got.stdout)
	readable := nodeAt(t, filed, "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })

	elements := sentElements(t, dev.asks()[1])
	for _, row := range rows {
		t.Run(row.row, func(t *testing.T) {
			element, sent := elements[row.field]
			require.True(t, sent, "the body carries nothing for %q", row.field)
			assert.Equal(t, row.sent, element.Type)
			assert.JSONEq(t, row.value, string(element.Value))
		})
	}

	read := runWith(t, dev.env(), "issue", "show", readable, "--comments=0", "--fields", "customFields")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	block := requireMapping(t, "stdout", read.stdout)
	for _, row := range rows {
		t.Run(row.row+" read back", func(t *testing.T) {
			if row.multi {
				assert.Equal(t, row.printed, valuesAt(t, block, "customFields", row.field))
				return
			}
			held := nodeAt(t, block, "customFields", row.field)
			assert.Equal(t, row.printed[0], held.Value)
			if row.block {
				assert.Equal(t, yaml.LiteralStyle, held.Style, "stdout: %q", read.stdout)
			}
		})
	}
}

func TestIssueUpdateNamesEveryRowTheClassTheCreationDid(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	filed := runWith(t, dev.env(), creationOfEveryRow(t, liveRows())...)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", filed.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	typedByTheTable := sentElements(t, lastAsk(dev))

	rows := rewrittenRows()
	argv := []string{"issue", "update", readable}
	for _, row := range rows {
		for _, value := range row.given {
			argv = append(argv, "--field", row.field+"="+value)
		}
	}

	got := runWith(t, dev.env(), argv...)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	typedLikeTheIssue := sentElements(t, lastAsk(dev))
	for _, row := range rows {
		t.Run(row.row, func(t *testing.T) {
			element, sent := typedLikeTheIssue[row.field]
			require.True(t, sent, "the body of the update carries nothing for %q", row.field)
			require.NotEmpty(t, typedByTheTable[row.field].Type,
				"the body of the creation carries nothing for %q", row.field)
			assert.Equal(t, typedByTheTable[row.field].Type, element.Type)
			assert.JSONEq(t, row.value, string(element.Value))
		})
	}

	read := runWith(t, dev.env(), "issue", "show", readable, "--comments=0", "--fields", "customFields")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	block := requireMapping(t, "stdout", read.stdout)
	for _, row := range rows {
		t.Run(row.row+" read back", func(t *testing.T) {
			if row.multi {
				assert.Equal(t, row.printed, valuesAt(t, block, "customFields", row.field))
				return
			}
			assert.Equal(t, row.printed[0], nodeAt(t, block, "customFields", row.field).Value)
		})
	}
}
