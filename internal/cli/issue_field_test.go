package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func projectOfTwoFields() string {
	return projectResponse(
		writableField{id: "180-1", name: "First", translate: "Localized", valueType: "enum", canBeEmpty: true},
		writableField{id: "180-2", name: "Second", valueType: "enum", isMultiValue: true, canBeEmpty: true},
	)
}

func TestIssueCreateRefusesAValueTheFieldCannotHold(t *testing.T) {
	t.Parallel()
	server := creating(t, fake.JSON(http.StatusOK, projectOfTwoFields()), fake.Unexpected(t))

	got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "x",
		"--field", "First=Early", "--field", "localized=Late")

	want := faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", writeMetadataRequest(server.URL, "DEV")},
			{"project", "DEV"},
			{"invalid", []any{[]detail{{"field", "First"}, {"value", "Late"}}}},
		},
	}
	assert.Equal(t, want, requireIssueWriteFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}

func TestIssueWriteRefusesANameNoFieldOfTheProjectAnswersTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		read    string
		argv    []string
		request func(address string) string
	}{
		{
			name:    "a creation",
			read:    projectOfTwoFields(),
			argv:    []string{"issue", "create", "DEV", "--summary", "x", "--field", "Frist=x", "--field", "Nothing=y"},
			request: func(address string) string { return writeMetadataRequest(address, "DEV") },
		},
		{
			name:    "an update",
			read:    issueToUpdate("DEV-1", projectOfTwoFields()),
			argv:    []string{"issue", "update", "DEV-1", "--field", "Frist=x", "--clear", "Nothing"},
			request: func(address string) string { return issueRequest(address, "DEV-1", issueWriteFields) },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, readThenUpdate(fake.JSON(http.StatusOK, tc.read), fake.Unexpected(t)))

			got := runWith(t, server.Env(), tc.argv...)

			want := faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", tc.request(server.URL)},
					{"project", "DEV"},
					{"unknown", []any{
						[]detail{{"field", "Frist"}, {"nearest", []any{"First"}}},
						[]detail{{"field", "Nothing"}, {"nearest", []any{"First", "Second"}}},
					}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, server.Methods())
		})
	}
}
