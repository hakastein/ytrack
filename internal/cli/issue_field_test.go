package cli_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type namedRow struct {
	name      string
	translate string
	valueType string
	isMulti   bool
	written   []string
	sent      string
}

func namedRows() []namedRow {
	return []namedRow{
		{name: "Type", translate: "Тип", valueType: "enum", written: []string{"Task"},
			sent: "SingleEnumIssueCustomField"},
		{name: "Клиент", valueType: "enum", isMulti: true, written: []string{`ООО "РОМАШКА", Москва`, "ACME"},
			sent: "MultiEnumIssueCustomField"},
		{name: "State", translate: "Состояние", valueType: "state", written: []string{"Новая"},
			sent: "StateIssueCustomField"},
		{name: "Релиз", valueType: "version", written: []string{"2026.1"},
			sent: "SingleVersionIssueCustomField"},
		{name: "Плановый спринт", valueType: "version", isMulti: true, written: []string{"SPR-92", "SPR-78"},
			sent: "MultiVersionIssueCustomField"},
		{name: "Fixed in build", valueType: "build", written: []string{"13757"},
			sent: "SingleBuildIssueCustomField"},
		{name: "Сборки", valueType: "build", isMulti: true, written: []string{"2026.1.1"},
			sent: "MultiBuildIssueCustomField"},
		{name: "Subsystem", translate: "Подсистема", valueType: "ownedField", written: []string{"Ядро"},
			sent: "SingleOwnedIssueCustomField"},
		{name: "Подсистемы", valueType: "ownedField", isMulti: true, written: []string{"Биллинг"},
			sent: "MultiOwnedIssueCustomField"},
		{name: "Assignee", translate: "Исполнитель", valueType: "user", written: []string{"admin"},
			sent: "SingleUserIssueCustomField"},
		{name: "Соисполнители", valueType: "user", isMulti: true, written: []string{"admin", "dev.limited"},
			sent: "MultiUserIssueCustomField"},
		{name: "Группа доступа", valueType: "group", written: []string{"DEVELOPMENT Team"},
			sent: "SingleGroupIssueCustomField"},
		{name: "Группы доступа", valueType: "group", isMulti: true, written: []string{"Все пользователи"},
			sent: "MultiGroupIssueCustomField"},
	}
}

func (r namedRow) nameInLowerCase() string {
	if r.translate != "" {
		return strings.ToLower(r.translate)
	}
	return strings.ToLower(r.name)
}

func (r namedRow) id() string {
	return "180-" + strconv.Itoa(len(r.name))
}

func (r namedRow) member() string {
	if r.valueType == "user" {
		return "login"
	}
	return "name"
}

func (r namedRow) element() string {
	values := make([]string, 0, len(r.written))
	for _, value := range r.written {
		values = append(values, `{`+strconv.Quote(r.member())+`:`+strconv.Quote(value)+`}`)
	}
	value := values[0]
	if r.isMulti {
		value = "[" + strings.Join(values, ",") + "]"
	}
	return `{"$type":` + strconv.Quote(r.sent) + `,"name":` + strconv.Quote(r.name) + `,"value":` + value + `}`
}

func (r namedRow) writable() writableField {
	return writableField{id: r.id(), kind: kindOfBinding(r.valueType), name: r.name, translate: r.translate,
		valueType: r.valueType, isMultiValue: r.isMulti, canBeEmpty: true}
}

func kindOfBinding(valueType string) string {
	switch valueType {
	case "state":
		return "StateProjectCustomField"
	case "user":
		return "UserProjectCustomField"
	case "group":
		return "GroupProjectCustomField"
	case "version":
		return "VersionProjectCustomField"
	case "build":
		return "BuildProjectCustomField"
	case "ownedField":
		return "OwnedProjectCustomField"
	case "period":
		return "PeriodProjectCustomField"
	case "text":
		return "TextProjectCustomField"
	case "date", "date and time", "integer", "float", "string":
		return "SimpleProjectCustomField"
	}
	return "EnumProjectCustomField"
}

