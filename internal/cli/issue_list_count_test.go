package cli_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

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

func countedAt(server *fake.Server) []int {
	var at []int
	for i, path := range server.Paths() {
		if path == countPath {
			at = append(at, i)
		}
	}
	return at
}

const stillCounting = "-1"

func TestIssueListAsksTheCounterAgainWhereItWasStillCounting(t *testing.T) {
	t.Parallel()
	calls := &countCalls{}
	counter := calls.recordingHandler(inTurn(countHandler(stillCounting), countHandler("7")))
	server := fake.Serve(t, fake.Searching(t, countedIssues(`[`+listedDEV1()+`]`, counter)))

	got := runWith(t, server.Env(), "issue", "list", "--query", "project: DEV", "--limit", "1")

	want := "total: 7\nreturned: 1\ntruncated: true\nissues:\n" + printedDEV1Row
	assert.Equal(t, outcome{stdout: want}, got)
	requireMarkedUpFirst(t, server, "project: DEV")
	assert.Equal(t, 2, sentTo(server, countPath))
	asked := countCall{method: http.MethodPost, contentType: "application/json", body: `{"query":"project: DEV"}`}
	assert.Equal(t, []countCall{asked, asked}, calls.calls())
}

func TestIssueListPrintsNoTotalWhereTheCounterWasStillCountingTwice(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Searching(t, countedIssues(`[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`,
		inTurn(countHandler(stillCounting), countHandler(stillCounting), countHandler("7")))))

	got := runWith(t, server.Env(), "issue", "list", "--query", "", "--limit", "3")

	rows := printedDEV1Row + printedDEV2Row + printedDEV3Row
	want := "total: null\nreturned: 3\ntruncated: null\nissues:\n" + rows
	assert.Equal(t, outcome{stdout: want}, got)
	requireMarkedUpFirst(t, server, "")
	assert.Equal(t, 2, sentTo(server, countPath))
}

func TestIssueListRefusesWhereTheRepeatOfTheCountFails(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	tests := []struct {
		name  string
		count http.HandlerFunc
	}{
		{name: "a server that failed", count: fake.JSON(http.StatusInternalServerError, said)},
		{name: "an answer that breaks off", count: breakOff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, countedIssues(`[`+listedDEV1()+`]`, inTurn(countHandler(stillCounting), tc.count))))

			got := runWith(t, server.Env(), "issue", "list", "--query", "a", "--limit", "1")

			assert.Equal(t, "upstream_failed", requireFault(t, got).code)
			requireMarkedUpFirst(t, server, "a")
			assert.Equal(t, 2, sentTo(server, countPath))
		})
	}
}
