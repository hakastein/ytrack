package cli_test

import (
	"bytes"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The body that asks the counter the selection of the contract scenarios.
const countedDevInstanceIssues = `{"query":"` + devInstanceIssues + `"}`

// inTurn answers each request with the next of the handlers and every request past the last of them with that
// last one, so a scenario holds what the second answer changes as well as that there was no third.
func inTurn(handlers ...http.HandlerFunc) http.HandlerFunc {
	var mu sync.Mutex
	answered := 0
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handler := handlers[min(answered, len(handlers)-1)]
		answered++
		mu.Unlock()
		handler(w, r)
	}
}

// countedAt is where in the log of the server each request that counts stands, so a scenario can read what came
// back to that request rather than to another.
func countedAt(server *upstream) []int {
	var at []int
	for i, path := range server.sentPaths() {
		if path == countPath {
			at = append(at, i)
		}
	}
	return at
}

// A count of -1 is the counter saying it has started and has no number yet, and the question is put again
// straight away; the number that comes back is the total, and nothing of the first answer is printed.
func TestIssueListAsksTheCounterAgainWhereItWasStillCounting(t *testing.T) {
	t.Parallel()
	calls := &countCalls{}
	server := searching(t, countedIssues(`[`+listedDEV1()+`]`, calls.recordingHandler(inTurn(countHandler("-1"), countHandler("7")))))

	got := runWith(t, server.env(), "issue", "list", "--query", "project: DEV", "--limit", "1")

	want := "total: 7\nreturned: 1\ntruncated: true\nissues:\n" + printedDEV1Row
	assert.Equal(t, outcome{stdout: want}, got)
	requireMarkedUpFirst(t, server, "project: DEV")
	assert.Equal(t, 2, sentTo(server, countPath))
	asked := countCall{method: http.MethodPost, contentType: "application/json", body: `{"query":"project: DEV"}`}
	assert.Equal(t, []countCall{asked, asked}, calls.calls())
}

// The question is put again once and no further, so an answer that is still -1 the second time is a total the
// document has no number for, and whether the rest were cut off is unknown with it.
func TestIssueListPrintsNoTotalWhereTheCounterWasStillCountingTwice(t *testing.T) {
	t.Parallel()
	// A third question would be answered a number, which is what a repeat that asked until it got one would print.
	server := searching(t, countedIssues(`[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`,
		inTurn(countHandler("-1"), countHandler("-1"), countHandler("7"))))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--limit", "3")

	rows := printedDEV1Row + printedDEV2Row + printedDEV3Row
	want := "total: null\nreturned: 3\ntruncated: null\nissues:\n" + rows
	assert.Equal(t, outcome{stdout: want}, got)
	requireMarkedUpFirst(t, server, "")
	assert.Equal(t, 2, sentTo(server, countPath))
}

// Only -1 is asked again: a repeat that fails takes the command with it the way the first question would, and
// nothing is asked a third time.
func TestIssueListRefusesWhereTheRepeatOfTheCountFails(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	tests := []struct {
		name  string
		count http.HandlerFunc
	}{
		{name: "a server that failed", count: respondWith(http.StatusInternalServerError, said)},
		{name: "an answer that breaks off", count: breakOff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, countedIssues(`[`+listedDEV1()+`]`, inTurn(countHandler("-1"), tc.count)))

			got := runWith(t, server.env(), "issue", "list", "--query", "a", "--limit", "1")

			assert.Equal(t, "upstream_failed", requireRefusal(t, got).code)
			requireMarkedUpFirst(t, server, "a")
			assert.Equal(t, 2, sentTo(server, countPath))
		})
	}
}

func TestIssueListCountsTheIssuesOfTheDevInstanceBeyondTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", devInstanceIssues, "--limit", "2")

	printed := requireIssueListing(t, got)
	assert.Equal(t, 3, *printed.Total)
	assert.Equal(t, 2, printed.Returned)
	assert.True(t, *printed.Truncated)
	requireMarkedUpFirst(t, dev, devInstanceIssues)
	// Whether the counter of the instance answers -1 is a matter of what it has counted lately, so the repeat is
	// held to the answer that was recorded: it is there where the first answer carried no number, and nowhere else.
	counted := countedAt(dev)
	require.NotEmpty(t, counted)
	firstAnswer := dev.answers()[counted[0]]
	stillCounted := bytes.Contains(firstAnswer, []byte(`"count":-1`))
	assert.Equal(t, stillCounted, len(counted) == 2, "the first answer of the counter: %s", firstAnswer)
	for _, at := range counted {
		assert.Equal(t, countedDevInstanceIssues, dev.asks()[at])
	}
}

// A page that fills the limit exactly is counted too: that it holds as many issues as were asked for proves
// nothing about what is beyond it.
func TestIssueListCountsTheIssuesOfTheDevInstanceThatFillTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", devInstanceIssues, "--limit", "3")

	requireIssuesOfTheDevInstance(t, got, "DEV-1", "DEV-2", "DEV-3")
	requireMarkedUpFirst(t, dev, devInstanceIssues)
	assert.NotEmpty(t, countedAt(dev))
}
