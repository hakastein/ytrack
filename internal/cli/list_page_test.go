package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A list counted by a pass over ids, as the argv that prints it, the path its records are read from, the type
// the server sends each of them under, the key they are printed under and what every request of it carries
// beside the page.
type pagedList struct {
	argv   []string
	path   string
	schema string
	plural string
	search url.Values
}

// sent is the query of a request of the list asking for that page.
func (l pagedList) sent(page url.Values) url.Values {
	query := url.Values{"fields": {"id"}}
	for key, values := range l.search {
		query[key] = values
	}
	for key, values := range page {
		query[key] = values
	}
	return query
}

func pagedLists() []pagedList {
	return []pagedList{
		{argv: []string{"article", "list", "--query", ""}, path: "/api/articles", schema: "Article", plural: "articles", search: url.Values{"query": {""}}},
		{argv: []string{"article", "list", "--parent", "DEV-A-1"}, path: "/api/articles/DEV-A-1/childArticles", schema: "Article", plural: "articles"},
		{argv: []string{"comment", "list", "DEV-1"}, path: "/api/issues/DEV-1/comments", schema: "IssueComment", plural: "comments"},
		{argv: []string{"attachment", "list", "DEV-1"}, path: "/api/issues/DEV-1/attachments", schema: "IssueAttachment", plural: "attachments"},
		{argv: []string{"tag", "list"}, path: "/api/tags", schema: "Tag", plural: "tags"},
		{argv: []string{"time", "list", "DEV-1"}, path: "/api/issues/DEV-1/timeTracking/workItems", schema: "IssueWorkItem", plural: "workItems"},
		{argv: []string{"user", "list", "--query", ""}, path: "/api/users", schema: "User", plural: "users", search: url.Values{"query": {""}}},
		{argv: []string{"project", "list"}, path: "/api/admin/projects", schema: "Project", plural: "projects"},
	}
}

// pagedServer is the server of a collection of that many records of schema: each request is answered the part of
// them its $top and $skip ask for, the way YouTrack pages a collection.
func pagedServer(t *testing.T, schema string, records int) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		top, err := strconv.Atoi(query.Get("$top"))
		if !assert.NoError(t, err, "$top of %s", r.URL) {
			return
		}
		skip := 0
		if query.Has("$skip") {
			skip, err = strconv.Atoi(query.Get("$skip"))
			if !assert.NoError(t, err, "$skip of %s", r.URL) {
				return
			}
		}
		end := records
		if top >= 0 {
			end = min(records, skip+top)
		}
		page := make([]string, 0, records)
		for at := skip; at < end; at++ {
			page = append(page, fmt.Sprintf(`{"$type":%q,"id":"1-%d"}`, schema, at))
		}
		respondWith(http.StatusOK, "["+strings.Join(page, ",")+"]")(w, r)
	})
}

// printedIDs is the document of a list printed with --fields id: the counters, then a record to a line.
func printedIDs(plural string, total int, truncated bool, ids ...int) string {
	head := fmt.Sprintf("total: %d\nreturned: %d\ntruncated: %t\n%s:", total, len(ids), truncated, plural)
	if len(ids) == 0 {
		return head + " []\n"
	}
	var rows strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&rows, "  - {id: \"1-%d\"}\n", id)
	}
	return head + "\n" + rows.String()
}

// A page in the middle of a collection: $skip goes out on the page and not on the count, total is the whole
// collection and truncated says records stand past the page.
func TestListPrintsAPageInTheMiddleOfTheCollection(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := pagedServer(t, list.schema, 5)

			got := runWith(t, server.env(), slices.Concat(list.argv, []string{"--fields", "id", "--limit", "2", "--skip", "2"})...)

			assert.Equal(t, outcome{stdout: printedIDs(list.plural, 5, true, 2, 3)}, got)
			assert.Equal(t, []url.Values{
				list.sent(url.Values{"$top": {"2"}, "$skip": {"2"}}),
				list.sent(url.Values{"$top": {"-1"}}),
			}, server.sentQueries())
			assert.Equal(t, []string{list.path, list.path}, server.sentPaths())
		})
	}
}

