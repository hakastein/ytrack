package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func issueToEmpty() string {
	return issueToUpdate("DEV-1", projectResponse(
		writableField{id: "180-1", name: "First", valueType: "enum"},
		writableField{id: "180-2", name: "Second", valueType: "enum", isMultiValue: true},
		writableField{id: "180-3", name: "Optional", valueType: "enum", isMultiValue: true, canBeEmpty: true},
		writableField{id: "180-4", kind: "UserProjectCustomField", name: "Single", translate: "Localized",
			valueType: "user", canBeEmpty: true},
	))
}

func requireIssueWriteRefusal(t *testing.T, got outcome) faultDocument {
	t.Helper()
	found := requireFault(t, got)
	for at, pair := range found.details {
		if pair.key != "invalid" {
			continue
		}
		entries, isList := pair.value.([]any)
		require.True(t, isList, "invalid: %v", pair.value)
		kept := make([]any, 0, len(entries))
		for _, entry := range entries {
			fields, isMap := entry.([]detail)
			require.True(t, isMap, "an entry under invalid: %v", entry)
			require.Len(t, fields, 3, "an entry under invalid: %v", entry)
			assert.Equal(t, "reason", fields[2].key)
			assert.NotEmpty(t, fields[2].value)
			kept = append(kept, fields[:2])
		}
		found.details[at].value = kept
	}
	return found
}

func TestIssueUpdateRefusesToEmptyEveryFieldTheProjectRequires(t *testing.T) {
	t.Parallel()
	server := updating(t, fake.JSON(http.StatusOK, issueToEmpty()), noUpdate(t))

	got := runWith(t, server.Env(), "issue", "update", "DEV-1",
		"--clear", "First", "--clear", "Second", "--clear", "Single")

	want := faultDocument{
		code: "missing_required",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", issueWriteFields)},
			{"project", "DEV"},
			{"missing", []any{"First", "Second"}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestIssueUpdateRefusesAFieldWrittenAndEmptiedAtOnce(t *testing.T) {
	t.Parallel()
	server := updating(t, fake.JSON(http.StatusOK, issueToEmpty()), noUpdate(t))

	got := runWith(t, server.Env(), "issue", "update", "DEV-1", "--field", "Single=first", "--clear", "localized")

	want := faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", issueWriteFields)},
			{"project", "DEV"},
			{"invalid", []any{[]detail{{"field", "Single"}, {"value", "first"}}}},
		},
	}
	assert.Equal(t, want, requireIssueWriteRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}
