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

// What the list asks of a comment where the caller writes none: on an issue whether it was taken back as well,
// and on an article, which keeps no such comment, not that.
const (
	issueCommentListFields   = "id,author(login),created,text,deleted"
	articleCommentListFields = "id,author(login),created,text"
)

const (
	devInstanceCommentedIssue = "DEV-7"
	devInstanceFirstComment   = "7-2"
	devInstanceSecondComment  = "7-3"
)

// Comments of the server as a page of the list brings them, $type and all.
const (
	listedIssueComment = `{"deleted":false,"author":{"login":"admin","$type":"User"},"created":1789395789677,` +
		`"text":"первая\nвторая","id":"7-2","$type":"IssueComment"}`
	listedDeletedComment = `{"deleted":true,"author":{"login":"dev.member","$type":"User"},` +
		`"created":1789395790000,"text":null,"id":"7-3","$type":"IssueComment"}`
	listedArticleComment = `{"author":{"login":"admin","$type":"User"},"created":1789395789747,` +
		`"text":"к статье","id":"8-4","$type":"ArticleComment"}`
)

// commentListing is the document comment list prints, read back.
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

func TestCommentListHelpNamesItsDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"comment", "list", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, issueCommentListFields)
}

// Every call that names no one owner ytrack can address is refused before the network, the way every command
// of an owner refuses it, and so is a limit no list of the tool takes.
func TestCommentListRefusesACallThatNamesNoOneOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no owner", argv: []string{"comment", "list"}},
		{name: "two owners", argv: []string{"comment", "list", "DEV-1", "DEV-2"}},
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

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// An issue goes to the API of issues with the limit as $top, and its records come one to a line in the order
// they arrived. A comment its author took back is one of them, printed with its text null and deleted true,
// where a show of the issue would leave it out.
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

// An article goes to the API of articles, and deleted is asked for by nobody there: the server sends no such
// name for a comment of an article, which it never keeps once taken back.
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

// A leading + adds to the default of the kind of owner the call named, and deleted written on an article is a
// name its comment does not carry.
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

		found := requireRefusal(t, got)
		assert.Equal(t, "unknown_name", found.code)
		assert.Equal(t, []string{articleCommentListFields + ",deleted"}, server.sentFields())
	})
}

// A page that fills the limit proves nothing about the rest, so the whole is read off a second pass over ids
// alone on the same path; a count below what arrived is two different collections and no document.
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
		}, requireRefusal(t, got))
	})

	t.Run("more arrived than the limit", func(t *testing.T) {
		t.Parallel()
		server := serve(t, respondWith(http.StatusOK, "["+listedIssueComment+","+listedDeletedComment+"]"))

		got := runWith(t, server.env(), "comment", "list", "DEV-7", "--limit", "1")

		assert.Equal(t, faultDocument{
			code:    "upstream_invalid",
			details: []detail{{"limit", 1}, {"returned", 2}},
		}, requireRefusal(t, got))
		assert.Len(t, server.requests(), 1)
	})
}

func TestCommentListReadsTheCommentsOfAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "comment", "list", devInstanceCommentedIssue)

	printed := requireCommentListing(t, got)
	assert.Equal(t, 2, printed.Total)
	require.Len(t, printed.Comments, 2)
	assert.Equal(t, devInstanceFirstComment, printed.Comments[0].ID)
	assert.Equal(t, "admin", printed.Comments[0].Author.Login)
	assert.Equal(t, devInstanceSecondComment, printed.Comments[1].ID)
	assert.Equal(t, "dev.member", printed.Comments[1].Author.Login)
	for _, comment := range printed.Comments {
		require.NotNil(t, comment.Deleted, comment.ID)
		assert.False(t, *comment.Deleted, comment.ID)
		require.NotNil(t, comment.Text, comment.ID)
		assert.NotEmpty(t, *comment.Text, comment.ID)
	}
	assert.Equal(t, []string{"/api/issues/" + devInstanceCommentedIssue + "/comments"}, dev.sentPaths())
	assert.Equal(t, []string{issueCommentListFields}, dev.sentFields())
}

func TestCommentListPrintsACommentTakenBackOnAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	standing := commentOn(t, dev, issue, "ytrack contract остаётся")
	gone := commentOn(t, dev, issue, "ytrack contract забрана")
	dev.replacing(markDeleted)
	requireUncertainty(t, runWith(t, dev.env(), "comment", "update", issue, gone, "--text", "ytrack contract x"))
	dev.replacing(nil)

	got := runWith(t, dev.env(), "comment", "list", issue)

	printed := requireCommentListing(t, got)
	assert.Equal(t, 2, printed.Total)
	require.Len(t, printed.Comments, 2)
	assert.Equal(t, standing, printed.Comments[0].ID)
	require.NotNil(t, printed.Comments[0].Deleted)
	assert.False(t, *printed.Comments[0].Deleted)
	assert.Equal(t, gone, printed.Comments[1].ID)
	require.NotNil(t, printed.Comments[1].Deleted)
	assert.True(t, *printed.Comments[1].Deleted)
	assert.Nil(t, printed.Comments[1].Text)
}

func TestCommentListReadsTheCommentsOfAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := commentedArticle(t, dev, "article")
	first := commentOn(t, dev, article, "ytrack contract первая")
	commentOn(t, dev, article, "ytrack contract вторая")
	before := len(dev.requests())

	got := runWith(t, dev.env(), "comment", "list", article, "--limit", "1")

	printed := requireCommentListing(t, got)
	assert.Equal(t, 2, printed.Total)
	assert.True(t, printed.Truncated)
	require.Len(t, printed.Comments, 1)
	assert.Equal(t, first, printed.Comments[0].ID)
	assert.Equal(t, "admin", printed.Comments[0].Author.Login)
	require.NotNil(t, printed.Comments[0].Text)
	assert.Equal(t, "ytrack contract первая", *printed.Comments[0].Text)
	assert.Nil(t, printed.Comments[0].Deleted)
	asked := dev.requests()[before:]
	require.Len(t, asked, 2)
	for _, request := range asked {
		assert.Equal(t, "/api/articles/"+article+"/comments", request.URL.Path)
	}
	assert.Equal(t, []url.Values{
		{"fields": {articleCommentListFields}, "$top": {"1"}},
		{"fields": {"id"}, "$top": {"-1"}},
	}, dev.sentQueries()[before:])
}
