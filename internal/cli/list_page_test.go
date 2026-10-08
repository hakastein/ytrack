package cli_test

import (
	"fmt"
	"net/http"
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
		{argv: []string{"issue", "list", "--query", ""}, record: recordOf("Issue")},
		{argv: []string{"article", "list", "--query", ""}, record: recordOf("Article")},
		{argv: []string{"article", "list", "--parent", "DEV-A-1"}, record: recordOf("Article")},
		{argv: []string{"comment", "list", "DEV-1"}, record: recordOf("IssueComment")},
		{argv: []string{"attachment", "list", "DEV-1"}, record: recordOf("IssueAttachment")},
		{argv: []string{"tag", "list"}, record: recordOf("Tag")},
		{argv: []string{"time", "list", "DEV-1"}, record: recordOf("IssueWorkItem")},
		{argv: []string{"user", "list", "--query", ""}, record: recordOf("User")},
		{argv: []string{"project", "list"}, record: recordOf("Project")},
		{argv: []string{"activity", "list", "DEV-1"}, record: activityAt},
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

func printedIDs(t *testing.T, stdout string) []string {
	t.Helper()
	page := requireMapping(t, "stdout", stdout)
	records := page.Content[len(page.Content)-1]
	ids := []string{}
	for _, record := range records.Content {
		ids = append(ids, nodeAt(t, record, "id").Value)
	}
	return ids
}

func paging(list pagedList, flags ...string) []string {
	return slices.Concat(list.argv, []string{"--fields", "id"}, flags)
}

func TestListPrintsThePageOfTheLimitAndTheSkip(t *testing.T) {
	t.Parallel()
	for _, list := range pagedLists() {
		t.Run(strings.Join(list.argv, " "), func(t *testing.T) {
			t.Parallel()
			server := servedList(t, list)

			got := runWith(t, envOf(server), paging(list, "--limit", "2", "--skip", "2")...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{"1-2", "1-3"}, printedIDs(t, got.stdout))
		})
	}
}
