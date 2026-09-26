package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pagedList struct {
	argv   []string
	record func(at int) string
	top    string
}

func recordOf(schema string) func(at int) string {
	return func(at int) string { return fmt.Sprintf(`{"$type":%q,"id":"1-%d"}`, schema, at) }
}

func activityAt(at int) string {
	const newest = 1789035410875
	return fmt.Sprintf(`{"$type":"IssueCreatedActivityItem","id":"1-%d","timestamp":%d,`+
		`"category":{"$type":"ActivityCategory","id":"IssueCreatedCategory"}}`, at, newest-at)
}

func pagedLists() []pagedList {
	return []pagedList{
		{argv: []string{"issue", "list", "--query", ""}, record: recordOf("Issue"), top: "2"},
		{argv: []string{"article", "list", "--query", ""}, record: recordOf("Article"), top: "2"},
		{argv: []string{"article", "list", "--parent", "DEV-A-1"}, record: recordOf("Article"), top: "2"},
		{argv: []string{"comment", "list", "DEV-1"}, record: recordOf("IssueComment"), top: "2"},
		{argv: []string{"attachment", "list", "DEV-1"}, record: recordOf("IssueAttachment"), top: "2"},
		{argv: []string{"tag", "list"}, record: recordOf("Tag"), top: "2"},
		{argv: []string{"time", "list", "DEV-1"}, record: recordOf("IssueWorkItem"), top: "2"},
		{argv: []string{"user", "list", "--query", ""}, record: recordOf("User"), top: "2"},
		{argv: []string{"project", "list"}, record: recordOf("Project"), top: "2"},
		{argv: []string{"activity", "list", "DEV-1"}, record: activityAt, top: "3"},
	}
}

const heldRecords = 5

func servedList(t *testing.T, list pagedList) *fake.Server {
	t.Helper()
	return fake.Serve(t, fake.Searching(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == countPath {
			countHandler(strconv.Itoa(heldRecords))(w, r)
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
		first, end := skip, min(heldRecords, skip+top)
		if top == -1 {
			first, end = 0, heldRecords
		}
		records := []string{}
		for at := first; at < end; at++ {
			records = append(records, list.record(at))
		}
		fake.JSON(http.StatusOK, "["+strings.Join(records, ",")+"]")(w, r)
	}))
}

func windowsSent(server *fake.Server) []url.Values {
	windows := []url.Values{}
	for _, query := range server.Queries() {
		if query.Has("$top") {
			windows = append(windows, url.Values{"$top": query["$top"], "$skip": query["$skip"]})
		}
	}
	return windows
}

func paging(list pagedList, flags ...string) []string {
	return slices.Concat(list.argv, []string{"--fields", "id"}, flags)
}

func TestListRefusesALimitOfNoRecordsBeforeItAsksForAnything(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), paging(list, "--limit", "0")...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestListAsksForThePageOfTheLimitAndTheSkip(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := servedList(t, list)

			got := runWith(t, envOf(server), paging(list, "--limit", "2", "--skip", "2")...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Contains(t, windowsSent(server), url.Values{"$top": {list.top}, "$skip": {"2"}})
		})
	}
}
