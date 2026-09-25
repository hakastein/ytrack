package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const linkListTarget = "idReadable,summary"

func linkListFields(target string) string {
	return "links(issues(" + target + "),direction,linkType(sourceToTarget,targetToSource),issuesSize)"
}

func TestLinkListRefusesANameUnderASlotOfATargetIssue(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "link", "list", "DEV-1", "--fields", "+links(id)")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestLinkListPrintsThePhrasesOfAnIssueInTheOrderReceived(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","links":[`+
		`{"$type":"IssueLink","direction":"INWARD","linkType":`+needsLinkType+`,"issuesSize":1,"issues":[`+
		`{"$type":"Issue","idReadable":"DEV-3","summary":"Third"}]},`+
		`{"$type":"IssueLink","direction":"OUTWARD","linkType":`+needsLinkType+`,"issuesSize":0,"issues":[]},`+
		`{"$type":"IssueLink","direction":"BOTH","linkType":`+tiesLinkType+`,"issuesSize":2,"issues":[`+
		`{"$type":"Issue","idReadable":"DEV-9","summary":"Ninth"},{"$type":"Issue","idReadable":"DEV-2","summary":"Second"}]}]}`))

	got := runWith(t, server.Env(), "link", "list", "DEV-1")

	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 3\ntruncated: false\nlinks:\n" +
		"  \"needs\":\n    - {idReadable: \"DEV-3\", summary: \"Third\"}\n" +
		"  \"ties\":\n    - {idReadable: \"DEV-9\", summary: \"Ninth\"}\n" +
		"    - {idReadable: \"DEV-2\", summary: \"Second\"}\n"}, got)
	assert.Equal(t, []string{"/api/issues/DEV-1?fields=" + linkListFields(linkListTarget)}, server.Targets(t))
}

func TestLinkListRefusesACountBelowNone(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","links":[` +
		`{"$type":"IssueLink","direction":"INWARD","linkType":` + needsLinkType + `,"issuesSize":-1,"issues":[]},` +
		`{"$type":"IssueLink","direction":"BOTH","linkType":` + tiesLinkType + `,"issuesSize":2,"issues":[` +
		`{"$type":"Issue","idReadable":"DEV-2","summary":"Second"}]}]}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "link", "list", "DEV-1")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", linkListFields(linkListTarget))},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}, requireFault(t, got))
}
