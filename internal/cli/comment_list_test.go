package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const (
	issueCommentListFields   = "id,author(login),created,text,deleted"
	articleCommentListFields = "id,author(login),created,text"
)

const (
	listedIssueComment = `{"deleted":false,"author":{"login":"admin","$type":"User"},"created":1789395789677,` +
		`"text":"первая\nвторая","id":"7-2","$type":"IssueComment"}`
	listedDeletedComment = `{"deleted":true,"author":{"login":"dev.member","$type":"User"},` +
		`"created":1789395790000,"text":null,"id":"7-3","$type":"IssueComment"}`
	listedArticleComment = `{"author":{"login":"admin","$type":"User"},"created":1789395789747,` +
		`"text":"к статье","id":"8-4","$type":"ArticleComment"}`
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
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentListPrintsTheCommentsOfAnIssueWithDeletedOnesAmongThem(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, "["+listedIssueComment+","+listedDeletedComment+"]"))

	got := runWith(t, server.env(), "comment", "list", "DEV-7")

	want := "total: 2\nreturned: 2\ntruncated: false\ncomments:\n" +
		`  - {id: "7-2", author: {login: "admin"}, created: "2026-09-14T14:23:09.677Z", text: "первая\nвторая", deleted: false}` + "\n" +
		`  - {id: "7-3", author: {login: "dev.member"}, created: "2026-09-14T14:23:10Z", text: null, deleted: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/issues/DEV-7/comments"}, server.sentPaths())
	assert.Equal(t, []url.Values{{"fields": {issueCommentListFields}, "$top": {"50"}}}, server.sentQueries())
}

func TestCommentListPrintsTheCommentsOfAnArticleWithoutDeleted(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, "["+listedArticleComment+"]"))

	got := runWith(t, server.env(), "comment", "list", "DEV-A-1")

	want := "total: 1\nreturned: 1\ntruncated: false\ncomments:\n" +
		`  - {id: "8-4", author: {login: "admin"}, created: "2026-09-14T14:23:09.747Z", text: "к статье"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/articles/DEV-A-1/comments"}, server.sentPaths())
	assert.Equal(t, []url.Values{{"fields": {articleCommentListFields}, "$top": {"50"}}}, server.sentQueries())
}

func TestCommentListAddsToTheDefaultOfTheOwnerItNamed(t *testing.T) {
	t.Parallel()

	t.Run("an addition on an article", func(t *testing.T) {
		t.Parallel()
		server := serve(t, respondWith(http.StatusOK, `[]`))

		got := runWith(t, server.env(), "comment", "list", "DEV-A-1", "--fields", "+updated")

		assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\ncomments: []\n"}, got)
		assert.Equal(t, []string{articleCommentListFields + ",updated"}, server.sentFields())
	})

	t.Run("deleted on an article", func(t *testing.T) {
		t.Parallel()
		server := serve(t, respondWith(http.StatusOK, "["+listedArticleComment+"]"))

		got := runWith(t, server.env(), "comment", "list", "DEV-A-1", "--fields", "+deleted")

		found := requireFault(t, got)
		assert.Equal(t, "unknown_name", found.code)
		assert.Equal(t, []string{articleCommentListFields + ",deleted"}, server.sentFields())
	})
}

func TestCommentListCountsTheCommentsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()

	t.Run("more counted than arrived", func(t *testing.T) {
		t.Parallel()
		count := `[{"$type":"IssueComment","id":"7-2"},{"$type":"IssueComment","id":"7-3"},{"$type":"IssueComment","id":"7-4"}]`
		server := serve(t, countedBy("["+listedIssueComment+"]", respondWith(http.StatusOK, count)))

		got := runWith(t, server.env(), "comment", "list", "DEV-7", "--limit", "1")

		printed := requireCommentListing(t, got)
		assert.Equal(t, 3, printed.Total)
		assert.Equal(t, 1, printed.Returned)
		assert.Equal(t, []url.Values{
			{"fields": {issueCommentListFields}, "$top": {"1"}},
			{"fields": {"id"}, "$top": {"-1"}},
		}, server.sentQueries())
		assert.Equal(t, []string{"/api/issues/DEV-7/comments", "/api/issues/DEV-7/comments"}, server.sentPaths())
	})

	t.Run("fewer counted than arrived", func(t *testing.T) {
		t.Parallel()
		server := serve(t, countedBy("["+listedIssueComment+"]", respondWith(http.StatusOK, `[]`)))

		got := runWith(t, server.env(), "comment", "list", "DEV-7", "--limit", "1")

		assert.Equal(t, faultDocument{
			code:    "upstream_failed",
			details: []detail{{"total", 0}, {"returned", 1}},
		}, requireFault(t, got))
	})

	t.Run("more arrived than the limit", func(t *testing.T) {
		t.Parallel()
		server := serve(t, respondWith(http.StatusOK, "["+listedIssueComment+","+listedDeletedComment+"]"))

		got := runWith(t, server.env(), "comment", "list", "DEV-7", "--limit", "1")

		assert.Equal(t, faultDocument{
			code:    "upstream_invalid",
			details: []detail{{"limit", 1}, {"returned", 2}},
		}, requireFault(t, got))
		assert.Len(t, server.requests(), 1)
	})
}
