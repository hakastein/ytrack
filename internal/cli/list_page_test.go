package cli_test

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

type pagedList struct {
	argv              []string
	plural            string
	record            func(at int) string
	totalBeforeTheEnd string
}

func recordOf(schema string) func(at int) string {
	return func(at int) string { return fmt.Sprintf(`{"$type":%q,"id":"1-%d"}`, schema, at) }
}

func activityAt(at int) string {
	const newest = 1789035410875
	return fmt.Sprintf(`{"$type":"IssueCreatedActivityItem","id":"1-%d","timestamp":%d,`+
		`"category":{"$type":"ActivityCategory","id":"IssueCreatedCategory"}}`, at, newest-at)
}

func countedLists() []pagedList {
	return []pagedList{
		{argv: []string{"issue", "list", "--query", ""}, plural: "issues", record: recordOf("Issue")},
		{argv: []string{"article", "list", "--query", ""}, plural: "articles", record: recordOf("Article")},
		{argv: []string{"article", "list", "--parent", "DEV-A-1"}, plural: "articles", record: recordOf("Article")},
		{argv: []string{"comment", "list", "DEV-1"}, plural: "comments", record: recordOf("IssueComment")},
		{argv: []string{"attachment", "list", "DEV-1"}, plural: "attachments", record: recordOf("IssueAttachment")},
		{argv: []string{"tag", "list"}, plural: "tags", record: recordOf("Tag")},
		{argv: []string{"time", "list", "DEV-1"}, plural: "workItems", record: recordOf("IssueWorkItem")},
		{argv: []string{"user", "list", "--query", ""}, plural: "users", record: recordOf("User")},
		{argv: []string{"project", "list"}, plural: "projects", record: recordOf("Project")},
	}
}

func pagedLists() []pagedList {
	counted := countedLists()
	for at := range counted {
		counted[at].totalBeforeTheEnd = "5"
	}
	activity := pagedList{argv: []string{"activity", "list", "DEV-1"}, plural: "activities", record: activityAt,
		totalBeforeTheEnd: "null"}
	return append(counted, activity)
}

type collection struct {
	records    int
	counted    int
	ignoresTop bool
}

func servedList(t *testing.T, list pagedList, held collection) *fake.Server {
	t.Helper()
	return fake.Serve(t, fake.Searching(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == countPath {
			countHandler(strconv.Itoa(held.counted))(w, r)
			return
		}
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
		first, end := skip, held.records
		switch {
		case top == -1:
			first, end = 0, held.counted
		case !held.ignoresTop:
			end = min(held.records, skip+top)
		}
		records := []string{}
		for at := first; at < end; at++ {
			records = append(records, list.record(at))
		}
		fake.JSON(http.StatusOK, "["+strings.Join(records, ",")+"]")(w, r)
	}))
}

func countsSent(server *fake.Server) int {
	counts := 0
	for _, request := range server.Requests() {
		if request.URL.Path == countPath || request.URL.Query().Get("$top") == "-1" {
			counts++
		}
	}
	return counts
}

func printedIDs(plural, total string, truncated bool, ids ...int) string {
	head := fmt.Sprintf("total: %s\nreturned: %d\ntruncated: %t\n%s:", total, len(ids), truncated, plural)
	if len(ids) == 0 {
		return head + " []\n"
	}
	var rows strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&rows, "  - {id: \"1-%d\"}\n", id)
	}
	return head + "\n" + rows.String()
}

func paging(list pagedList, flags ...string) []string {
	return slices.Concat(list.argv, []string{"--fields", "id"}, flags)
}

func TestListRefusesAPageItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "no limit", flags: []string{"--limit", "0"}},
		{name: "a negative skip", flags: []string{"--skip", "-1"}},
	}
	for _, list := range pagedLists() {
		for _, tc := range tests {
			t.Run(strings.Join(list.argv, " ")+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				server := fake.ServeNothing(t)

				got := runWith(t, server.Env(), paging(list, tc.flags...)...)

				assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
				assert.Empty(t, server.Requests())
			})
		}
	}
}

func TestActivityRefusesALimitThatLeavesNoRoomForTheActivityPastIt(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "activity", "list", "DEV-1", "--limit", "2147483647")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestListPrintsAPageInTheMiddleOfTheCollection(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := servedList(t, list, collection{records: 5, counted: 5})

			got := runWith(t, server.Env(), paging(list, "--limit", "2", "--skip", "2")...)

			assert.Equal(t, outcome{stdout: printedIDs(list.plural, list.totalBeforeTheEnd, true, 2, 3)}, got)
		})
	}
}

func TestListCountsNothingOnTheLastPage(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := servedList(t, list, collection{records: 5, counted: 5})

			got := runWith(t, server.Env(), paging(list, "--limit", "2", "--skip", "4")...)

			assert.Equal(t, outcome{stdout: printedIDs(list.plural, "5", false, 4)}, got)
			assert.Zero(t, countsSent(server))
		})
	}
}

func TestListPrintsAnEmptyPagePastTheEnd(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := servedList(t, list, collection{records: 5, counted: 5})

			got := runWith(t, server.Env(), paging(list, "--limit", "2", "--skip", "9")...)

			assert.Equal(t, outcome{stdout: printedIDs(list.plural, list.totalBeforeTheEnd, false)}, got)
		})
	}
}

func TestListRefusesACountBelowThePageItFollows(t *testing.T) {
	t.Parallel()
	for _, list := range countedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := servedList(t, list, collection{records: 5, counted: 3})

			got := runWith(t, server.Env(), paging(list, "--limit", "2", "--skip", "2")...)

			want := faultDocument{code: "upstream_failed", details: []detail{{"total", 3}, {"returned", 2}}}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
}

func TestListRefusesMoreRecordsThanTheLimit(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := servedList(t, list, collection{records: 5, counted: 5, ignoresTop: true})

			got := runWith(t, server.Env(), paging(list, "--limit", "1")...)

			want := faultDocument{code: "upstream_invalid", details: []detail{{"limit", 1}, {"returned", 5}}}
			assert.Equal(t, want, requireFault(t, got))
			assert.Zero(t, countsSent(server))
		})
	}
}
