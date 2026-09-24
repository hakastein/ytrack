package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The class of an int64 belongs to its name, so created is an instant wherever it stands and size and
// numberInProject are the numbers they arrived as.
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
	server := serve(t, answer(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", "created,updated,resolved,numberInProject,attachments(created,size)")

	assert.Equal(t, outcome{stdout: printed}, got)
}

// The fraction stands for the milliseconds that are there and for no more: a whole second carries none at all.
func TestIssueShowPrintsAnInstantWithNoMillisecondsToSpare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrived string
		printed string
	}{
		{name: "the epoch itself", arrived: "0", printed: "1970-01-01T00:00:00Z"},
		{name: "a whole second", arrived: "1788134400000", printed: "2026-08-31T00:00:00Z"},
		{name: "a tenth of a second", arrived: "1788134400100", printed: "2026-08-31T00:00:00.1Z"},
		{name: "every millisecond", arrived: "1788134400123", printed: "2026-08-31T00:00:00.123Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(http.StatusOK, `{"$type":"Issue","created":`+tc.arrived+`}`))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "created")

			assert.Equal(t, outcome{stdout: "created: \"" + tc.printed + "\"\n"}, got)
		})
	}
}

// Printing an instant the server wrote some other way would invent a moment, so the answer is refused instead.
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
			server := serve(t, answer(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "created")

			assert.Equal(t, refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", "created")},
					{"upstream_status", 200},
					{"upstream_body", tc.body},
				},
			}, requireRefusal(t, got))
		})
	}
}
