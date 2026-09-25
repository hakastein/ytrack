package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const (
	activityIssue  = "DEV-7"
	activitiesPath = "/api/issues/" + activityIssue + "/activities"
)

const (
	defaultActivityFields = "timestamp,author(login),category,field,added(id,idReadable,login,name,urls),removed(id,idReadable,login,name,urls)"
	sentActivityFields    = "timestamp,author(login),category(id),field(name,customField(name,fieldType(valueType)))," +
		"added(id,idReadable,login,name,urls,minutes),removed(id,idReadable,login,name,urls,minutes)"
)

const activityCategories = "AttachmentsCategory,CommentTextCategory,CommentsCategory,CustomFieldCategory," +
	"DescriptionCategory,IssueCreatedCategory,IssueResolvedCategory,LinksCategory,SummaryCategory," +
	"TagsCategory,VcsChangeCategory,WorkItemCategory"

type sentActivity struct {
	kind        string
	category    string
	categoryRaw string
	timestamp   string
	login       string
	author      string
	added       string
	removed     string
	field       string
}

const (
	sentPredefinedField = `{"$type":"PredefinedFilterField","name":"создана"}`
	sentStateField      = `{"$type":"CustomFilterField","name":"Состояние","customField":{"$type":"CustomField",` +
		`"name":"State","fieldType":{"$type":"FieldType","valueType":"state"}}}`
)

const (
	sentStateValue   = `[{"$type":"StateBundleElement","id":"156-17","name":"Duplicate"}]`
	sentStateBefore  = `[{"$type":"StateBundleElement","id":"156-18","name":"Новая"}]`
	sentLinkedIssue  = `[{"$type":"Issue","id":"3-21","idReadable":"DEV-3"}]`
	sentNothingAdded = `[]`
)

func (a sentActivity) sent() string {
	login := a.login
	if login == "" {
		login = "admin"
	}
	author := a.author
	if author == "" {
		author = `{"$type":"User","login":` + strconv.Quote(login) + `}`
	}
	category := a.categoryRaw
	if category == "" {
		category = `{"$type":"ActivityCategory","id":` + strconv.Quote(a.category) + `}`
	}
	field := a.field
	if field == "" {
		field = sentPredefinedField
	}
	added, removed := a.added, a.removed
	if added == "" {
		added = sentNothingAdded
	}
	if removed == "" {
		removed = sentNothingAdded
	}
	return `{"$type":"` + a.kind + `","id":"163-1","category":` + category + `,"timestamp":` + a.timestamp +
		`,"added":` + added + `,"removed":` + removed + `,"field":` + field + `,"author":` + author + `}`
}

func sentFieldActivity(timestamp string) string {
	return sentActivity{
		kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: timestamp,
		field: sentStateField, added: sentStateValue, removed: sentStateBefore,
	}.sent()
}

func sentLinkActivity(timestamp string) string {
	return sentActivity{
		kind: "LinksActivityItem", category: "LinksCategory", timestamp: timestamp,
		field: `{"$type":"LinkTypeFilterField","name":"Зависит от"}`, added: sentLinkedIssue,
	}.sent()
}

func sentCreatedActivity(timestamp string) string {
	return sentActivity{kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory", timestamp: timestamp}.sent()
}

const (
	printedFieldRow = `  - {timestamp: "2026-09-10T10:16:52.4Z", author: {login: "admin"}, ` +
		`category: "CustomFieldCategory", field: "State", added: [{id: "156-17", name: "Duplicate"}], ` +
		`removed: [{id: "156-18", name: "Новая"}]}` + "\n"
	printedLinkRow = `  - {timestamp: "2026-09-10T10:16:51Z", author: {login: "admin"}, ` +
		`category: "LinksCategory", field: "depends on", added: [{id: "3-21", idReadable: "DEV-3"}], removed: []}` + "\n"
	printedCreatedRow = `  - {timestamp: "2026-09-10T10:16:50.875Z", author: {login: "admin"}, ` +
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

const noActivities = `[]`

func activityServer(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, linksKnown(handler))
}

func oneRecord(printed string) string {
	return "total: 1\nreturned: 1\ntruncated: false\nactivities:\n  - {" + printed + "}\n"
}

func activityRequest(address, top string) string {
	return "GET " + address + activitiesPath + "?categories=" + activityCategories + "&reverse=true&fields=" +
		sentActivityFields + "&$top=" + top
}

type activityListing struct {
	Total      *int             `yaml:"total"`
	Returned   int              `yaml:"returned"`
	Truncated  bool             `yaml:"truncated"`
	Activities []map[string]any `yaml:"activities"`
}

func requireActivityListing(t *testing.T, got outcome) activityListing {
	t.Helper()
	assert.Empty(t, got.stderr)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed activityListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Activities, printed.Returned)
	return printed
}