func (r namedRow) received() receivedField {
	values := make([]string, 0, len(r.written))
	for _, value := range r.written {
		values = append(values, r.valueJSON(value))
	}
	held := values[0]
	if r.isMulti {
		held = "[" + strings.Join(values, ",") + "]"
	}
	return receivedField{name: r.name, valueType: r.valueType, isMultiValue: r.isMulti,
		ordinal: strconv.Itoa(len(r.name)), binding: r.id(), value: held}
}

func (r namedRow) valueJSON(value string) string {
	switch r.valueType {
	case "user":
		return `{"$type":"User","login":` + strconv.Quote(value) + `}`
	case "group":
		return `{"$type":"UserGroup","name":` + strconv.Quote(value) + `}`
	}
	return bundleElement(value)
}

func projectOfRows(rows []namedRow) string {
	fields := make([]writableField, 0, len(rows))
	for _, row := range rows {
		fields = append(fields, row.writable())
	}
	return projectResponse(fields...)
}

func issueOfRows(readable, summary string, rows []namedRow) string {
	fields := make([]receivedField, 0, len(rows))
	for _, row := range rows {
		fields = append(fields, row.received())
	}
	return createdIssueWith(readable, summary, "null", receivedFields(fields...))
}

func shuffledFlagsOfRows(rows []namedRow) []string {
	var flags []string
	for _, at := range []int{9, 2, 12, 0, 6, 11, 4, 8, 1, 10, 3, 7, 5} {
		row := rows[at]
		for _, value := range row.written {
			flags = append(flags, "--field", row.nameInLowerCase()+"="+value)
		}
	}
	return flags
}

func TestIssueCreateRefusesAFieldItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
	}{
		{name: "no equals sign", written: "Type"},
		{name: "no name before it", written: "=x"},
		{name: "the title of the issue", written: "summary=x"},
		{name: "the prose of the issue in another letter case", written: "DESCRIPTION=x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", tc.written)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueCreateWritesEveryTypeNamedByAName(t *testing.T) {
	t.Parallel()
	rows := namedRows()
	server := creating(t, respondWith(http.StatusOK, projectOfRows(rows)),
		respondWith(http.StatusOK, issueOfRows("DEV-7", "x", rows)))

	got := runWith(t, server.env(), append([]string{"issue", "create", "DEV", "--summary", "x"}, shuffledFlagsOfRows(rows)...)...)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))

	elements := make([]string, 0, len(rows))
	for _, row := range rows {
		elements = append(elements, row.element())
	}
	want := `{"project":{"id":"0-1"},"summary":"x","customFields":[` + strings.Join(elements, ",") + `]}`
	assert.JSONEq(t, want, server.asks()[1])
}

func TestIssueCreateSplitsAFieldAtTheFirstEquals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		field   string
		value   string
	}{
		{name: "a value holding a comma", written: `Клиент=ООО "РОМАШКА", Москва`, field: "Клиент",
			value: `ООО "РОМАШКА", Москва`},
		{name: "a value holding an equals sign", written: "a=b=c", field: "a", value: "b=c"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := projectResponse(
				writableField{id: "180-18", name: "Клиент", valueType: "enum", canBeEmpty: true},
				writableField{id: "180-19", name: "a", valueType: "enum", canBeEmpty: true},
			)
			held := receivedFields(receivedField{name: tc.field, valueType: "enum", binding: "180-18",
				value: bundleElement(tc.value)})
			server := creating(t, respondWith(http.StatusOK, metadata),
				respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", tc.written)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := `{"project":{"id":"0-1"},"summary":"x","customFields":[{"$type":"SingleEnumIssueCustomField",` +
				`"name":` + strconv.Quote(tc.field) + `,"value":{"name":` + strconv.Quote(tc.value) + `}}]}`
			assert.JSONEq(t, want, server.asks()[1])
		})
	}
}

