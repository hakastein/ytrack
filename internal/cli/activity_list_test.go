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

// The issue the scenarios of a stub server read the journal of, and where that journal is asked for.
const (
	journalIssue   = "DEV-7"
	activitiesPath = "/api/issues/" + journalIssue + "/activities"
)

// What activity list prints by default, which is what its help names, and the expression that goes out for it:
// the category and the field arrive as objects and are printed by one name each, so the request asks for the
// members they are read out of and for the type of the custom field, which says how a bare value of one reads,
// and a value is asked for the minutes a duration is printed out of.
const (
	activityFields     = "timestamp,author(login),category,field,added(id,idReadable,login,name,urls),removed(id,idReadable,login,name,urls)"
	sentActivityFields = "timestamp,author(login),category(id),field(name,customField(name,fieldType(valueType)))," +
		"added(id,idReadable,login,name,urls,minutes),removed(id,idReadable,login,name,urls,minutes)"
)

// Every category of the table, in the order it goes out, which is the order of the code points.
const activityCategories = "AttachmentsCategory,CommentTextCategory,CommentsCategory,CustomFieldCategory," +
	"DescriptionCategory,IssueCreatedCategory,IssueResolvedCategory,LinksCategory,SummaryCategory," +
	"TagsCategory,VcsChangeCategory,WorkItemCategory"

// An activity as the server sends it: $type on every object, an id nobody asked for, and the keys in an order
// other than the one asked for. No target: the request never asks for it.
type sentActivity struct {
	kind     string
	category string
	// The whole of the category, for a scenario that sends one of another shape; the object naming the
	// identifier above otherwise.
	categoryHeld string
	// Milliseconds since the epoch, as JSON holds them.
	timestamp string
	login     string
	// The whole of the author, for a scenario that asks for names below it; the login alone otherwise.
	author string
	// What the change put there and took away, as JSON, and the field it stands for. A change of a custom field
	// names that field and the type of its values; every other category names a filter with no custom field
	// under it.
	added   string
	removed string
	field   string
}

const (
	sentPredefinedField = `{"$type":"PredefinedFilterField","name":"создана"}`
	sentStateField      = `{"$type":"CustomFilterField","name":"Состояние","customField":{"$type":"CustomField",` +
		`"name":"State","fieldType":{"$type":"FieldType","valueType":"state"}}}`
)

// The values of a change of a state field and of a link, as the server sends them: a bundle value carries the
// name it goes by and an issue the readable id, each beside the id.
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
	category := a.categoryHeld
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

// Three activities of three categories, newest first, of the shapes the polygon answers with.
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

// The three moments of those rows, newest first.
const (
	newest = "1789035412400"
	middle = "1789035411000"
	oldest = "1789035410875"
)

func threeActivities() string {
	return `[` + sentFieldActivity(newest) + `,` + sentLinkActivity(middle) + `,` + sentCreatedActivity(oldest) + `]`
}

// An empty journal, which is what the server answers an issue nothing of the categories asked for happened to.
const noActivities = `[]`

// journal is the server of a journal: it answers the link types with the five the polygon keeps and leaves
// every other request to handler, so a scenario that says nothing of them runs against one that passes.
func journal(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, linksKnown(handler))
}

// oneRecord is the document a journal of one activity prints, with nothing in the record but the names asked
// for: what a value is printed as reads off the whole document, quotes, style and all.
func oneRecord(printed string) string {
	return "total: 1\nreturned: 1\ntruncated: false\nactivities:\n  - {" + printed + "}\n"
}

// activityRequest is the request the journal of journalIssue sends, as a refusal names it.
func activityRequest(address, top string) string {
	return "GET " + address + activitiesPath + "?categories=" + activityCategories + "&reverse=true&fields=" +
		sentActivityFields + "&$top=" + top
}

// activityListing is the document activity list prints, read back. total is a pointer because a journal cut off
// at the limit prints null: YouTrack counts activities nowhere.
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

// activitySent is the query of the request the journal itself sent, which is the one past the link types a
// journal that prints a link reads first.
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

// activities is the records of a printed journal as they were printed, so a scenario can hold a value to the
// style it was written in.
func activities(t *testing.T, got outcome) []*yaml.Node {
	t.Helper()
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "activities").Content
}

