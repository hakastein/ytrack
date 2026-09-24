package cli_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The project the scenarios of --clear stand on: a field that holds one value, one that holds several and two
// the project lets no issue stand without.
func projectToEmpty() string {
	return projectToWrite(
		writableField{id: "180-15", name: "Type", valueType: "enum"},
		writableField{id: "180-18", name: "Клиент", valueType: "enum", isMultiValue: true},
		writableField{id: "180-20", name: "Система", valueType: "enum", isMultiValue: true, canBeEmpty: true},
		writableField{id: "180-21", kind: "UserProjectCustomField", name: "Assignee", translate: "Исполнитель",
			valueType: "user", canBeEmpty: true},
	)
}

// The issue those scenarios write into, carrying the two fields they empty, so that the class of each element
// of the body is the one the server named rather than the one the table holds.
func issueToEmpty() string {
	return issueToUpdate("DEV-1", projectToEmpty(),
		heldField{name: "Система", kind: "MultiEnumIssueCustomField", binding: "180-20"},
		heldField{name: "Assignee", kind: "SingleUserIssueCustomField", binding: "180-21"})
}

// sentProse is the description the body carried, as JSON: the prose, the null of a call that emptied it, and
// nothing where the call said nothing about it.
func sentProse(t *testing.T, body string) string {
	t.Helper()
	var sent struct {
		Description json.RawMessage `json:"description"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &sent))
	return string(sent.Description)
}

// What --clear takes is the name of a part an issue may hold nothing in. The title is no such part —
// YouTrack files no issue without one — and a part the call writes and empties both says two things at once.
func TestIssueUpdateRefusesAClearItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the title", argv: []string{"--clear", "summary"}},
		{name: "the title in another letter case", argv: []string{"--clear", "SUMMARY"}},
		{name: "a name of nothing at all", argv: []string{"--clear", ""}},
		{name: "prose written and emptied both",
			argv: []string{"--description", "первая", "--clear", "description"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"issue", "update", "DEV-1"}, tc.argv...)...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// An empty value is written the way the type holds one: null where the field holds one value, an empty
// list where it holds several — a null there is answered Field value cannot be null — and null for the prose
// of the issue, which is no custom field and goes by its own key.
func TestIssueUpdateEmptiesEachPartTheWayItsTypeHoldsNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		// The description the answer brings back, as JSON; empty stands for the null of an issue with none.
		prose  string
		body   string
		fields []arrivedField
	}{
		{
			name:   "a field that holds one value",
			argv:   []string{"--clear", "Assignee"},
			body:   `{"customFields":[{"$type":"SingleUserIssueCustomField","name":"Assignee","value":null}]}`,
			fields: []arrivedField{{name: "Assignee", valueType: "user", ordinal: "4", binding: "180-21"}},
		},
		{
			name: "a field that holds several",
			argv: []string{"--clear", "Система"},
			body: `{"customFields":[{"$type":"MultiEnumIssueCustomField","name":"Система","value":[]}]}`,
			fields: []arrivedField{{name: "Система", valueType: "enum", isMultiValue: true, ordinal: "3",
				binding: "180-20", value: "[]"}},
		},
		{
			// A field the answer carries nowhere is a field the issue holds nothing in, which is exactly what
			// the call asked for; the same absence where the call filled the field is a mismatch.
			name: "a field the answer does not carry at all",
			argv: []string{"--clear", "Assignee"},
			body: `{"customFields":[{"$type":"SingleUserIssueCustomField","name":"Assignee","value":null}]}`,
		},
		{
			name:  "the prose of the issue, named in another letter case",
			argv:  []string{"--clear", "DESCRIPTION"},
			body:  `{"description":null}`,
			prose: "null",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prose := "null"
			if tc.prose != "" {
				prose = tc.prose
			}
			written := filedIssueHolding("DEV-1", "x", prose, arrivedFields(tc.fields...))
			server := updating(t, answer(http.StatusOK, issueToEmpty()), answer(http.StatusOK, written))

			got := runWith(t, server.env(), append([]string{"issue", "update", "DEV-1"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
			assert.JSONEq(t, tc.body, server.asks()[1])
		})
	}
}

// A field the project requires is emptied by nobody, and every such field the call names is named back at
// once and before anything is written: the server would answer one of them per attempt. The fields the call
// does not name are held to nothing — an issue filed before its project required a field holds it empty to
// this day.
func TestIssueUpdateRefusesToEmptyEveryFieldTheProjectRequires(t *testing.T) {
	t.Parallel()
	server := updating(t, answer(http.StatusOK, issueToEmpty()), noUpdate(t))

	got := runWith(t, server.env(), "issue", "update", "DEV-1",
		"--clear", "Type", "--clear", "Клиент", "--clear", "Assignee")

	want := refusal{
		code: "missing_required",
		details: []detail{
			{"request", issueRequest(server.url, "DEV-1", issueWriteFields)},
			{"project", "DEV"},
			{"missing", []any{"Type", "Клиент"}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// A field the call writes a value into and empties in the same breath is two instructions about one
// field, and which of them the write would leave behind is nothing ytrack picks. The name is resolved the way
// every name is, so the two flags reach the same field whatever each of them was typed as.
func TestIssueUpdateRefusesAFieldWrittenAndEmptiedAtOnce(t *testing.T) {
	t.Parallel()
	server := updating(t, answer(http.StatusOK, issueToEmpty()), noUpdate(t))

	got := runWith(t, server.env(), "issue", "update", "DEV-1",
		"--field", "Assignee=admin", "--clear", "исполнитель")

	want := refusal{
		code: "bad_usage",
		details: []detail{
			{"request", issueRequest(server.url, "DEV-1", issueWriteFields)},
			{"project", "DEV"},
			{"invalid", []any{[]detail{{"field", "Assignee"}, {"value", "admin"},
				{"reason", "the call writes a value into the custom field and empties it both, and one write " +
					"leaves it one way"}}}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// A part the call emptied comes back holding nothing, and a part that came back holding something is the
// answer disagreeing with the write as much as a value that came back another. The write happened by then,
// which is what the exit code of such a refusal says.
func TestIssueUpdateRefusesAnAnswerThatStillHoldsWhatTheCallEmptied(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		argv     []string
		prose    string
		fields   []arrivedField
		mismatch []any
	}{
		{
			name: "a custom field that came back filled",
			argv: []string{"--clear", "Assignee"},
			fields: []arrivedField{{name: "Assignee", valueType: "user", ordinal: "4", binding: "180-21",
				value: `{"$type":"User","login":"admin"}`}},
			mismatch: []any{[]detail{{"field", "Assignee"}, {"written", nil}, {"arrived", "admin"}}},
		},
		{
			name:     "prose that came back written",
			argv:     []string{"--clear", "description"},
			prose:    `"первая"`,
			mismatch: []any{[]detail{{"field", "description"}, {"written", nil}, {"arrived", "первая"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prose := "null"
			if tc.prose != "" {
				prose = tc.prose
			}
			written := filedIssueHolding("DEV-1", "x", prose, arrivedFields(tc.fields...))
			server := updating(t, answer(http.StatusOK, issueToEmpty()), answer(http.StatusOK, written))

			got := runWith(t, server.env(), append([]string{"issue", "update", "DEV-1"}, tc.argv...)...)

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", updateRequest(server.url, "DEV-1", askedIssueFields)},
					{"issue", "DEV-1"},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
		})
	}
}

// Four parts of one issue of the polygon emptied by one call: a field that holds one user, one that holds
// several, the prose of the issue and a field of text. Each goes out as its own type holds nothing, and what
// the answer brings back holds nothing at all, which is why the block of custom fields prints none of the
// three: a field an issue holds nothing in is left out of it.
func TestIssueUpdateEmptiesThePartsOfAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev, "--description", "первая",
		"--field", "Assignee=admin", "--field", "Соисполнители=admin", "--field", "Примечание=заметка")

	got := runWith(t, dev.env(), "issue", "update", readable, "--clear", "Assignee",
		"--clear", "Соисполнители", "--clear", "description", "--clear", "Примечание")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "!!null", nodeAt(t, mapping, "description").ShortTag())
	printed := keysOf(nodeAt(t, mapping, "customFields"))
	for _, emptied := range []string{"Assignee", "Соисполнители", "Примечание"} {
		assert.NotContains(t, printed, emptied)
	}

	sent := sentElements(t, lastAsk(dev))
	assert.Equal(t, "null", string(sent["Assignee"].Value))
	assert.Equal(t, "[]", string(sent["Соисполнители"].Value))
	assert.Equal(t, "null", string(sent["Примечание"].Value))
	assert.Equal(t, "null", sentProse(t, lastAsk(dev)))
}

// A field DEV requires is emptied by nobody, and the polygon is never asked: the read before the write is
// the whole of what the refusal needs, and the write itself never goes out.
func TestIssueUpdateRefusesToEmptyAFieldTheDevProjectRequires(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev)
	before := len(dev.requests())

	got := runWith(t, dev.env(), "issue", "update", readable, "--clear", "Type")

	want := refusal{
		code: "missing_required",
		details: []detail{
			{"request", issueRequest(dev.url, readable, issueWriteFields)},
			{"project", "DEV"},
			{"missing", []any{"Type"}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	sent := dev.requests()[before:]
	require.Len(t, sent, 1)
	assert.Equal(t, http.MethodGet, sent[0].Method)
}
