package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestLinkListPrintsTheLinksOfAnIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","links":[`+
		`{"$type":"IssueLink","direction":"INWARD","linkType":`+needsLinkType+`,"issuesSize":1,"issues":[`+
		`{"$type":"Issue","idReadable":"DEV-3","summary":"Third"}]}]}`))

	got := runWith(t, envOf(server), "link", "list", "DEV-1")

	assert.Equal(t, outcome{stdout: "total: 1\nreturned: 1\ntruncated: false\nlinks:\n" +
		"  \"needs\":\n    - {idReadable: \"DEV-3\", summary: \"Third\"}\n"}, got)
	assert.Contains(t, server.Routes(), "GET /api/issues/DEV-1")
}