// The journal hangs from one issue and has no body of its own, so its one verb is list, the issue is the one
// word it takes, and the name the journal went by before is no command at all. An article keeps no journal and
// an internal id addresses no issue, so both are refused before anything is sent.
func TestActivityTakesTheIssueAsItsOneArgument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no command of the group", argv: []string{"activity"}},
		{name: "one activity by an address of its own", argv: []string{"activity", "show", "DEV-1"}},
		{
			name: "the name the journal went by before",
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The flags of a list are the ones every list takes, save that the limit stops one short of the largest int32:
// the request asks for one activity past it.
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

			got := runWith(t, server.env(), slices.Concat([]string{"activity", "list", journalIssue}, tc.flags)...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestActivityHelpNamesTheCategoriesAndTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"activity", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	for _, said := range append(strings.Split(activityCategories, ","), activityFields, "--category") {
		assert.Contains(t, got.stdout, said)
	}
	assert.NotContains(t, got.stdout, "--query")
	assert.NotContains(t, got.stdout, "target")
}

// The journal is read at the issue it hangs from, the categories, the order and the one activity past the limit
// go out on every call, and nothing else is asked first: no search to mark up, and no link types where no link
// is printed. The target is asked for by nobody.
func TestActivitySendsTheJournalOfTheIssueItWasGiven(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, `[`+sentCreatedActivity(middle)+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--category", "IssueCreatedCategory")

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

// A record carries the keys asked for, in the order asked for, whatever order the server sent them in, and it
// is one line: the category prints as the identifier it arrived under, a value as the tree the default asks of
// it, and neither the id of the activity nor the issue it belongs to is printed at all.
func TestActivityPrintsAnActivityToALine(t *testing.T) {
	t.Parallel()
	server := journal(t, answer(http.StatusOK, threeActivities()))

	got := runWith(t, server.env(), "activity", "list", journalIssue)

	rows := printedFieldRow + printedLinkRow + printedCreatedRow
	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 3\ntruncated: false\nactivities:\n" + rows}, got)
	for _, record := range activities(t, got) {
		assert.Equal(t, []string{"timestamp", "author", "category", "field", "added", "removed"}, recordKeys(record))
		assert.Equal(t, yaml.DoubleQuotedStyle, nodeAt(t, record, "category").Style)
	}
	assert.Equal(t, 1, sentTo(server, activitiesPath))
	assert.NotContains(t, got.stdout, "163-1")
}

// The shape of the document is a function of the command and not of how many activities arrived.
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
			server := journal(t, answer(http.StatusOK, tc.activities))

			got := runWith(t, server.env(), "activity", "list", journalIssue)

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

// The one activity past the limit is what says the rest were cut off, and it says nothing about how many they
// are, so the total is null while truncated is true.
func TestActivityReadsTheCutOffTheActivityPastTheLimit(t *testing.T) {
	t.Parallel()
	server := journal(t, answer(http.StatusOK, threeActivities()))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--limit", "2")

	rows := printedFieldRow + printedLinkRow
	assert.Equal(t, outcome{stdout: "total: null\nreturned: 2\ntruncated: true\nactivities:\n" + rows}, got)
	assert.Equal(t, []string{"3"}, activitySent(t, server)["$top"])
}

// The activity past the limit came from the same answer as the rest and says as much about it, so it is judged
// with them and thrown away only afterwards: a server that stops honouring reverse or the categories says so at
// the boundary of the limit as readily as anywhere.
func TestActivityJudgesTheActivityPastTheLimitWithTheRest(t *testing.T) {
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
			server := journal(t, answer(http.StatusOK, three))

			got := runWith(t, server.env(), "activity", "list", journalIssue, "--limit", "2")

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}

// More than the limit and the one activity past it means $top went out wrong or the server ignored it, and
// nothing of what came back stands for the journal any more.
func TestActivityRefusesMoreActivitiesThanItAskedFor(t *testing.T) {
	t.Parallel()
	four := `[` + sentFieldActivity(newest) + `,` + sentLinkActivity(middle) + `,` +
		sentCreatedActivity(oldest) + `,` + sentCreatedActivity(oldest) + `]`
	server := journal(t, answer(http.StatusOK, four))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--limit", "2")

	want := refusal{
		code:    "upstream_lied",
		details: []detail{{"limit", 2}, {"returned", 4}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
}

// reverse=true is a parameter that could quietly stop being understood, and the order is checked by the data
// rather than taken on the server's word; two activities of one moment are lawful.
func TestActivityHoldsTheServerToTheOrderItAskedFor(t *testing.T) {
	t.Parallel()
	t.Run("an activity newer than the one before it", func(t *testing.T) {
		t.Parallel()
		server := journal(t, answer(http.StatusOK, `[`+sentCreatedActivity(oldest)+`,`+sentLinkActivity(middle)+`]`))

		got := runWith(t, server.env(), "activity", "list", journalIssue)

		found := requireRefusal(t, got)
		assert.Equal(t, "upstream_lied", found.code)
	})
	t.Run("two activities of one moment", func(t *testing.T) {
		t.Parallel()
		server := journal(t, answer(http.StatusOK, `[`+sentLinkActivity(middle)+`,`+sentLinkActivity(middle)+`]`))

		got := runWith(t, server.env(), "activity", "list", journalIssue)

		want := "total: 2\nreturned: 2\ntruncated: false\nactivities:\n" + printedLinkRow + printedLinkRow
		assert.Equal(t, outcome{stdout: want}, got)
	})
}

// The server answers a category it does not know with an empty journal rather than a refusal, so an activity of
// a category nobody asked for is the filter not being applied. A vote is such a category: the instance keeps
// activities of it and the table does not.
func TestActivityRefusesAnActivityOfACategoryItDidNotAskFor(t *testing.T) {
	t.Parallel()
	voted := sentActivity{kind: "VotersActivityItem", category: "VotersCategory", timestamp: middle}.sent()
	server := journal(t, answer(http.StatusOK, `[`+sentFieldActivity(newest)+`,`+voted+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue)

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_lied", found.code)
}

// An issue the instance has none of and one the token may not see are both answered 404 by the server, and that
// is the answer the caller is given: nothing of the issue is read first.
func TestActivityPassesOnAnIssueTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	const said = `{"error":"Not Found","error_description":"Entity with id DEV-7 not found"}`
	server := journal(t, answer(http.StatusNotFound, said))

	got := runWith(t, server.env(), "activity", "list", journalIssue)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, activityRequest(server.url, "51"), detailNamed(t, found, "request"))
	assert.Equal(t, "Entity with id DEV-7 not found", detailNamed(t, found, "upstream_message"))
	assert.Equal(t, []string{linkTypesPath, activitiesPath}, server.sentPaths())
}