func activitySent(t *testing.T, server *upstream) url.Values {
	t.Helper()
	for _, request := range server.requests() {
		if strings.HasSuffix(request.URL.Path, "/activities") {
			return request.URL.Query()
		}
	}
	require.FailNow(t, "no request reached the activities of an issue")
	return nil
}

func activities(t *testing.T, got outcome) []*yaml.Node {
	t.Helper()
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "activities").Content
}

func TestActivityTakesTheIssueAsItsOneArgument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no subcommand", argv: []string{"activity"}},
		{name: "one activity by an address of its own", argv: []string{"activity", "show", "DEV-1"}},
		{
			name: "the name the activity went by before",
			argv: []string{"issue-history", "list", "--query", "issue id: DEV-1"},
		},
		{name: "no issue at all", argv: []string{"activity", "list"}},
		{name: "two issues", argv: []string{"activity", "list", "DEV-1", "DEV-2"}},
		{name: "a search in place of the issue", argv: []string{"activity", "list", "DEV-1", "--query", "issue id: DEV-1"}},
		{name: "the readable id of an article", argv: []string{"activity", "list", "DEV-A-1"}},
		{name: "an internal id", argv: []string{"activity", "list", "3-19"}},
		{name: "a string of neither form", argv: []string{"activity", "list", "DEV"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestActivityRefusesTheFlagsOfAListItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "a limit of zero", flags: []string{"--limit", "0"}},
		{
			name:  "a limit of the largest int32, which leaves no room for the activity past it",
			flags: []string{"--limit", "2147483647"},
		},
		{name: "the limit twice", flags: []string{"--limit", "1", "--limit", "2"}},
		{name: "the fields twice", flags: []string{"--fields", "timestamp", "--fields", "category"}},
		{name: "an expression that closes nothing", flags: []string{"--fields", "timestamp(added"}},
		{name: "a name under the category", flags: []string{"--fields", "category(id)"}},
		{name: "a name under the category added to the default", flags: []string{"--fields", "+category(id)"}},
		{name: "a name under the field", flags: []string{"--fields", "field(name)"}},
		{name: "a name under the field added to the default", flags: []string{"--fields", "+field(customField(name))"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"activity", "list", activityIssue}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestActivityHelpNamesTheCategoriesAndTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"activity", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	for _, said := range append(strings.Split(activityCategories, ","), defaultActivityFields, "--category") {
		assert.Contains(t, got.stdout, said)
	}
	assert.NotContains(t, got.stdout, "--query")
	assert.NotContains(t, got.stdout, "target")
}

func TestActivitySendsTheActivitiesOfTheIssueItWasGiven(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `[`+sentCreatedActivity(middle)+`]`))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--category", "IssueCreatedCategory")

	want := "total: 1\nreturned: 1\ntruncated: false\nactivities:\n" +
		strings.Replace(printedCreatedRow, "50.875Z", "51Z", 1)
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{activitiesPath}, server.sentPaths())
	sent := activitySent(t, server)
	assert.Equal(t, []string{"IssueCreatedCategory"}, sent["categories"])
	assert.Equal(t, []string{"true"}, sent["reverse"])
	assert.Equal(t, []string{"51"}, sent["$top"])
	assert.Equal(t, []string{sentActivityFields}, sent["fields"])
	assert.NotContains(t, sent.Get("fields"), "target")
	assert.NotContains(t, sent, "issueQuery")
}

