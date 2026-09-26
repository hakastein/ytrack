package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const (
	issuesPath = "/api/issues"
	countPath  = "/api/issuesGetter/count"
)

const foundDEV1AndDEV2 = `[{"$type":"Issue","idReadable":"DEV-1"},{"$type":"Issue","idReadable":"DEV-2"}]`

const printedDEV1AndDEV2 = "total: 2\nreturned: 2\ntruncated: false\nissues:\n" +
	`  - {idReadable: "DEV-1"}` + "\n" + `  - {idReadable: "DEV-2"}` + "\n"

func countHandler(count string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, `{"$type":"IssueCountResponse","count":`+count+`}`)
}

func TestIssueListRefusesACallWithoutAQueryBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, envOf(server), "issue", "list")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestIssueListPrintsTheIssuesTheSearchFinds(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, foundDEV1AndDEV2)))

	got := runWith(t, envOf(server), "issue", "list", "--query", " project: DEV ", "--fields", "idReadable")

	assert.Equal(t, outcome{stdout: printedDEV1AndDEV2}, got)
	sent := server.Last(t)
	assert.Equal(t, http.MethodGet, sent.Method)
	assert.Equal(t, issuesPath, sent.URL.Path)
	assert.Equal(t, " project: DEV ", sent.URL.Query().Get("query"))
}
