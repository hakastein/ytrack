package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What an update reads before it writes: the id it is addressed by, the class the server names each field the
// issue already holds by, and the project whole, which is where the names are resolved.
const issueWriteFields = "idReadable,customFields($type,name,projectCustomField(id))," +
	"project(" + projectWriteFields + ")"

// A custom field as that read sees it on the issue. Nothing of what it holds is asked for: an update writes
// values and the check of it reads them out of the answer to the write.
type heldField struct {
	name    string
	kind    string
	binding string
}

func (f heldField) sent() string {
	return `{"$type":` + strconv.Quote(f.kind) + `,"name":` + strconv.Quote(f.name) +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(f.binding) + `}}`
}

// The issue an update reads, with the project it stands in and the fields it already carries.
func issueToUpdate(readable, project string, held ...heldField) string {
	fields := make([]string, 0, len(held))
	for _, field := range held {
		fields = append(fields, field.sent())
	}
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) +
		`,"customFields":[` + strings.Join(fields, ",") + `],"project":` + project + `}`
}

// updating is the server of an update: read answers the GET that settles the id, the project and the classes,
// and update the POST that writes, so a scenario says what each half of the command was told.
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

// noUpdate stands for the request a refusal before the write must not send.
func noUpdate(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "an update reached the server", "%s %s", r.Method, r.URL)
	}
}

func updateRequest(address, readable, fields string) string {
	return "POST " + address + "/api/issues/" + readable + "?fields=" + fields
}

// What an update takes: one issue, and at least one part to write into it. Free text YouTrack would keep
// as something other than what was written never goes out, as it does not on a creation — there the write
// would have happened and the document would disagree with it.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
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

