package cli_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The request the removal of a comment goes out as, which is the one a refusal about it names. No fields stand
// on it: the specification declares none, and the answer carries nothing to name.
func commentDeletionRequest(address, owner, comment string) string {
	return "DELETE " + address + "/api/issues/" + owner + "/comments/" + comment
}

// removing is the server of a removal: the DELETE is the whole command, so a scenario says what that one
// request was answered with.
func removingAComment(t *testing.T, deletion http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodDelete, r.Method, "a comment is removed by one DELETE and nothing else") {
			return
		}
		deletion(w, r)
	})
}

// commentIDs is every comment the owner holds, as its show prints them: what a scenario holds a removal to is
// which comment is gone and which are still there.
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

// What a removal takes: the owner and the id, both as arguments, and nothing else at all. A single id is
// no address, so a call carrying one is short of an argument rather than given a bad one, and there is no flag
// to say the removal twice.
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
		// The comment is gone by the time the server has answered, so there is nothing to ask for: an
		// expression is refused as the unknown flag it is, and no expression of a removal is ever read.
		{name: "an expression", argv: []string{"comment", "delete", "DEV-1", "7-1", "--fields", "id("}},
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

// Nothing in the help offers a way to say the removal twice: ytrack removes what it was told to remove,
// once, and what a caller reads the comment with first stands there instead.
func TestCommentDeleteHelpOffersNoConfirmation(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"comment", "delete", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	for _, flag := range []string{"--yes", "--force", "--confirm"} {
		assert.NotContains(t, got.stdout, flag)
	}
}

// The id is held to its form before anything is sent, and for the same reason as everywhere else a child
// is addressed: the generated client resolves the segment against the server, and ".." there turns the removal
// of a comment into the removal of the issue it hangs from.
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
		// The digits are ASCII and no others: these two look like a 7 and a 1 and are written in bytes for it.
		{name: "digits in full width", id: "\xef\xbc\x97-\xef\xbc\x91"},
		{name: "letters", id: "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "comment", "delete", "--", "DEV-1", tc.id)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// The whole of the command on an issue: one DELETE to that comment of that issue, carrying no body and no
// query at all, and the id as the document. Nothing is read before it and nothing after it — the comment is
// gone by the time the server has answered, and the id the caller wrote is the one it went by.
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

// The same command on an article goes to the knowledge base and nowhere near the issues: the form of the
// owner settles it, as it does for every other command that works on either kind.
func TestCommentDeleteRemovesACommentOfAnArticleInOneRequest(t *testing.T) {
	t.Parallel()
	server := removingAComment(t, deletionDone())

	got := runWith(t, server.env(), "comment", "delete", "DEV-A-3", "8-5")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "id: \"8-5\"\n", got.stdout)
	assert.Equal(t, []string{"/api/articles/DEV-A-3/comments/8-5"}, server.sentPaths())
}

// What the server says about a removal it refused passes on word for word, and a 200 carrying anything at
// all is not the answer of the endpoint that was asked: a comment may well be gone, and the exit code says
// the caller cannot answer it by sending the call again.
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
			deletion: answer(http.StatusNotFound, entityNotFound("7-12")),
			code:     "not_found",
			exit:     1,
		},
		{
			name: "a token that may see the issue and not remove the comment",
			deletion: answer(http.StatusForbidden,
				`{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`),
			code: "denied",
			exit: 1,
		},
		{
			name:     "an answer carrying an object",
			deletion: answer(http.StatusOK, `{"x":1}`),
			code:     "upstream_lied",
			exit:     2,
		},
		{
			name:     "a page under a 200",
			deletion: gateway(http.StatusOK),
			code:     "upstream_lied",
			exit:     2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removingAComment(t, tc.deletion)

			got := runWith(t, server.env(), "comment", "delete", "DEV-7", "7-12")

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", commentDeletionRequest(server.url, "DEV-7", "7-12")},
				found.details[0])
			assert.Equal(t, []string{http.MethodDelete}, sentMethods(server))
		})
	}
}

// A comment of an issue of the polygon removed for real: the document is the id it went by, the show of
// the issue no longer carries it, and the server answers everything addressed to it afterwards as if it had
// never been written.
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
	assert.Equal(t, "not_found", requireRefusal(t, again).code)

	written := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract x")
	assert.Equal(t, "not_found", requireRefusal(t, written).code)
}

// The same on an article of the polygon, where a comment is removed outright in any case: the removal
// takes the one that was named and leaves the other where it was.
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
	assert.Equal(t, "not_found", requireRefusal(t, again).code)
}

// The server checks which owner a comment hangs from on a removal as it does on a write, across both
// kinds, and answers a comment of somebody else's owner as if it were not there. One request settles it, so
// ytrack neither guesses nor asks twice, and neither comment is taken away.
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

			assert.Equal(t, "not_found", requireRefusal(t, got).code)
			assert.Len(t, dev.requests()[before:], 1)
		})
	}

	assert.True(t, slices.Contains(commentIDs(t, dev, "issue", "show", issue, "--fields", "idReadable"), comment))
	assert.True(t, slices.Contains(commentIDs(t, dev, "article", "show", article, "--fields", "idReadable"),
		articleComment))
}

// A token that may not see the issue is answered as if the issue were not there, and the removal is the
// one request there is, so nothing is taken away and the admin finds the comment where it was.
func TestCommentDeleteRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	const first = "ytrack contract первая"
	comment := commentOn(t, dev, issue, first)
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"comment", "delete", issue, comment)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, detail{"upstream_message", "Entity with id " + issue + " not found"}, found.details[3])
	assert.Len(t, dev.requests()[before:], 1)

	assert.Equal(t, first, textOfTheComment(t, dev, []string{"issue", "show", issue, "--fields", "idReadable"}, comment))
}
