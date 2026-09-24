package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The one place a comment stands under its owner in an address: everything before it names the owner, and
// everything after it names the comment.
const underItsOwner = "/comments/"

// What the server answers a path it routes nowhere: a refusal about the address rather than about an entity,
// which is how a route that is not there is told apart from a comment that is not there.
const noSuchRoute = "HTTP 404 Not Found"

// The collections a comment would live in if YouTrack held comments out from under their owner. Neither
// stands in the specification, and ytrack addresses neither.
const (
	topLevelIssueComments   = "issueComments"
	topLevelArticleComments = "articleComments"
)

// topLevel is the rewrite of a request on its way to the polygon: a comment addressed under its owner is
// addressed by itself instead, under the collection a top-level comment would live in. Every request naming no
// comment — the creation, the show of the owner, the deletion of the owner — stands as ytrack sent it, so the
// fixtures of the scenario are filed and taken away over this very proxy.
func topLevel(collection string) func(*url.URL) {
	return func(u *url.URL) {
		_, comment, found := strings.Cut(u.Path, underItsOwner)
		if !found {
			return
		}
		u.Path = "/api/" + collection + "/" + comment
		u.RawPath = ""
	}
}

// takenBack is the replacement of the body of a write: the text the caller wrote becomes the one field that
// takes a comment of an issue back, and the read before the write, which carries no body, is left alone. It is
// how the scenario has the polygon take a comment back over the write ytrack really sends.
func takenBack(r *http.Request, body []byte) []byte {
	if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, underItsOwner) {
		return body
	}
	return []byte(`{"deleted":true}`)
}

// requireRoutedNowhere holds a refusal to being the server's word about the address: the status is a 404 and
// the word with it names the protocol rather than an entity, which no comment of the polygon is ever answered
// with.
func requireRoutedNowhere(t *testing.T, got outcome) {
	t.Helper()
	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, 404, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, noSuchRoute, detailNamed(t, found, "upstream_message"))
}

// ytrack addresses a comment under its owner and has no way to address it otherwise, so the address without the
// owner is put on the wire between ytrack and the polygon: the reading before a write and the removal both come
// back a 404 about the route. The same calls against the address ytrack does send go through, so the 404 is the
// address and not the comment, and a comment of that owner that is really missing is refused in the server's
// other words.
func TestCommentHasNoAddressOfItsOwnOnAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")

	t.Run("toplevel_get", func(t *testing.T) {
		dev.rewriting(topLevel(topLevelIssueComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract x")

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1, "the read that found no route was the whole call")
		assert.Equal(t, http.MethodGet, sent[0].Method)
		assert.Equal(t, "/api/"+topLevelIssueComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("toplevel_delete", func(t *testing.T) {
		dev.rewriting(topLevel(topLevelIssueComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "delete", issue, comment)

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1)
		assert.Equal(t, http.MethodDelete, sent[0].Method)
		assert.Equal(t, "/api/"+topLevelIssueComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("missing", func(t *testing.T) {
		class, _, found := strings.Cut(comment, "-")
		require.True(t, found, "the id of a comment is a class, a dash and a number: %q", comment)

		got := runWith(t, dev.env(), "comment", "delete", issue, class+"-999999999")

		refused := requireRefusal(t, got)
		assert.Equal(t, "not_found", refused.code)
		assert.Equal(t, "Entity with id "+class+"-999999999 not found",
			detailNamed(t, refused, "upstream_message"))
	})

	t.Run("control", func(t *testing.T) {
		const text = "ytrack contract y"

		written := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", text)

		require.Equal(t, 0, written.code, "stderr: %s", written.stderr)
		assert.Equal(t, text, nodeAt(t, requireMapping(t, "stdout", written.stdout), "text").Value)

		removed := runWith(t, dev.env(), "comment", "delete", issue, comment)
		assert.Equal(t, outcome{stdout: "id: \"" + comment + "\"\n"}, removed)
	})
}

// The same on an article, where a write is the whole call: the address without the owner is a route the
// server has none of there either, and a comment of an article that is really missing is refused in the words
// it keeps for an entity.
func TestCommentHasNoAddressOfItsOwnOnAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := commentedArticle(t, dev, "article")
	comment := commentOn(t, dev, article, "ytrack contract первая")

	t.Run("toplevel_post", func(t *testing.T) {
		dev.rewriting(topLevel(topLevelArticleComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "update", article, comment, "--text", "ytrack contract x")

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1)
		assert.Equal(t, http.MethodPost, sent[0].Method)
		assert.Equal(t, "/api/"+topLevelArticleComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("toplevel_delete", func(t *testing.T) {
		dev.rewriting(topLevel(topLevelArticleComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "delete", article, comment)

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1)
		assert.Equal(t, http.MethodDelete, sent[0].Method)
		assert.Equal(t, "/api/"+topLevelArticleComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("missing", func(t *testing.T) {
		class, _, found := strings.Cut(comment, "-")
		require.True(t, found, "the id of a comment is a class, a dash and a number: %q", comment)

		got := runWith(t, dev.env(), "comment", "delete", article, class+"-999999999")

		refused := requireRefusal(t, got)
		assert.Equal(t, "not_found", refused.code)
		assert.Equal(t, "Entity with id "+class+"-999999999 not found",
			detailNamed(t, refused, "upstream_message"))
	})

	t.Run("control", func(t *testing.T) {
		const text = "ytrack contract y"

		written := runWith(t, dev.env(), "comment", "update", article, comment, "--text", text)

		require.Equal(t, 0, written.code, "stderr: %s", written.stderr)
		assert.Equal(t, text, nodeAt(t, requireMapping(t, "stdout", written.stdout), "text").Value)

		removed := runWith(t, dev.env(), "comment", "delete", article, comment)
		assert.Equal(t, outcome{stdout: "id: \"" + comment + "\"\n"}, removed)
	})
}

// What the read before a write of an issue's comment is there for, held to the polygon from end to end. A
// comment is taken back by a write ytrack never sends, so the write it does send carries that body instead;
// the answer comes back a 200 whose text is gone, and the check of the write is what says so. Afterwards the
// comment is out of the show of the issue, no write reaches it at all, and the removal is what takes it away
// for good.
func TestCommentTakenBackOnTheDevInstanceIsRemovedAndNeverWritten(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")
	show := []string{"issue", "show", issue, "--fields", "idReadable"}

	t.Run("soft_delete", func(t *testing.T) {
		dev.replacing(takenBack)
		defer dev.replacing(nil)

		got := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract x")

		found := requireUncertainty(t, got)
		assert.Equal(t, "upstream_lied", found.code)
		assert.Equal(t, comment, detailNamed(t, found, "comment"))
		assert.Equal(t, []any{[]detail{
			{"field", "text"},
			{"written", "ytrack contract x"},
			{"arrived", nil},
		}}, detailNamed(t, found, "mismatch"))
	})

	assert.NotContains(t, commentIDs(t, dev, show...), comment,
		"a comment taken back is no comment of the issue any more")

	before := len(dev.requests())
	written := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract y")
	assert.Equal(t, "bad_usage", requireRefusal(t, written).code)
	sent := dev.requests()[before:]
	require.Len(t, sent, 1, "the read before the write is the whole call")
	assert.Equal(t, http.MethodGet, sent[0].Method)

	removed := runWith(t, dev.env(), "comment", "delete", issue, comment)
	assert.Equal(t, outcome{stdout: "id: \"" + comment + "\"\n"}, removed)

	again := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract y")
	assert.Equal(t, "not_found", requireRefusal(t, again).code)
}
