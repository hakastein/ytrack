package cli_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func commentDeletionRequest(address, owner, comment string) string {
	return "DELETE " + address + "/api/issues/" + owner + "/comments/" + comment
}

func removingAComment(t *testing.T, deletion http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodDelete, r.Method, "a comment is removed by one DELETE and nothing else") {
			return
		}
		deletion(w, r)
	})
}

func commentIDs(t *testing.T, dev *upstream, show ...string) []string {
	t.Helper()
	got := runWith(t, dev.env(), show...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	var ids []string
	for _, held := range nodeAt(t, requireMapping(t, "stdout", got.stdout), "comments").Content {
		ids = append(ids, nodeAt(t, held, "id").Value)
	}
	return ids
}

func TestCommentDeleteRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "neither owner nor id", argv: []string{"comment", "delete"}},
		{name: "an owner and no id", argv: []string{"comment", "delete", "DEV-1"}},
		{name: "the id of the comment alone", argv: []string{"comment", "delete", "7-12"}},
		{name: "a second id", argv: []string{"comment", "delete", "DEV-1", "7-1", "7-2"}},
		{name: "a flag that says it twice", argv: []string{"comment", "delete", "DEV-1", "7-1", "--yes"}},
		{name: "a flag that says it anyway", argv: []string{"comment", "delete", "DEV-1", "7-1", "--force"}},
		{name: "an expression", argv: []string{"comment", "delete", "DEV-1", "7-1", "--fields", "id("}},
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

func TestCommentDeleteHelpOffersNoConfirmation(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"comment", "delete", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	for _, flag := range []string{"--yes", "--force", "--confirm"} {
		assert.NotContains(t, got.stdout, flag)
	}
}

func TestCommentDeleteRefusesAnIDThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "a dot", id: "."},
		{name: "two dots", id: ".."},
		{name: "a number alone", id: "7"},
		{name: "a number and a dash", id: "7-"},
		{name: "a number without a class", id: "-1"},
		{name: "three numbers", id: "7-1-1"},
		{name: "the readable id of an issue", id: "DEV-1"},
		{name: "the readable id of an article", id: "DEV-A-1"},
		{name: "a path after the id", id: "7-1/.."},
		{name: "an escaped slash after the id", id: "7-1%2F1"},
		{name: "a space before the id", id: " 7-1"},
		{name: "a line ending after the id", id: "7-1\n"},
		{name: "digits in full width", id: "\xef\xbc\x97-\xef\xbc\x91"},
		{name: "letters", id: "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "comment", "delete", "--", "DEV-1", tc.id)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentDeleteRemovesACommentOfAnIssueInOneRequest(t *testing.T) {
	t.Parallel()
	server := removingAComment(t, deletionDone())

	got := runWith(t, server.env(), "comment", "delete", "dev-7", "7-12")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "id: \"7-12\"\n", got.stdout)
	assert.Equal(t, []string{"/api/issues/dev-7/comments/7-12"}, server.sentPaths())
	assert.Empty(t, server.requests()[0].URL.RawQuery, "the specification declares no parameter for the removal")
	assert.Equal(t, []string{""}, server.asks())
}

func TestCommentDeleteRemovesACommentOfAnArticleInOneRequest(t *testing.T) {
	t.Parallel()
	server := removingAComment(t, deletionDone())

	got := runWith(t, server.env(), "comment", "delete", "DEV-A-3", "8-5")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "id: \"8-5\"\n", got.stdout)
	assert.Equal(t, []string{"/api/articles/DEV-A-3/comments/8-5"}, server.sentPaths())
}

func TestCommentDeleteReadsTheAnswerOfTheRemoval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		deletion http.HandlerFunc
		code     string
		exit     int
	}{
		{
			name:     "a comment the server has none of",
			deletion: respondWith(http.StatusNotFound, entityNotFound("7-12")),
			code:     "not_found",
			exit:     1,
		},
		{
			name: "a token that may see the issue and not remove the comment",
			deletion: respondWith(http.StatusForbidden,
				`{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`),
			code: "denied",
			exit: 1,
		},
		{
			name:     "an answer carrying an object",
			deletion: respondWith(http.StatusOK, `{"x":1}`),
			code:     "upstream_invalid",
			exit:     2,
		},
		{
			name:     "a page under a 200",
			deletion: gateway(http.StatusOK),
			code:     "upstream_invalid",
			exit:     2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removingAComment(t, tc.deletion)

			got := runWith(t, server.env(), "comment", "delete", "DEV-7", "7-12")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", commentDeletionRequest(server.url, "DEV-7", "7-12")},
				found.details[0])
			assert.Equal(t, []string{http.MethodDelete}, sentMethods(server))
		})
	}
}

func TestCommentDeleteRemovesACommentOfAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")
	kept := commentOn(t, dev, issue, "ytrack contract вторая")
	show := []string{"issue", "show", issue, "--fields", "idReadable"}

	got := runWith(t, dev.env(), "comment", "delete", issue, comment)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "id: \""+comment+"\"\n", got.stdout)
	assert.Equal(t, []string{kept}, commentIDs(t, dev, show...))

	again := runWith(t, dev.env(), "comment", "delete", issue, comment)
	assert.Equal(t, "not_found", requireFault(t, again).code)

	written := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract x")
	assert.Equal(t, "not_found", requireFault(t, written).code)
}

func TestCommentDeleteRemovesACommentOfAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := commentedArticle(t, dev, "article")
	comment := commentOn(t, dev, article, "ytrack contract первая")
	kept := commentOn(t, dev, article, "ytrack contract вторая")
	show := []string{"article", "show", article, "--fields", "idReadable"}

	got := runWith(t, dev.env(), "comment", "delete", article, comment)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "id: \""+comment+"\"\n", got.stdout)
	assert.Equal(t, []string{kept}, commentIDs(t, dev, show...))

	again := runWith(t, dev.env(), "comment", "delete", article, comment)
	assert.Equal(t, "not_found", requireFault(t, again).code)
}

func TestCommentDeleteRefusesACommentOfAnotherOwner(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	otherIssue := commentedIssue(t, dev, "other issue")
	article := commentedArticle(t, dev, "article")
	comment := commentOn(t, dev, issue, "ytrack contract комментарий задачи")
	articleComment := commentOn(t, dev, article, "ytrack contract комментарий статьи")

	tests := []struct {
		name    string
		owner   string
		comment string
	}{
		{name: "another issue", owner: otherIssue, comment: comment},
		{name: "an article for the comment of an issue", owner: article, comment: comment},
		{name: "an issue for the comment of an article", owner: issue, comment: articleComment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := len(dev.requests())

			got := runWith(t, dev.env(), "comment", "delete", tc.owner, tc.comment)

			assert.Equal(t, "not_found", requireFault(t, got).code)
			assert.Len(t, dev.requests()[before:], 1)
		})
	}

	assert.True(t, slices.Contains(commentIDs(t, dev, "issue", "show", issue, "--fields", "idReadable"), comment))
	assert.True(t, slices.Contains(commentIDs(t, dev, "article", "show", article, "--fields", "idReadable"),
		articleComment))
}

func TestCommentDeleteRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	const first = "ytrack contract первая"
	comment := commentOn(t, dev, issue, first)
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"comment", "delete", issue, comment)

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, detail{"upstream_message", "Entity with id " + issue + " not found"}, found.details[3])
	assert.Len(t, dev.requests()[before:], 1)

	assert.Equal(t, first, textOfTheComment(t, dev, []string{"issue", "show", issue, "--fields", "idReadable"}, comment))
}
