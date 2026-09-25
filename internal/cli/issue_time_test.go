package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestIssueShowPrintsAnInstantOfTheIssueAndLeavesEveryOtherNumberAlone(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","created":1789035410875,"updated":1787942509046,"resolved":null,` +
		`"numberInProject":1,"attachments":[{"$type":"IssueAttachment","created":0,"size":75}]}`
	const printed = `created: "2026-09-10T10:16:50.875Z"
updated: "2026-08-28T18:41:49.046Z"
resolved: null
numberInProject: 1
attachments:
  - {created: "1970-01-01T00:00:00Z", size: 75}
`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", "created,updated,resolved,numberInProject,attachments(created,size)")

	assert.Equal(t, outcome{stdout: printed}, got)
}

func TestIssueShowPrintsAnInstantWithNoMillisecondsToSpare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
		printed  string
	}{
		{name: "the epoch itself", received: "0", printed: "1970-01-01T00:00:00Z"},
		{name: "a whole second", received: "1788134400000", printed: "2026-08-31T00:00:00Z"},
		{name: "a tenth of a second", received: "1788134400100", printed: "2026-08-31T00:00:00.1Z"},
		{name: "every millisecond", received: "1788134400123", printed: "2026-08-31T00:00:00.123Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","created":`+tc.received+`}`))

			got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "created")

			assert.Equal(t, outcome{stdout: "created: \"" + tc.printed + "\"\n"}, got)
		})
	}
}

func TestIssueShowRefusesAnInstantThatIsNoWholeNumberOfMilliseconds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "a number inside a string", body: `{"$type":"Issue","created":"1789035410875"}`},
		{name: "a fraction of a millisecond", body: `{"$type":"Issue","created":1.5}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "created")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.URL, "DEV-1", "created")},
					{"upstream_status", 200},
					{"upstream_body", tc.body},
				},
			}, requireFault(t, got))
		})
	}
}