// The class of a field the issue already carries is copied off the read word for word, and a field the
// issue does not carry gets the class of the table: the server knows of a StateMachineIssueCustomField, which
// no table of ytrack's can, and where the issue carries nothing for a field there is nothing to copy. The
// write goes out to the readable id that read gave, whatever letter case the caller typed.
func TestIssueUpdateNamesEachFieldTheClassItArrivedUnder(t *testing.T) {
	t.Parallel()
	project := projectToWrite(
		writableField{id: "180-14", kind: "StateProjectCustomField", name: "State", valueType: "state",
			canBeEmpty: true, defaults: []string{"Новая"}},
		writableField{id: "180-23", name: "Причина отклонения", valueType: "enum", canBeEmpty: true},
	)
	// A state-machine workflow on the project turns State into a class of its own, and the issue carries no
	// Причина отклонения at all: a condition kept it off.
	read := issueToUpdate("DEV-1", project,
		heldField{name: "State", kind: "StateMachineIssueCustomField", binding: "180-14"})
	held := arrivedFields(
		arrivedField{name: "State", valueType: "state", ordinal: "1", binding: "180-14",
			value: bundleElement("Отклонена")},
		arrivedField{name: "Причина отклонения", valueType: "enum", ordinal: "2", binding: "180-23",
			value: bundleElement("Дубль")},
	)
	server := updating(t, answer(http.StatusOK, read),
		answer(http.StatusOK, filedIssueHolding("DEV-1", "x", "null", held)))

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

// A period goes out as the minutes it holds and nothing else: the ISO duration beside them is the id of
// the value, and a write carrying that is answered 200 while the field is left empty. What came back empty
// where the write filled it is the answer disagreeing with the write, and the issue holds it by then, which
// is what the exit code of a write that happened says.
func TestIssueUpdateRefusesAnAnswerThatEmptiedWhatTheWriteFilled(t *testing.T) {
	t.Parallel()
	project := projectToWrite(writableField{id: "187-2", kind: "PeriodProjectCustomField", name: "Оценка",
		valueType: "period", canBeEmpty: true})
	read := issueToUpdate("DEV-1", project,
		heldField{name: "Оценка", kind: "PeriodIssueCustomField", binding: "187-2"})
	held := arrivedFields(arrivedField{name: "Оценка", valueType: "period", binding: "187-2"})
	server := updating(t, answer(http.StatusOK, read),
		answer(http.StatusOK, filedIssueHolding("DEV-1", "x", "null", held)))

	got := runWith(t, server.env(), "issue", "update", "DEV-1", "--field", "Оценка=PT7H")

	want := refusal{
		code: "upstream_lied",
		details: []detail{
			{"request", updateRequest(server.url, "DEV-1", askedIssueFields)},
			{"issue", "DEV-1"},
			{"mismatch", []any{[]detail{{"field", "Оценка"}, {"written", "PT7H"}, {"arrived", nil}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.JSONEq(t, `{"customFields":[{"$type":"PeriodIssueCustomField","name":"Оценка","value":{"minutes":420}}]}`,
		server.asks()[1])
}

// The class the server named a field the issue already holds by goes into the body word for word, so it is
// held to its shape before anything is sent: read as an empty string, it would be a $type of nobody's.
func TestIssueUpdateRefusesTheClassesOfTheIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	project := projectToWrite(writableField{id: "180-14", kind: "StateProjectCustomField", name: "State",
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
			server := updating(t, answer(http.StatusOK, read), noUpdate(t))

			got := runWith(t, server.env(), "issue", "update", "DEV-1", "--field", "State=Новая")

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", issueWriteFields)},
					{"upstream_status", 200},
					{"upstream_body", read},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// The expression of an update is the expression of a creation: the check of the write reads every value that
// went out whatever the caller asked to print, a name of a custom field is picked out of the answer rather
// than by the server, and the comments are no part of a write.
func TestIssueUpdateTakesTheExpressionOfAWrite(t *testing.T) {
	t.Parallel()
	project := projectToWrite(
		writableField{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-16", name: "Priority", valueType: "enum", canBeEmpty: true},
	)
	read := issueToUpdate("DEV-1", project,
		heldField{name: "Type", kind: "SingleEnumIssueCustomField", binding: "180-15"})
	held := arrivedFields(
		arrivedField{name: "Priority", valueType: "enum", ordinal: "2", binding: "180-16",
			value: bundleElement("Low")},
		arrivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15",
			value: bundleElement("Task")},
	)
	written := filedIssueHolding("DEV-1", "x", "null", held)
	t.Run("the check reads more than the document prints", func(t *testing.T) {
		t.Parallel()
		server := updating(t, answer(http.StatusOK, read), answer(http.StatusOK, written))

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
				answer(http.StatusOK, devCatalogue())(w, r)
			case r.Method == http.MethodPost:
				answer(http.StatusOK, written)(w, r)
			default:
				answer(http.StatusOK, read)(w, r)
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

		assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
		assert.Empty(t, server.requests())
	})
}

// The write of an update is as much a write as a creation, and the document of it is raised after the same
// 2xx: the issue holds the title by then, whatever the block of custom fields came back as.
func TestIssueUpdateIsUncertainWhereTheAnswerCannotBePrinted(t *testing.T) {
	t.Parallel()
	project := projectToWrite(writableField{id: "180-14", kind: "StateProjectCustomField", name: "State",
		valueType: "state", canBeEmpty: true})
	held := arrivedFields(arrivedField{name: "Примечание", valueType: "string", binding: "187-10", value: "42"})
	server := updating(t, answer(http.StatusOK, issueToUpdate("DEV-1", project)),
		answer(http.StatusOK, filedIssueHolding("DEV-1", "x", "null", held)))

	got := runWith(t, server.env(), "issue", "update", "DEV-1", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Equal(t, detail{"upstream_status", 200}, found.details[1])
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
}

// filedForUpdate files an issue of the polygon for a scenario that writes into it, filled with what DEV
// requires and with filled besides. The cleanup runs before the recorder's own, so the cassette records the
// deletion; the context of the test is cancelled before any cleanup, so removeIssue gets one of its own.
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

// The values a field holds are replaced by the ones the call writes rather than added to: the issue was
// filed holding ACME and the update names two others, so ACME is gone. The server answers in the order of
// the bundle, which is neither the order of the flags nor anything the call said.
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

// A field a condition hides is the server's to refuse, and it refuses word for word; the same write that
// uncovers it writes it. It is also the one live place the table settles a class on an update: the read before
// the write found the field nowhere on the issue, so there was no class of the server's to copy.
func TestIssueUpdateWritesTheFieldOfTheDevInstanceTheSameWriteUncovers(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev, "--field", "State=Новая")

	refused := runWith(t, dev.env(), "issue", "update", readable, "--field", "Причина отклонения=Дубль")

	found := requireRefusal(t, refused)
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

// Prose holding what a description keeps byte for byte: trailing spaces, lines of three dashes and three
// tildes, a character outside the basic plane, and no line ending at the end of it.
func proseThatSurvives() string {
	return "Шаги:  \n1. открыть   \n---\n~~~\nи ещё 😀"
}

// The title and the prose reach the polygon as they were typed and come back the same: spaces at the
// ends of a title and a tab in it are kept, and so is everything a description keeps. Nothing here is
// asserted twice — the check of the write is what would refuse a byte that changed.
func TestIssueUpdateWritesTheTextOfTheDevInstanceByteForByte(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev)
	title := "  " + contractTitle(t) + "\t"
	prose := proseThatSurvives()

	got := runWith(t, dev.env(), "issue", "update", readable, "--summary", title, "--description", prose)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, title, nodeAt(t, mapping, "summary").Value)
	assert.Equal(t, prose, nodeAt(t, mapping, "description").Value)
}

// A field the instance computes for itself takes no value from anybody, and what it says about that
// passes on word for word.
func TestIssueUpdateRefusesTheFieldTheDevInstanceComputesItself(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev)

	got := runWith(t, dev.env(), "issue", "update", readable, "--field", "Затраченное время=PT1H")

	found := requireRefusal(t, got)
	assert.Equal(t, "rejected", found.code)
	said, isText := detailNamed(t, found, "upstream_message").(string)
	require.True(t, isText, "upstream_message: %v", found.details)
	assert.Contains(t, said, "автоматически рассчитывается")
}