// A page short of the limit ends the collection, so the records passed over and the records it holds are the
// whole of it and nothing is counted.
func TestListCountsNothingOnTheLastPage(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := pagedServer(t, list.schema, 5)

			got := runWith(t, server.env(), slices.Concat(list.argv, []string{"--fields", "id", "--limit", "2", "--skip", "4"})...)

			assert.Equal(t, outcome{stdout: printedIDs(list.plural, 5, false, 4)}, got)
			assert.Equal(t, []url.Values{list.sent(url.Values{"$top": {"2"}, "$skip": {"4"}})}, server.sentQueries())
		})
	}
}

// A skip past the end is an empty page rather than a refusal, and the empty page says nothing of how far the end
// was, so the collection is counted.
func TestListPrintsAnEmptyPagePastTheEnd(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := pagedServer(t, list.schema, 5)

			got := runWith(t, server.env(), slices.Concat(list.argv, []string{"--fields", "id", "--limit", "2", "--skip", "9"})...)

			assert.Equal(t, outcome{stdout: printedIDs(list.plural, 5, false)}, got)
			assert.Len(t, server.requests(), 2)
		})
	}
}

// A count below the records passed over and the records that arrived is the collection changing between the
// two requests.
func TestListRefusesACountBelowThePageItFollows(t *testing.T) {
	t.Parallel()
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			respondWith(http.StatusOK, `[{"$type":"Tag","id":"1-0"},{"$type":"Tag","id":"1-1"},{"$type":"Tag","id":"1-2"}]`)(w, r)
			return
		}
		respondWith(http.StatusOK, `[{"$type":"Tag","id":"1-2"},{"$type":"Tag","id":"1-3"}]`)(w, r)
	})

	got := runWith(t, server.env(), "tag", "list", "--fields", "id", "--limit", "2", "--skip", "2")

	assert.Equal(t, "upstream_failed", requireRefusal(t, got).code)
	assert.Len(t, server.requests(), 2)
}

// $skip is an int32 in the specification, and a skip it cannot carry is refused before any request, on every
// list that takes a page.
func TestListRefusesASkipItCannotSend(t *testing.T) {
	t.Parallel()
	lists := [][]string{{"issue", "list", "--query", ""}, {"activity", "list", "DEV-1"}}
	for _, list := range pagedLists() {
		lists = append(lists, list.argv)
	}
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a negative skip", argv: []string{"--skip", "-1"}},
		{name: "past the largest int32", argv: []string{"--skip", "2147483648"}},
		{name: "given twice", argv: []string{"--skip", "1", "--skip", "2"}},
	}
	for _, list := range lists {
		for _, tc := range tests {
			t.Run(strings.Join(list, " ")+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				server := serveNothing(t)

				got := runWith(t, server.env(), slices.Concat(list, tc.argv)...)

				assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
				assert.Empty(t, server.requests())
			})
		}
	}
}

// A skip of none is the first page, and the request is the one it always was.
func TestListSendsNoSkipForTheFirstPage(t *testing.T) {
	t.Parallel()
	server := pagedServer(t, "Project", 1)

	got := runWith(t, server.env(), "project", "list", "--fields", "id", "--skip", "0")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []url.Values{{"fields": {"id"}, "$top": {"50"}}}, server.sentQueries())
}

// A selection of issues is counted by the counter of the server rather than by ids, and the counter knows
// nothing of the page: $skip goes out on the page alone.
func TestIssueListPrintsAPageInTheMiddleOfTheSelection(t *testing.T) {
	t.Parallel()
	server := searching(t, countedIssues(`[`+listedDEV1()+`]`, countHandler("7")))

	got := runWith(t, server.env(), "issue", "list", "--query", "project: DEV", "--limit", "1", "--skip", "3")

	want := "total: 7\nreturned: 1\ntruncated: true\nissues:\n" + printedDEV1Row
	assert.Equal(t, outcome{stdout: want}, got)
	var skips []string
	for _, request := range server.requests() {
		if request.URL.Path == "/api/issues" {
			skips = append(skips, request.URL.Query().Get("$skip"))
		}
	}
	assert.Equal(t, []string{"3"}, skips)
	assert.Equal(t, 1, sentTo(server, countPath))
}

