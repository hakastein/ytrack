package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func projectToEmpty() string {
	return projectResponse(
		writableField{id: "180-15", name: "Type", valueType: "enum"},
		writableField{id: "180-18", name: "Клиент", valueType: "enum", isMultiValue: true},
		writableField{id: "180-20", name: "Система", valueType: "enum", isMultiValue: true, canBeEmpty: true},
		writableField{id: "180-21", kind: "UserProjectCustomField", name: "Assignee", translate: "Исполнитель",
			valueType: "user", canBeEmpty: true},
	)
}

func issueToEmpty() string {
	return issueToUpdate("DEV-1", projectToEmpty(),
		currentField{name: "Система", kind: "MultiEnumIssueCustomField", binding: "180-20"},
		currentField{name: "Assignee", kind: "SingleUserIssueCustomField", binding: "180-21"})
}

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
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"issue", "update", "DEV-1"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestIssueUpdateEmptiesEachPartTheWayItsTypeStoresNoValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		argv        []string
		description string
		body        string
		fields      []receivedField
	}{
		{
			name:   "a field that holds one value",
			argv:   []string{"--clear", "Assignee"},
			body:   `{"customFields":[{"$type":"SingleUserIssueCustomField","name":"Assignee","value":null}]}`,
			fields: []receivedField{{name: "Assignee", valueType: "user", ordinal: "4", binding: "180-21"}},
		},
		{
			name: "a field that holds several",
			argv: []string{"--clear", "Система"},
			body: `{"customFields":[{"$type":"MultiEnumIssueCustomField","name":"Система","value":[]}]}`,
			fields: []receivedField{{name: "Система", valueType: "enum", isMultiValue: true, ordinal: "3",
				binding: "180-20", value: "[]"}},
		},
		{
			name: "a field the answer does not carry at all",
			argv: []string{"--clear", "Assignee"},
			body: `{"customFields":[{"$type":"SingleUserIssueCustomField","name":"Assignee","value":null}]}`,
		},
		{
			name:        "the prose of the issue, named in another letter case",
			argv:        []string{"--clear", "DESCRIPTION"},
			body:        `{"description":null}`,
			description: "null",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text := "null"
			if tc.description != "" {
				text = tc.description
			}
			written := createdIssueWith("DEV-1", "x", text, receivedFields(tc.fields...))
			server := updating(t, fake.JSON(http.StatusOK, issueToEmpty()), fake.JSON(http.StatusOK, written))

			got := runWith(t, server.Env(), append([]string{"issue", "update", "DEV-1"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
			assert.JSONEq(t, tc.body, server.Bodies()[1])
		})
	}
}

func TestIssueUpdateRefusesToEmptyEveryFieldTheProjectRequires(t *testing.T) {
	t.Parallel()
	server := updating(t, fake.JSON(http.StatusOK, issueToEmpty()), noUpdate(t))

	got := runWith(t, server.Env(), "issue", "update", "DEV-1",
		"--clear", "Type", "--clear", "Клиент", "--clear", "Assignee")

	want := faultDocument{
		code: "missing_required",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", issueWriteFields)},
			{"project", "DEV"},
			{"missing", []any{"Type", "Клиент"}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestIssueUpdateRefusesAFieldWrittenAndEmptiedAtOnce(t *testing.T) {
	t.Parallel()
	server := updating(t, fake.JSON(http.StatusOK, issueToEmpty()), noUpdate(t))

	got := runWith(t, server.Env(), "issue", "update", "DEV-1",
		"--field", "Assignee=admin", "--clear", "исполнитель")

	want := faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", issueWriteFields)},
			{"project", "DEV"},
			{"invalid", []any{[]detail{{"field", "Assignee"}, {"value", "admin"},
				{"reason", "the call writes a value into the custom field and empties it both, and one write " +
					"leaves it one way"}}}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestIssueUpdateRefusesAResponseThatStillHasWhatTheCallEmptied(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		argv        []string
		description string
		fields      []receivedField
		mismatch    []any
	}{
		{
			name: "a custom field that came back filled",
			argv: []string{"--clear", "Assignee"},
			fields: []receivedField{{name: "Assignee", valueType: "user", ordinal: "4", binding: "180-21",
				value: `{"$type":"User","login":"admin"}`}},
			mismatch: []any{[]detail{{"field", "Assignee"}, {"expected", nil}, {"actual", "admin"}}},
		},
		{
			name:        "prose that came back written",
			argv:        []string{"--clear", "description"},
			description: `"первая"`,
			mismatch:    []any{[]detail{{"field", "description"}, {"expected", nil}, {"actual", "первая"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text := "null"
			if tc.description != "" {
				text = tc.description
			}
			written := createdIssueWith("DEV-1", "x", text, receivedFields(tc.fields...))
			server := updating(t, fake.JSON(http.StatusOK, issueToEmpty()), fake.JSON(http.StatusOK, written))

			got := runWith(t, server.Env(), append([]string{"issue", "update", "DEV-1"}, tc.argv...)...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", updateRequest(server.URL, "DEV-1", askedIssueFields)},
					{"issue", "DEV-1"},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
		})
	}
}
