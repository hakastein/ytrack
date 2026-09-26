package cli_test

import (
	"net/http"
	"path"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const (
	needsLinkType = `{"$type":"IssueLinkType","id":"5-1","sourceToTarget":"is needed by","targetToSource":"needs",` +
		`"localizedSourceToTarget":null,"localizedTargetToSource":null}`
	tiesLinkType = `{"$type":"IssueLinkType","id":"5-2","sourceToTarget":"ties","targetToSource":"",` +
		`"localizedSourceToTarget":null,"localizedTargetToSource":null}`
	linkSourceRead = `{"$type":"Issue","id":"3-1","idReadable":"DEV-1","links":[` +
		`{"$type":"IssueLink","id":"5-1t","direction":"INWARD","linkType":` + needsLinkType + `},` +
		`{"$type":"IssueLink","id":"5-1s","direction":"OUTWARD","linkType":` + needsLinkType + `},` +
		`{"$type":"IssueLink","id":"5-2","direction":"BOTH","linkType":` + tiesLinkType + `}]}`
	linkTargetRead = `{"$type":"Issue","id":"3-2","idReadable":"DEV-2"}`
)

func linking(t *testing.T, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method != http.MethodGet:
			write(w, r)
		case path.Base(r.URL.Path) == "DEV-1":
			fake.JSON(http.StatusOK, linkSourceRead)(w, r)
		default:
			fake.JSON(http.StatusOK, linkTargetRead)(w, r)
		}
	})
}

func TestLinkAddWritesTheLinkAndPrintsTheLinksOfTheIssue(t *testing.T) {
	t.Parallel()
	server := linking(t, fake.JSON(http.StatusOK, `{"$type":"Issue","id":"3-2","links":[`+
		`{"$type":"IssueLink","direction":"OUTWARD","linkType":`+needsLinkType+`,"issues":[`+
		`{"$type":"Issue","id":"3-1","links":[`+
		`{"$type":"IssueLink","direction":"INWARD","linkType":`+needsLinkType+`,"issuesSize":1,"issues":[`+
		`{"$type":"Issue","id":"3-2","idReadable":"DEV-2","summary":"Second"}]},`+
		`{"$type":"IssueLink","direction":"BOTH","linkType":`+tiesLinkType+`,"issuesSize":1,"issues":[`+
		`{"$type":"Issue","id":"3-4","idReadable":"DEV-4","summary":"Fourth"}]}]}]}]}`))

	got := runWith(t, envOf(server), "link", "add", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, outcome{stdout: "total: 2\nreturned: 2\ntruncated: false\nlinks:\n" +
		"  \"needs\":\n    - {idReadable: \"DEV-2\", summary: \"Second\"}\n" +
		"  \"ties\":\n    - {idReadable: \"DEV-4\", summary: \"Fourth\"}\n"}, got)
	assert.Contains(t, server.Routes(), "POST /api/issues/DEV-1/links/5-1t/issues")
}
