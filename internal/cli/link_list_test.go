package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestLinkListPrintsThePhrasesOfAnIssueInTheOrderReceived(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","links":[`+
		`{"$type":"IssueLink","direction":"INWARD","linkType":`+needsLinkType+`,"issuesSize":1,"issues":[`+
		`{"$type":"Issue","idReadable":"DEV-3","summary":"Third"}]},`+
		`{"$type":"IssueLink","direction":"OUTWARD","linkType":`+needsLinkType+`,"issuesSize":0,"issues":[]},`+
		`{"$type":"IssueLink","direction":"BOTH","linkType":`+tiesLinkType+`,"issuesSize":2,"issues":[`+
		`{"$type":"Issue","idReadable":"DEV-9","summary":"Ninth"},{"$type":"Issue","idReadable":"DEV-2","summary":"Second"}]}]}`))

	got := runWith(t, envOf(server), "link", "list", "DEV-1")

	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 3\ntruncated: false\nlinks:\n" +
		"  \"needs\":\n    - {idReadable: \"DEV-3\", summary: \"Third\"}\n" +
		"  \"ties\":\n    - {idReadable: \"DEV-9\", summary: \"Ninth\"}\n" +
		"    - {idReadable: \"DEV-2\", summary: \"Second\"}\n"}, got)
	assert.Contains(t, server.Routes(), "GET /api/issues/DEV-1")
}
