package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	activityIssue  = "DEV-7"
	activitiesPath = "/api/issues/" + activityIssue + "/activities"
	linkTypesPath  = "/api/issueLinkTypes"
)

const sentActivityFields = "timestamp,author(login),category(id),field(name,customField(name,fieldType(valueType)))," +
	"added(id,idReadable,login,name,urls,minutes),removed(id,idReadable,login,name,urls,minutes)"

const activityCategories = "AttachmentsCategory,CommentTextCategory,CommentsCategory,CustomFieldCategory," +
	"DescriptionCategory,IssueCreatedCategory,IssueResolvedCategory,LinksCategory,SummaryCategory," +
	"TagsCategory,VcsChangeCategory,WorkItemCategory"

const (
	linkTypesFields = "sourceToTarget,targetToSource,localizedSourceToTarget,localizedTargetToSource"
	sentLinkTypes   = `[{"$type":"IssueLinkType","sourceToTarget":"leads to","targetToSource":"follows",` +
		`"localizedSourceToTarget":"Goes before","localizedTargetToSource":"Comes after"}]`
)

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

func sentFieldActivity(timestamp string) string {
	return sentActivity{
		kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: timestamp,
		field: `{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField",` +
			`"name":"State","fieldType":{"$type":"FieldType","valueType":"state"}}}`,
		added:   `[{"$type":"StateBundleElement","id":"1-2","name":"Second"}]`,
		removed: `[{"$type":"StateBundleElement","id":"1-1","name":"First"}]`,
	}.sent()
}

func sentLinkActivity(timestamp string) string {
	return sentActivity{
		kind: "LinksActivityItem", category: "LinksCategory", timestamp: timestamp,
		field: `{"$type":"LinkTypeFilterField","name":"Comes after"}`,
		added: `[{"$type":"Issue","id":"1-3","idReadable":"DEV-3"}]`, removed: `[]`,
	}.sent()
}

func sentCreatedActivity(timestamp string) string {
	return sentActivity{
		kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory", timestamp: timestamp,
		field: `{"$type":"PredefinedFilterField","name":"created"}`, added: `[]`, removed: `[]`,
	}.sent()
}

const (
	printedFieldRow = `  - {timestamp: "2026-09-10T10:16:52.4Z", author: {login: "user"}, ` +
		`category: "CustomFieldCategory", field: "State", added: [{id: "1-2", name: "Second"}], ` +
		`removed: [{id: "1-1", name: "First"}]}` + "\n"
	printedLinkRow = `  - {timestamp: "2026-09-10T10:16:51Z", author: {login: "user"}, ` +
		`category: "LinksCategory", field: "follows", added: [{id: "1-3", idReadable: "DEV-3"}], removed: []}` + "\n"
	printedCreatedRow = `  - {timestamp: "2026-09-10T10:16:50.875Z", author: {login: "user"}, ` +
		`category: "IssueCreatedCategory", field: null, added: [], removed: []}` + "\n"
)

const (
	newest = "1789035412400"
	middle = "1789035411000"
	oldest = "1789035410875"
)

func threeActivities() string {
	return `[` + sentFieldActivity(newest) + `,` + sentLinkActivity(middle) + `,` + sentCreatedActivity(oldest) + `]`
}

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

func activityRequest(address, top string) string {
	return "GET " + address + activitiesPath + "?categories=" + activityCategories + "&reverse=true&fields=" +
		sentActivityFields + "&$top=" + top
}

func TestActivityRefusesAnExpressionThatClosesNothing(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "timestamp(added")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestActivityPrintsTheActivitiesOfTheIssueItWasGiven(t *testing.T) {
	t.Parallel()
	server := activityServer(t, fake.JSON(http.StatusOK, threeActivities()))

	got := runWith(t, server.Env(), "activity", "list", activityIssue)

	rows := printedFieldRow + printedLinkRow + printedCreatedRow
	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 3\ntruncated: false\nactivities:\n" + rows}, got)
	assert.Equal(t, []string{
		linkTypesPath + "?fields=" + linkTypesFields + "&$top=1000",
		activitiesPath + "?categories=" + activityCategories + "&reverse=true&fields=" + sentActivityFields + "&$top=51",
	}, server.Targets(t))
}

func TestActivityRefusesAnAnswerOfAnotherShape(t *testing.T) {
	t.Parallel()
	outOfOrder := `[` + sentCreatedActivity(oldest) + `,` + sentLinkActivity(middle) + `]`
	server := activityServer(t, fake.JSON(http.StatusOK, outOfOrder))

	got := runWith(t, server.Env(), "activity", "list", activityIssue)

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", activityRequest(server.URL, "51")},
			{"upstream_status", 200},
			{"upstream_body", outOfOrder},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}

func TestActivityChecksTheActivityPastTheLimitWithTheRest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		past string
	}{
		{name: "an activity past the limit newer than the one before it", past: sentCreatedActivity(newest)},
		{
			name: "an activity past the limit of a category nobody asked for",
			past: sentActivity{
				kind: "VotersActivityItem", category: "VotersCategory", timestamp: oldest,
				field: `null`, added: `[]`, removed: `[]`,
			}.sent(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			three := `[` + sentFieldActivity(newest) + `,` + sentLinkActivity(middle) + `,` + tc.past + `]`
			server := activityServer(t, fake.JSON(http.StatusOK, three))

			got := runWith(t, server.Env(), "activity", "list", activityIssue, "--limit", "2")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}
