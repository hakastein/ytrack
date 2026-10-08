package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const (
	activityIssue  = "DEV-7"
	activitiesPath = "/api/issues/" + activityIssue + "/activities"
	linkTypesPath  = "/api/issueLinkTypes"
)

const sentLinkTypes = `[{"$type":"IssueLinkType","sourceToTarget":"leads to","targetToSource":"follows",` +
	`"localizedSourceToTarget":"Goes before","localizedTargetToSource":"Comes after"}]`

type sentActivity struct {
	kind      string
	category  string
	timestamp string
	added     string
	removed   string
	field     string
}

func (a sentActivity) sent() string {
	return `{"$type":"` + a.kind + `","id":"1-1","category":{"$type":"ActivityCategory","id":` + strconv.Quote(a.category) +
		`},"timestamp":` + a.timestamp + `,"added":` + a.added + `,"removed":` + a.removed + `,"field":` + a.field +
		`,"author":{"$type":"User","login":"user"}}`
}

func sentCreatedActivity(timestamp string) string {
	return sentActivity{
		kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory", timestamp: timestamp,
		field: `{"$type":"PredefinedFilterField","name":"created"}`, added: `[]`, removed: `[]`,
	}.sent()
}

const printedCreatedRow = `  - {timestamp: "2026-09-10T10:16:50.875Z", author: {login: "user"}, ` +
	`category: "IssueCreatedCategory", field: null, added: [], removed: []}` + "\n"

const oldest = "1789035410875"

func activityServer(t *testing.T, handler http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == linkTypesPath {
			fake.JSON(http.StatusOK, sentLinkTypes)(w, r)
			return
		}
		handler(w, r)
	})
}

func TestActivityPrintsTheActivitiesOfTheIssueItWasGiven(t *testing.T) {
	t.Parallel()
	server := activityServer(t, fake.JSON(http.StatusOK, `[`+sentCreatedActivity(oldest)+`]`))

	got := runWith(t, envOf(server), "activity", "list", activityIssue)

	assert.Equal(t, outcome{stdout: "total: 1\nreturned: 1\ntruncated: false\nactivities:\n" + printedCreatedRow}, got)
	assert.Contains(t, server.Routes(), http.MethodGet+" "+activitiesPath)
}
