package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
)

const issueCommentListFields = "id,author(login),created,text,deleted"

const (
	listedIssueComment = `{"deleted":false,"author":{"login":"admin","$type":"User"},"created":1789395789677,` +
		`"text":"First\nSecond","id":"7-2","$type":"IssueComment"}`
	listedDeletedComment = `{"deleted":true,"author":{"login":"member","$type":"User"},` +
		`"created":1789395790000,"text":null,"id":"7-3","$type":"IssueComment"}`
)

type commentListing struct {
	Total     int             `yaml:"total"`
	Returned  int             `yaml:"returned"`
	Truncated bool            `yaml:"truncated"`
	Comments  []listedComment `yaml:"comments"`
}

type listedComment struct {
	ID     string `yaml:"id"`
	Author struct {
		Login string `yaml:"login"`
	} `yaml:"author"`
	Created string  `yaml:"created"`
	Text    *string `yaml:"text"`
	Deleted *bool   `yaml:"deleted"`
}

func requireCommentListing(t *testing.T, got outcome) commentListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed commentListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Comments, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

func TestCommentListRefusesACallThatNamesNoOneOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an internal id", argv: []string{"comment", "list", "7-12"}},
		{name: "a path that would reach another endpoint", argv: []string{"comment", "list", ".."}},
		{name: "a limit of zero", argv: []string{"comment", "list", "DEV-1", "--limit", "0"}},
		{name: "a negative limit", argv: []string{"comment", "list", "DEV-1", "--limit", "-1"}},
		{name: "a limit past the largest int32", argv: []string{"comment", "list", "DEV-1", "--limit", "2147483648"}},
		{
			name: "a limit given twice",
			argv: []string{"comment", "list", "DEV-1", "--limit", "1", "--limit", "2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCommentListPrintsTheCommentsOfAnIssueWithDeletedOnesAmongThem(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedIssueComment+","+listedDeletedComment+"]"))

	got := runWith(t, server.Env(), "comment", "list", "DEV-7")

	want := "total: 2\nreturned: 2\ntruncated: false\ncomments:\n" +
		`  - {id: "7-2", author: {login: "admin"}, created: "2026-09-14T14:23:09.677Z", text: "First\nSecond", deleted: false}` + "\n" +
		`  - {id: "7-3", author: {login: "member"}, created: "2026-09-14T14:23:10Z", text: null, deleted: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/issues/DEV-7/comments"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {issueCommentListFields}, "$top": {"50"}}}, server.Queries())
}

func TestCommentListCountsTheCommentsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()

	t.Run("more counted than arrived", func(t *testing.T) {
		t.Parallel()
		count := `[{"$type":"IssueComment","id":"7-2"},{"$type":"IssueComment","id":"7-3"},{"$type":"IssueComment","id":"7-4"}]`
		server := fake.Serve(t, countedBy("["+listedIssueComment+"]", fake.JSON(http.StatusOK, count)))

		got := runWith(t, server.Env(), "comment", "list", "DEV-7", "--limit", "1")

		printed := requireCommentListing(t, got)
		assert.Equal(t, 3, printed.Total)
		assert.Equal(t, 1, printed.Returned)
		assert.Equal(t, []url.Values{
			{"fields": {issueCommentListFields}, "$top": {"1"}},
			{"fields": {"id"}, "$top": {"-1"}},
		}, server.Queries())
		assert.Equal(t, []string{"/api/issues/DEV-7/comments", "/api/issues/DEV-7/comments"}, server.Paths())
	})

	t.Run("fewer counted than arrived", func(t *testing.T) {
		t.Parallel()
		server := fake.Serve(t, countedBy("["+listedIssueComment+"]", fake.JSON(http.StatusOK, `[]`)))

		got := runWith(t, server.Env(), "comment", "list", "DEV-7", "--limit", "1")

		assert.Equal(t, faultDocument{
			code:    "upstream_failed",
			details: []detail{{"total", 0}, {"returned", 1}},
		}, requireFault(t, got))
	})

	t.Run("more arrived than the limit", func(t *testing.T) {
		t.Parallel()
		server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedIssueComment+","+listedDeletedComment+"]"))

		got := runWith(t, server.Env(), "comment", "list", "DEV-7", "--limit", "1")

		assert.Equal(t, faultDocument{
			code:    "upstream_invalid",
			details: []detail{{"limit", 1}, {"returned", 2}},
		}, requireFault(t, got))
		assert.Len(t, server.Requests(), 1)
	})
}