func TestActivityPrintsAnActivityToALine(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, threeActivities()))

	got := runWith(t, server.env(), "activity", "list", activityIssue)

	rows := printedFieldRow + printedLinkRow + printedCreatedRow
	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 3\ntruncated: false\nactivities:\n" + rows}, got)
	for _, record := range activities(t, got) {
		assert.Equal(t, []string{"timestamp", "author", "category", "field", "added", "removed"}, recordKeys(record))
		assert.Equal(t, yaml.DoubleQuotedStyle, nodeAt(t, record, "category").Style)
	}
	assert.Equal(t, 1, sentTo(server, activitiesPath))
	assert.NotContains(t, got.stdout, "163-1")
}

func TestActivityPrintsAListWhateverTheCountOfActivities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		activities string
		want       string
	}{
		{
			name:       "no activities at all",
			activities: noActivities,
			want:       "total: 0\nreturned: 0\ntruncated: false\nactivities: []\n",
		},
		{
			name:       "one activity",
			activities: `[` + sentLinkActivity(middle) + `]`,
			want:       "total: 1\nreturned: 1\ntruncated: false\nactivities:\n" + printedLinkRow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, respondWith(http.StatusOK, tc.activities))

			got := runWith(t, server.env(), "activity", "list", activityIssue)

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestActivityReadsTheCutOffTheActivityPastTheLimit(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, threeActivities()))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--limit", "2")

	rows := printedFieldRow + printedLinkRow
	assert.Equal(t, outcome{stdout: "total: null\nreturned: 2\ntruncated: true\nactivities:\n" + rows}, got)
	assert.Equal(t, []string{"3"}, activitySent(t, server)["$top"])
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
			past: sentActivity{kind: "VotersActivityItem", category: "VotersCategory", timestamp: oldest}.sent(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			three := `[` + sentFieldActivity(newest) + `,` + sentLinkActivity(middle) + `,` + tc.past + `]`
			server := activityServer(t, respondWith(http.StatusOK, three))

			got := runWith(t, server.env(), "activity", "list", activityIssue, "--limit", "2")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}

func TestActivityRefusesMoreActivitiesThanItAskedFor(t *testing.T) {
	t.Parallel()
	four := `[` + sentFieldActivity(newest) + `,` + sentLinkActivity(middle) + `,` +
		sentCreatedActivity(oldest) + `,` + sentCreatedActivity(oldest) + `]`
	server := activityServer(t, respondWith(http.StatusOK, four))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--limit", "2")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 2}, {"returned", 4}},
	}
	assert.Equal(t, want, requireFault(t, got))
}

func TestActivityChecksTheServerKeepsTheRequestedOrder(t *testing.T) {
	t.Parallel()
	t.Run("an activity newer than the one before it", func(t *testing.T) {
		t.Parallel()
		server := activityServer(t, respondWith(http.StatusOK, `[`+sentCreatedActivity(oldest)+`,`+sentLinkActivity(middle)+`]`))

		got := runWith(t, server.env(), "activity", "list", activityIssue)

		found := requireFault(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
	})
	t.Run("two activities of one moment", func(t *testing.T) {
		t.Parallel()
		server := activityServer(t, respondWith(http.StatusOK, `[`+sentLinkActivity(middle)+`,`+sentLinkActivity(middle)+`]`))

		got := runWith(t, server.env(), "activity", "list", activityIssue)

		want := "total: 2\nreturned: 2\ntruncated: false\nactivities:\n" + printedLinkRow + printedLinkRow
		assert.Equal(t, outcome{stdout: want}, got)
	})
}

func TestActivityRefusesAnActivityOfACategoryItDidNotAskFor(t *testing.T) {
	t.Parallel()
	voted := sentActivity{kind: "VotersActivityItem", category: "VotersCategory", timestamp: middle}.sent()
	server := activityServer(t, respondWith(http.StatusOK, `[`+sentFieldActivity(newest)+`,`+voted+`]`))

	got := runWith(t, server.env(), "activity", "list", activityIssue)

	found := requireFault(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
}

func TestActivityPassesOnAnIssueTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	const said = `{"error":"Not Found","error_description":"Entity with id DEV-7 not found"}`
	server := activityServer(t, respondWith(http.StatusNotFound, said))

	got := runWith(t, server.env(), "activity", "list", activityIssue)

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, activityRequest(server.url, "51"), detailNamed(t, found, "request"))
	assert.Equal(t, "Entity with id DEV-7 not found", detailNamed(t, found, "upstream_message"))
	assert.Equal(t, []string{linkTypesPath, activitiesPath}, server.sentPaths())
}
