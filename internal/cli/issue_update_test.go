package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const issueWriteFields = "idReadable,customFields($type,name,projectCustomField(id))," +
	"project(" + projectWriteFields + ")"

type currentField struct {
	name    string
	kind    string
	binding string
}

func (f currentField) sent() string {
	return `{"$type":` + strconv.Quote(f.kind) + `,"name":` + strconv.Quote(f.name) +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(f.binding) + `}}`
}

func issueToUpdate(readable, project string, held ...currentField) string {
	fields := make([]string, 0, len(held))
	for _, field := range held {
		fields = append(fields, field.sent())
	}
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) +
		`,"customFields":[` + strings.Join(fields, ",") + `],"project":` + project + `}`
}

func updating(t *testing.T, read, update http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, readThenUpdate(read, update))
}

func readThenUpdate(read, update http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			update(w, r)
			return
		}
		read(w, r)
	}
}

func noUpdate(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "an update reached the server", "%s %s", r.Method, r.URL)
	}
}

func updateRequest(address, readable, fields string) string {
	return "POST " + address + "/api/issues/" + readable + "?fields=" + fields
}

func TestIssueUpdateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no id", argv: []string{"issue", "update"}},
		{name: "two ids", argv: []string{"issue", "update", "DEV-1", "DEV-2"}},
		{name: "an internal id", argv: []string{"issue", "update", "3-26", "--summary", "x"}},
		{name: "nothing to write", argv: []string{"issue", "update", "DEV-1"}},
		{name: "an empty title", argv: []string{"issue", "update", "DEV-1", "--summary", ""}},
		{name: "a line feed in the title", argv: []string{"issue", "update", "DEV-1", "--summary", "a\nb"}},
		{name: "a carriage return in the prose", argv: []string{"issue", "update", "DEV-1", "--description", "a\rb"}},
		{name: "a title twice", argv: []string{"issue", "update", "DEV-1", "--summary", "a", "--summary", "b"}},
		{name: "prose twice", argv: []string{"issue", "update", "DEV-1", "--description", "a", "--description", "b"}},
		{name: "an empty prose", argv: []string{"issue", "update", "DEV-1", "--description", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueUpdateHelpNamesTheDefaultAndNoFile(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"issue", "update", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, issueShowFields)
	assert.NotContains(t, got.stdout, "-file")
}

func TestIssueUpdateNamesEachFieldTheClassItWasReceivedUnder(t *testing.T) {
	t.Parallel()
	project := projectResponse(
		writableField{id: "180-14", kind: "StateProjectCustomField", name: "State", valueType: "state",
			canBeEmpty: true, defaults: []string{"Новая"}},
		writableField{id: "180-23", name: "Причина отклонения", valueType: "enum", canBeEmpty: true},
	)
	read := issueToUpdate("DEV-1", project,
		currentField{name: "State", kind: "StateMachineIssueCustomField", binding: "180-14"})
	held := receivedFields(
		receivedField{name: "State", valueType: "state", ordinal: "1", binding: "180-14",
			value: bundleElement("Отклонена")},
		receivedField{name: "Причина отклонения", valueType: "enum", ordinal: "2", binding: "180-23",
			value: bundleElement("Дубль")},
	)
	server := updating(t, respondWith(http.StatusOK, read),
		respondWith(http.StatusOK, createdIssueWith("DEV-1", "x", "null", held)))

	got := runWith(t, server.env(), "issue", "update", "dev-1",
		"--field", "State=Отклонена", "--field", "Причина отклонения=Дубль")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/issues/dev-1", "/api/issues/DEV-1"}, server.sentPaths())
	assert.Equal(t, []string{issueWriteFields, askedIssueFields}, server.sentFields())

	want := `{"customFields":[` +
		`{"$type":"StateMachineIssueCustomField","name":"State","value":{"name":"Отклонена"}},` +
		`{"$type":"SingleEnumIssueCustomField","name":"Причина отклонения","value":{"name":"Дубль"}}]}`
	assert.JSONEq(t, want, server.asks()[1])
}

func TestIssueUpdateRefusesAnAnswerThatEmptiedWhatTheWriteFilled(t *testing.T) {
	t.Parallel()
	project := projectResponse(writableField{id: "187-2", kind: "PeriodProjectCustomField", name: "Оценка",
		valueType: "period", canBeEmpty: true})
	read := issueToUpdate("DEV-1", project,
		currentField{name: "Оценка", kind: "PeriodIssueCustomField", binding: "187-2"})
	held := receivedFields(receivedField{name: "Оценка", valueType: "period", binding: "187-2"})
	server := updating(t, respondWith(http.StatusOK, read),
		respondWith(http.StatusOK, createdIssueWith("DEV-1", "x", "null", held)))

	got := runWith(t, server.env(), "issue", "update", "DEV-1", "--field", "Оценка=PT7H")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", updateRequest(server.url, "DEV-1", askedIssueFields)},
			{"issue", "DEV-1"},
			{"mismatch", []any{[]detail{{"field", "Оценка"}, {"expected", "PT7H"}, {"actual", nil}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.JSONEq(t, `{"customFields":[{"$type":"PeriodIssueCustomField","name":"Оценка","value":{"minutes":420}}]}`,
		server.asks()[1])
}

func TestIssueUpdateRefusesTheClassesOfTheIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	project := projectResponse(writableField{id: "180-14", kind: "StateProjectCustomField", name: "State",
		valueType: "state", canBeEmpty: true})
	tests := []struct {
		name string
		held string
	}{
		{
			name: "the class of a field the issue holds",
			held: `{"$type":7,"name":"State","projectCustomField":{"$type":"ProjectCustomField","id":"180-14"}}`,
		},
		{
			name: "the binding a field of the issue stands for",
			held: `{"$type":"StateIssueCustomField","name":"State","projectCustomField":{"$type":"ProjectCustomField","id":7}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := `{"$type":"Issue","idReadable":"DEV-1","customFields":[` + tc.held + `],"project":` + project + `}`
			server := updating(t, respondWith(http.StatusOK, read), noUpdate(t))

			got := runWith(t, server.env(), "issue", "update", "DEV-1", "--field", "State=Новая")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", issueWriteFields)},
					{"upstream_status", 200},
					{"upstream_body", read},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestIssueUpdateTakesTheExpressionOfAWrite(t *testing.T) {
	t.Parallel()
	project := projectResponse(
		writableField{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-16", name: "Priority", valueType: "enum", canBeEmpty: true},
	)
	read := issueToUpdate("DEV-1", project,
		currentField{name: "Type", kind: "SingleEnumIssueCustomField", binding: "180-15"})
	held := receivedFields(
		receivedField{name: "Priority", valueType: "enum", ordinal: "2", binding: "180-16",
			value: bundleElement("Low")},
		receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15",
			value: bundleElement("Task")},
	)
	written := createdIssueWith("DEV-1", "x", "null", held)
	t.Run("the check reads more than the document prints", func(t *testing.T) {
		t.Parallel()
		server := updating(t, respondWith(http.StatusOK, read), respondWith(http.StatusOK, written))

		got := runWith(t, server.env(), "issue", "update", "DEV-1", "--summary", "x",
			"--field", "Type=Task", "--fields", "idReadable")

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Equal(t, "idReadable: \"DEV-1\"\n", got.stdout)
		assert.Equal(t, []string{issueWriteFields, "idReadable,summary," + customFieldsFields},
			server.sentFields())
	})
	t.Run("a custom field named in the expression", func(t *testing.T) {
		t.Parallel()
		server := serve(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, cataloguePath):
				respondWith(http.StatusOK, devCatalogue())(w, r)
			case r.Method == http.MethodPost:
				respondWith(http.StatusOK, written)(w, r)
			default:
				respondWith(http.StatusOK, read)(w, r)
			}
		})

		got := runWith(t, server.env(), "issue", "update", "DEV-1", "--field", "Type=Task",
			"--fields", `idReadable,customFields("приоритет")`)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Equal(t, "idReadable: \"DEV-1\"\ncustomFields:\n  \"Priority\": \"Low\"\n", got.stdout)
		assert.Equal(t, []string{"/api/issues/DEV-1", cataloguePath, "/api/issues/DEV-1"}, server.sentPaths())
		for _, query := range server.sentQueries() {
			assert.Empty(t, query["customFields"], "a write never cuts the answer down by name")
		}
	})
	t.Run("the comments of the issue", func(t *testing.T) {
		t.Parallel()
		server := serveNothing(t)

		got := runWith(t, server.env(), "issue", "update", "DEV-1", "--summary", "x",
			"--fields", "+comments(text)")

		assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		assert.Empty(t, server.requests())
	})
}

func TestIssueUpdateIsUncertainWhereTheAnswerCannotBePrinted(t *testing.T) {
	t.Parallel()
	project := projectResponse(writableField{id: "180-14", kind: "StateProjectCustomField", name: "State",
		valueType: "state", canBeEmpty: true})
	held := receivedFields(receivedField{name: "Примечание", valueType: "string", binding: "187-10", value: "42"})
	server := updating(t, respondWith(http.StatusOK, issueToUpdate("DEV-1", project)),
		respondWith(http.StatusOK, createdIssueWith("DEV-1", "x", "null", held)))

	got := runWith(t, server.env(), "issue", "update", "DEV-1", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, detail{"upstream_status", 200}, found.details[1])
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
}

func filedForUpdate(t *testing.T, dev *upstream, filled ...string) string {
	t.Helper()
	argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)
	got := runWith(t, dev.env(), append(argv, filled...)...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

func TestIssueUpdateReplacesWhatAFieldOfTheDevInstanceHeld(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev)

	got := runWith(t, dev.env(), "issue", "update", readable, "--field", "Клиент=ГАММА", "--field", "Клиент=АЛЬФА")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, readable, nodeAt(t, mapping, "idReadable").Value)
	assert.Equal(t, []string{"АЛЬФА", "ГАММА"}, valuesAt(t, mapping, "customFields", "Клиент"))
}

func TestIssueUpdateWritesTheFieldOfTheDevInstanceTheSameWriteUncovers(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev, "--field", "State=Новая")

	refused := runWith(t, dev.env(), "issue", "update", readable, "--field", "Причина отклонения=Дубль")

	found := requireFault(t, refused)
	assert.Equal(t, "rejected", found.code)
	assert.Contains(t, found.details, detail{"upstream_message", "Вы можете обновлять значение поля Причина " +
		"отклонения, только когда значение поля State равно Отклонена"})

	got := runWith(t, dev.env(), "issue", "update", readable,
		"--field", "State=Отклонена", "--field", "Причина отклонения=Дубль")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "Отклонена", nodeAt(t, mapping, "customFields", "State").Value)
	assert.Equal(t, "Дубль", nodeAt(t, mapping, "customFields", "Причина отклонения").Value)
	assert.Equal(t, "SingleEnumIssueCustomField", sentFieldTypes(t, lastAsk(dev))["Причина отклонения"])
}

func textThatSurvives() string {
	return "Шаги:  \n1. открыть   \n---\n~~~\nи ещё 😀"
}

func TestIssueUpdateWritesTheTextOfTheDevInstanceByteForByte(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev)
	title := "  " + contractTitle(t) + "\t"
	text := textThatSurvives()

	got := runWith(t, dev.env(), "issue", "update", readable, "--summary", title, "--description", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, title, nodeAt(t, mapping, "summary").Value)
	assert.Equal(t, text, nodeAt(t, mapping, "description").Value)
}

func TestIssueUpdateRefusesTheFieldTheDevInstanceComputesItself(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev)

	got := runWith(t, dev.env(), "issue", "update", readable, "--field", "Затраченное время=PT1H")

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	said, isText := detailNamed(t, found, "upstream_message").(string)
	require.True(t, isText, "upstream_message: %v", found.details)
	assert.Contains(t, said, "автоматически рассчитывается")
}
