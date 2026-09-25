package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const underItsOwner = "/comments/"

const unroutedPathMessage = "HTTP 404 Not Found"

const (
	unroutedIssueComments   = "issueComments"
	unroutedArticleComments = "articleComments"
)

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

func markDeleted(r *http.Request, body []byte) []byte {
	if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, underItsOwner) {
		return body
	}
	return []byte(`{"deleted":true}`)
}

func requireRoutedNowhere(t *testing.T, got outcome) {
	t.Helper()
	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, 404, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, unroutedPathMessage, detailNamed(t, found, "upstream_message"))
}

func TestCommentHasNoAddressOfItsOwnOnAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")

	t.Run("toplevel_get", func(t *testing.T) {
		dev.rewriting(topLevel(unroutedIssueComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract x")

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1, "the read that found no route was the whole call")
		assert.Equal(t, http.MethodGet, sent[0].Method)
		assert.Equal(t, "/api/"+unroutedIssueComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("toplevel_delete", func(t *testing.T) {
		dev.rewriting(topLevel(unroutedIssueComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "delete", issue, comment)

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1)
		assert.Equal(t, http.MethodDelete, sent[0].Method)
		assert.Equal(t, "/api/"+unroutedIssueComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("missing", func(t *testing.T) {
		class, _, found := strings.Cut(comment, "-")
		require.True(t, found, "the id of a comment is a class, a dash and a number: %q", comment)

		got := runWith(t, dev.env(), "comment", "delete", issue, class+"-999999999")

		refused := requireFault(t, got)
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

func TestCommentHasNoAddressOfItsOwnOnAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := commentedArticle(t, dev, "article")
	comment := commentOn(t, dev, article, "ytrack contract первая")

	t.Run("toplevel_post", func(t *testing.T) {
		dev.rewriting(topLevel(unroutedArticleComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "update", article, comment, "--text", "ytrack contract x")

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1)
		assert.Equal(t, http.MethodPost, sent[0].Method)
		assert.Equal(t, "/api/"+unroutedArticleComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("toplevel_delete", func(t *testing.T) {
		dev.rewriting(topLevel(unroutedArticleComments))
		defer dev.rewriting(nil)
		before := len(dev.requests())

		got := runWith(t, dev.env(), "comment", "delete", article, comment)

		requireRoutedNowhere(t, got)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1)
		assert.Equal(t, http.MethodDelete, sent[0].Method)
		assert.Equal(t, "/api/"+unroutedArticleComments+"/"+comment, sent[0].URL.Path)
	})

	t.Run("missing", func(t *testing.T) {
		class, _, found := strings.Cut(comment, "-")
		require.True(t, found, "the id of a comment is a class, a dash and a number: %q", comment)

		got := runWith(t, dev.env(), "comment", "delete", article, class+"-999999999")

		refused := requireFault(t, got)
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

func TestCommentTakenBackOnTheDevInstanceIsRemovedAndNeverWritten(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")
	show := []string{"issue", "show", issue, "--fields", "idReadable"}

	t.Run("soft_delete", func(t *testing.T) {
		dev.replacing(markDeleted)
		defer dev.replacing(nil)

		got := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract x")

		found := requireUncertainty(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
		assert.Equal(t, comment, detailNamed(t, found, "comment"))
		assert.Equal(t, []any{[]detail{
			{"field", "text"},
			{"expected", "ytrack contract x"},
			{"actual", nil},
		}}, detailNamed(t, found, "mismatch"))
	})

	assert.NotContains(t, commentIDs(t, dev, show...), comment,
		"a comment taken back is no comment of the issue any more")

	before := len(dev.requests())
	written := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract y")
	assert.Equal(t, "bad_usage", requireFault(t, written).code)
	sent := dev.requests()[before:]
	require.Len(t, sent, 1, "the read before the write is the whole call")
	assert.Equal(t, http.MethodGet, sent[0].Method)

	removed := runWith(t, dev.env(), "comment", "delete", issue, comment)
	assert.Equal(t, outcome{stdout: "id: \"" + comment + "\"\n"}, removed)

	again := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", "ytrack contract y")
	assert.Equal(t, "not_found", requireFault(t, again).code)
}