// The journal is cut off by the one activity past the limit, so $skip passes over the newest and $top still
// asks for one past the page.
func TestActivityPrintsAPageOfTheJournal(t *testing.T) {
	t.Parallel()
	server := journal(t, respondWith(http.StatusOK, `[`+sentLinkActivity(middle)+`,`+sentCreatedActivity(oldest)+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--limit", "1", "--skip", "1")

	assert.Equal(t, outcome{stdout: "total: null\nreturned: 1\ntruncated: true\nactivities:\n" + printedLinkRow}, got)
	sent := activitySent(t, server)
	assert.Equal(t, []string{"2"}, sent["$top"])
	assert.Equal(t, []string{"1"}, sent["$skip"])
}

// A page of the journal short of the activity past it ends the journal, so the activities passed over and the
// ones printed are the whole of it.
func TestActivityCountsTheLastPageOfTheJournal(t *testing.T) {
	t.Parallel()
	server := journal(t, respondWith(http.StatusOK, `[`+sentLinkActivity(middle)+`,`+sentCreatedActivity(oldest)+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--limit", "5", "--skip", "1")

	rows := printedLinkRow + printedCreatedRow
	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 2\ntruncated: false\nactivities:\n" + rows}, got)
}

// An empty page after a skip says nothing of where the journal ended, and the journal is counted nowhere.
func TestActivityPrintsNoTotalForAnEmptyPagePastTheEnd(t *testing.T) {
	t.Parallel()
	server := journal(t, respondWith(http.StatusOK, noActivities))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--skip", "9")

	assert.Equal(t, outcome{stdout: "total: null\nreturned: 0\ntruncated: false\nactivities: []\n"}, got)
}

func TestIssueListPrintsTheSecondPageOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", devInstanceIssues, "--fields", "idReadable", "--limit", "1", "--skip", "1")

	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 1\ntruncated: true\nissues:\n  - {idReadable: \"DEV-2\"}\n"}, got)
	assert.Equal(t, []string{"1"}, dev.sentQueries()[1]["$skip"])
}

func TestIssueListPrintsAnEmptyPagePastTheIssuesOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", devInstanceIssues, "--fields", "idReadable", "--skip", "5")

	assert.Equal(t, outcome{stdout: "total: 3\nreturned: 0\ntruncated: false\nissues: []\n"}, got)
}

func TestArticleListPrintsTheChildOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "list", "--parent", "DEV-A-1")

	assert.Equal(t, outcome{stdout: "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedChildRow}, got)
	assert.Equal(t, []string{"/api/articles/DEV-A-1/childArticles"}, dev.sentPaths())
}

func TestArticleListPrintsAnEmptyPagePastTheChildOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "list", "--parent", "DEV-A-1", "--skip", "1")

	assert.Equal(t, outcome{stdout: "total: 1\nreturned: 0\ntruncated: false\narticles: []\n"}, got)
}

// The journal takes $skip: the page past the newest three of the mixed fixture begins where the first page of
// four ended.
func TestActivityPrintsTheSecondPageOfTheJournalOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	first := runWith(t, dev.env(), "activity", "list", mixedFixture, "--fields", "timestamp", "--limit", "4")
	second := runWith(t, dev.env(), "activity", "list", mixedFixture, "--fields", "timestamp", "--limit", "1", "--skip", "3")

	whole := requireActivityListing(t, first)
	page := requireActivityListing(t, second)
	require.Len(t, whole.Activities, 4)
	require.Len(t, page.Activities, 1)
	assert.Equal(t, whole.Activities[3], page.Activities[0])
}