func TestIssueCreateAsksForTheFieldsItChecks(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(writableField{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true})
	held := receivedFields(receivedField{name: "Type", valueType: "enum", binding: "180-15", value: bundleElement("Task")})
	server := creating(t, respondWith(http.StatusOK, metadata),
		respondWith(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-7","summary":"x","customFields":`+held+`}`))

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", "Type=Task",
		"--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "idReadable: \"DEV-7\"\n", got.stdout)
	assert.Equal(t, []string{projectWriteFields, "idReadable,summary," + customFieldsFields}, server.sentFields())
	for _, query := range server.sentQueries() {
		assert.Empty(t, query["customFields"], "a write never cuts the answer down by name")
	}
}

func TestIssueCreateChecksTheResponseAgainstTheValuesItWrote(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(
		writableField{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-18", name: "Клиент", valueType: "enum", isMultiValue: true, canBeEmpty: true},
		writableField{id: "180-21", kind: "UserProjectCustomField", name: "Assignee", valueType: "user",
			canBeEmpty: true},
	)
	client := `[` + bundleElement("АЛЬФА") + `,` + bundleElement("ACME") + `]`
	written := []string{
		"--field", "Type=task", "--field", "Клиент=ACME", "--field", "Клиент=АЛЬФА", "--field", "Клиент=acme",
		"--field", "Assignee=ADMIN",
	}
	t.Run("what the server resolved is what was written", func(t *testing.T) {
		t.Parallel()
		held := receivedFields(
			receivedField{name: "Type", valueType: "enum", binding: "180-15", value: bundleElement("Task")},
			receivedField{name: "Клиент", valueType: "enum", isMultiValue: true, binding: "180-18", value: client},
			receivedField{name: "Assignee", valueType: "user", binding: "180-21",
				value: `{"$type":"User","login":"admin"}`},
		)
		server := creating(t, respondWith(http.StatusOK, metadata),
			respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

		got := runWith(t, server.env(), append([]string{"issue", "create", "DEV", "--summary", "x"}, written...)...)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Empty(t, got.stderr)
	})
	t.Run("a set the answer holds one more value in", func(t *testing.T) {
		t.Parallel()
		held := receivedFields(
			receivedField{name: "Type", valueType: "enum", binding: "180-15", value: bundleElement("Task")},
			receivedField{name: "Клиент", valueType: "enum", isMultiValue: true, binding: "180-18",
				value: `[` + bundleElement("АЛЬФА") + `,` + bundleElement("ACME") + `,` + bundleElement("ГАММА") + `]`},
			receivedField{name: "Assignee", valueType: "user", binding: "180-21",
				value: `{"$type":"User","login":"admin"}`},
		)
		server := creating(t, respondWith(http.StatusOK, metadata),
			respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

		got := runWith(t, server.env(), append([]string{"issue", "create", "DEV", "--summary", "x"}, written...)...)

		found := requireUncertainty(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
		assert.Equal(t, []any{[]detail{
			{"field", "Клиент"},
			{"expected", []any{"ACME", "АЛЬФА", "acme"}},
			{"actual", []any{"АЛЬФА", "ACME", "ГАММА"}},
		}}, detailNamed(t, found, "mismatch"))
	})
	t.Run("a value that came back another", func(t *testing.T) {
		t.Parallel()
		held := receivedFields(
			receivedField{name: "Type", valueType: "enum", binding: "180-15", value: bundleElement("Bug")},
			receivedField{name: "Клиент", valueType: "enum", isMultiValue: true, binding: "180-18", value: client},
			receivedField{name: "Assignee", valueType: "user", binding: "180-21",
				value: `{"$type":"User","login":"admin"}`},
		)
		server := creating(t, respondWith(http.StatusOK, metadata),
			respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

		got := runWith(t, server.env(), append([]string{"issue", "create", "DEV", "--summary", "x"}, written...)...)

		want := faultDocument{
			code: "upstream_invalid",
			details: []detail{
				{"request", creationRequest(server.url, askedIssueFields)},
				{"issue", "DEV-7"},
				{"mismatch", []any{[]detail{{"field", "Type"}, {"expected", "task"}, {"actual", "Bug"}}}},
			},
		}
		assert.Equal(t, want, requireUncertainty(t, got))
	})
}

func TestIssueCreateRefusesAnEmptyResponseValueWhereTheWriteSetOne(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(
		writableField{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-18", name: "Клиент", valueType: "enum", isMultiValue: true, canBeEmpty: true},
	)
	tests := []struct {
		name     string
		held     string
		mismatch []any
	}{
		{
			name: "a value the answer sent null for",
			held: receivedFields(
				receivedField{name: "Type", valueType: "enum", binding: "180-15"},
				receivedField{name: "Клиент", valueType: "enum", isMultiValue: true, binding: "180-18",
					value: `[` + bundleElement("ACME") + `]`},
			),
			mismatch: []any{[]detail{{"field", "Type"}, {"expected", "Task"}, {"actual", nil}}},
		},
		{
			name: "a field the answer does not carry at all",
			held: receivedFields(receivedField{name: "Type", valueType: "enum", binding: "180-15",
				value: bundleElement("Task")}),
			mismatch: []any{[]detail{{"field", "Клиент"}, {"expected", []any{"ACME"}}, {"actual", nil}}},
		},
		{
			name: "a field that holds several and holds none",
			held: receivedFields(
				receivedField{name: "Type", valueType: "enum", binding: "180-15", value: bundleElement("Task")},
				receivedField{name: "Клиент", valueType: "enum", isMultiValue: true, binding: "180-18", value: "[]"},
			),
			mismatch: []any{[]detail{{"field", "Клиент"}, {"expected", []any{"ACME"}}, {"actual", []any{}}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creating(t, respondWith(http.StatusOK, metadata),
				respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", tc.held)))

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x",
				"--field", "Type=Task", "--field", "Клиент=ACME")

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, detail{"mismatch", tc.mismatch}, found.details[2])
		})
	}
}

func invalidRow(t *testing.T, found faultDocument, index int, field string, value any) {
	t.Helper()
	rows, ok := detailNamed(t, found, "invalid").([]any)
	require.True(t, ok, "invalid: %v", found.details)
	require.Greater(t, len(rows), index)
	row, ok := rows[index].([]detail)
	require.True(t, ok, "invalid[%d]: %v", index, rows[index])
	require.Len(t, row, 3)
	assert.Equal(t, detail{"field", field}, row[0])
	assert.Equal(t, detail{"value", value}, row[1])
	assert.Equal(t, "reason", row[2].key)
	assert.NotEmpty(t, row[2].value)
}

func TestIssueCreateRefusesAValueItCannotSend(t *testing.T) {
	t.Parallel()
	t.Run("a value of nothing at all", func(t *testing.T) {
		t.Parallel()
		metadata := projectResponse(writableField{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true})
		server := creating(t, respondWith(http.StatusOK, metadata), noCreation(t))

		got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", "Type=")

		found := requireFault(t, got)
		assert.Equal(t, "bad_usage", found.code)
		invalidRow(t, found, 0, "Type", "")
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
	t.Run("a type the catalogue of ytrack does not model", func(t *testing.T) {
		t.Parallel()
		metadata := projectResponse(writableField{id: "180-15", name: "Type", valueType: "quantum", canBeEmpty: true})
		server := creating(t, respondWith(http.StatusOK, metadata), noCreation(t))

		got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", "Type=Task")

		found := requireFault(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
}

func TestIssueCreateRefusesNamesAndRepeatsAfterTheMetadata(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(
		writableField{id: "180-15", name: "Type", translate: "Тип", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-18", name: "Клиент", valueType: "enum", isMultiValue: true, canBeEmpty: true},
	)
	t.Run("one field named twice", func(t *testing.T) {
		t.Parallel()
		server := creating(t, respondWith(http.StatusOK, metadata), noCreation(t))

		got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x",
			"--field", "Type=Bug", "--field", "тип=Task")

		found := requireFault(t, got)
		assert.Equal(t, "bad_usage", found.code)
		assert.Equal(t, []detail{
			{"request", writeMetadataRequest(server.url, "DEV")},
			{"project", "DEV"},
		}, found.details[:2])
		invalidRow(t, found, 0, "Type", "Task")
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
	t.Run("names no field of the project answers to", func(t *testing.T) {
		t.Parallel()
		server := creating(t, respondWith(http.StatusOK, metadata), noCreation(t))

		got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x",
			"--field", "Типп=x", "--field", "Нет=y")

		want := faultDocument{
			code: "unknown_name",
			details: []detail{
				{"request", writeMetadataRequest(server.url, "DEV")},
				{"project", "DEV"},
				{"unknown", []any{
					[]detail{{"field", "Типп"}, {"nearest", []any{"Type"}}},
					[]detail{{"field", "Нет"}, {"nearest", []any{"Type", "Клиент"}}},
				}},
			},
		}
		assert.Equal(t, want, requireFault(t, got))
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
	t.Run("a name more than one field of the project answers to", func(t *testing.T) {
		t.Parallel()
		twice := projectResponse(
			writableField{id: "180-15", name: "Type", translate: "Общее", valueType: "enum", canBeEmpty: true},
			writableField{id: "180-18", name: "Клиент", translate: "Общее", valueType: "enum", canBeEmpty: true},
		)
		server := creating(t, respondWith(http.StatusOK, twice), noCreation(t))

		got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", "общее=x")

		want := faultDocument{
			code: "unknown_name",
			details: []detail{
				{"request", writeMetadataRequest(server.url, "DEV")},
				{"project", "DEV"},
				{"ambiguous", []any{[]detail{{"field", "общее"}, {"candidates", []any{"Type", "Клиент"}}}}},
			},
		}
		assert.Equal(t, want, requireFault(t, got))
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
}

func TestIssueWriteNamesAnUnresolvedNameOnce(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(
		writableField{id: "180-15", name: "Type", translate: "Тип", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-18", name: "Клиент", valueType: "enum", isMultiValue: true, canBeEmpty: true},
	)
	ambiguous := projectResponse(
		writableField{id: "180-15", name: "Type", translate: "Общее", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-18", name: "Клиент", translate: "Общее", valueType: "enum", canBeEmpty: true},
	)
	tests := []struct {
		name     string
		project  string
		argv     []string
		key      string
		listed   []any
		requests []string
	}{
		{
			name:    "a name of no field written by two --field",
			project: metadata,
			argv: []string{"issue", "create", "DEV", "--summary", "x",
				"--field", "Типп=x", "--field", "Типп=y"},
			key:      "unknown",
			listed:   []any{[]detail{{"field", "Типп"}, {"nearest", []any{"Type"}}}},
			requests: []string{http.MethodGet},
		},
		{
			name:    "a name of more than one field written by two --field",
			project: ambiguous,
			argv: []string{"issue", "create", "DEV", "--summary", "x",
				"--field", "общее=x", "--field", "общее=y"},
			key:      "ambiguous",
			listed:   []any{[]detail{{"field", "общее"}, {"candidates", []any{"Type", "Клиент"}}}},
			requests: []string{http.MethodGet},
		},
		{
			name:     "a name of no field written by --field and --clear",
			project:  metadata,
			argv:     []string{"issue", "update", "DEV-1", "--field", "Типп=x", "--clear", "Типп"},
			key:      "unknown",
			listed:   []any{[]detail{{"field", "Типп"}, {"nearest", []any{"Type"}}}},
			requests: []string{http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := respondWith(http.StatusOK, tc.project)
			if tc.argv[1] == "update" {
				read = respondWith(http.StatusOK, issueToUpdate("DEV-1", tc.project))
			}
			server := serve(t, readThenUpdate(read, noUpdate(t)))

			got := runWith(t, server.env(), tc.argv...)

			found := requireFault(t, got)
			assert.Equal(t, "unknown_name", found.code)
			assert.Equal(t, tc.listed, detailNamed(t, found, tc.key))
			assert.Equal(t, tc.requests, sentMethods(server))
		})
	}
}

func TestIssueCreateWritesAUserByLoginAlone(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(writableField{id: "180-21", kind: "UserProjectCustomField", name: "Assignee",
		valueType: "user", canBeEmpty: true})
	loginsOfAnyForm := []string{"2-1", "me", "7fae4e41-01f8-42c0-9cc4-960c478d8a72", "Иван Иванов"}
	for _, login := range loginsOfAnyForm {
		t.Run("a login of "+strconv.Quote(login), func(t *testing.T) {
			t.Parallel()
			held := receivedFields(receivedField{name: "Assignee", valueType: "user", binding: "180-21",
				value: `{"$type":"User","login":` + strconv.Quote(login) + `}`})
			server := creating(t, respondWith(http.StatusOK, metadata),
				respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", "Assignee="+login)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := `{"project":{"id":"0-1"},"summary":"x","customFields":[{"$type":"SingleUserIssueCustomField",` +
				`"name":"Assignee","value":{"login":` + strconv.Quote(login) + `}}]}`
			assert.JSONEq(t, want, server.asks()[1])
		})
	}
	t.Run("a login the server has no user for", func(t *testing.T) {
		t.Parallel()
		said := `{"error":"","error_description":"Не существует пользователя с именем me","error_field":"value"}`
		server := creating(t, respondWith(http.StatusOK, metadata), respondWith(http.StatusBadRequest, said))

		got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--field", "Assignee=me")

		found := requireFault(t, got)
		assert.Equal(t, "rejected", found.code)
		assert.Contains(t, found.details, detail{"upstream_message", "Не существует пользователя с именем me"})
	})
}

func devRequired() []string {
	return []string{
		"--field", "Type=Task",
		"--field", "Категория=Развитие технологий",
		"--field", "Клиент=ACME",
		"--field", "Модуль системы=Инфраструктура. DevOps",
	}
}

func valuesAt(t *testing.T, mapping *yaml.Node, path ...string) []string {
	t.Helper()
	node := nodeAt(t, mapping, path...)
	require.Equal(t, yaml.SequenceNode, node.Kind, "no list stands under %v", path)
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		values = append(values, item.Value)
	}
	return values
}

func sentFieldTypes(t *testing.T, body string) map[string]string {
	t.Helper()
	var sent struct {
		CustomFields []struct {
			Type string `json:"$type"`
			Name string `json:"name"`
		} `json:"customFields"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &sent))
	types := make(map[string]string, len(sent.CustomFields))
	for _, field := range sent.CustomFields {
		types[field.Name] = field.Type
	}
	return types
}

func TestIssueCreateFillsTheFieldsOfTheDevProject(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	title := contractTitle(t)
	argv := append([]string{"issue", "create", "DEV", "--summary", title}, devRequired()...)
	argv = append(argv, "--field", "Priority=Medium", "--field", "Клиент=АЛЬФА", "--field", "State=новая",
		"--field", "Assignee=ADMIN", "--field", "Группа доступа=development team")

	got := runWith(t, dev.env(), argv...)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	readable := nodeAt(t, mapping, "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })

	assert.Equal(t, "Новая", nodeAt(t, mapping, "customFields", "State").Value)
	assert.Equal(t, "admin", nodeAt(t, mapping, "customFields", "Assignee").Value)
	assert.Equal(t, "DEVELOPMENT Team", nodeAt(t, mapping, "customFields", "Группа доступа").Value)
	assert.Equal(t, []string{"ACME", "АЛЬФА"}, valuesAt(t, mapping, "customFields", "Клиент"))

	assert.Equal(t, map[string]string{
		"Type":           "SingleEnumIssueCustomField",
		"Категория":      "SingleEnumIssueCustomField",
		"Клиент":         "MultiEnumIssueCustomField",
		"Модуль системы": "MultiEnumIssueCustomField",
		"Priority":       "SingleEnumIssueCustomField",
		"State":          "StateIssueCustomField",
		"Assignee":       "SingleUserIssueCustomField",
		"Группа доступа": "SingleGroupIssueCustomField",
	}, sentFieldTypes(t, dev.asks()[1]))
}

func TestIssueCreateRefusesAValueTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)
	const cyrillicS = "\xd1\x81"
	notInBundle := "Нужен рекве" + cyrillicS + "т на выпуск"
	argv = append(argv, "--field", "Статус разработки="+notInBundle)

	got := runWith(t, dev.env(), argv...)

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Contains(t, found.details, detail{"upstream_message",
		"Сущность типа " + notInBundle + " с указанным именем ({1}) не найдена"})
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(dev))
}

func TestIssueCreateRefusesAUserTheFieldDoesNotAllow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login string
		said  string
	}{
		{name: "a user the field does not allow", login: "dev.limited", said: "Недопустимое значение"},
		{name: "a word the server reads as a login", login: "me", said: "Не существует пользователя с именем me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)
			argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)
			argv = append(argv, "--field", "Assignee="+tc.login)

			got := runWith(t, dev.env(), argv...)

			found := requireFault(t, got)
			assert.Equal(t, "rejected", found.code)
			assert.Contains(t, found.details, detail{"upstream_message", tc.said})
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(dev))
		})
	}
}
