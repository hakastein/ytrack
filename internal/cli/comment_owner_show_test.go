package cli_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const (
	blockComment  = "\n первая   \n---\n~~~\nсмайл \xf0\x9f\x98\x80\nпоследняя"
	quotedComment = "первая\r\nвторая\xe2\x80\xa8третья"
	shortComment  = "ytrack contract short"
	voteComment   = "+1"
)

func readComments(t *testing.T, dev *upstream, show ...string) []*yaml.Node {
	t.Helper()
	got := runWith(t, dev.env(), show...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	require.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	keys := keysOf(mapping)
	require.NotEmpty(t, keys)
	assert.Equal(t, "comments", keys[len(keys)-1], "comments are the last key of the owner's document")
	return nodeAt(t, mapping, "comments").Content
}

func idsOfComments(t *testing.T, held []*yaml.Node) []string {
	t.Helper()
	ids := make([]string, 0, len(held))
	for _, comment := range held {
		ids = append(ids, nodeAt(t, comment, "id").Value)
	}
	return ids
}

func requireInCreationOrder(t *testing.T, held []*yaml.Node) {
	t.Helper()
	for i := 1; i < len(held); i++ {
		earlier, later := createdAt(t, held[i-1]), createdAt(t, held[i])
		require.True(t, earlier.Before(later), "the comment written at %s stands before the one written at %s",
			later, earlier)
	}
}

func createdAt(t *testing.T, comment *yaml.Node) time.Time {
	t.Helper()
	written, err := time.Parse(time.RFC3339, nodeAt(t, comment, "created").Value)
	require.NoError(t, err, "created is no instant: %q", nodeAt(t, comment, "created").Value)
	return written
}

func TestArticleShowReadsTheCommentsWrittenOnAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := commentedArticle(t, dev, "article")
	block := commentOn(t, dev, article, blockComment)
	quoted := commentOn(t, dev, article, quotedComment)
	short := commentOn(t, dev, article, shortComment)
	show := []string{"article", "show", article, "--fields", "idReadable"}

	held := readComments(t, dev, show...)

	require.Len(t, held, 3)
	requireInCreationOrder(t, held)
	assert.Equal(t, []string{block, quoted, short}, idsOfComments(t, held))
	for i, text := range []string{blockComment, quotedComment, shortComment} {
		assert.Equal(t, "admin", nodeAt(t, held[i], "author", "login").Value)
		assert.Equal(t, text, nodeAt(t, held[i], "text").Value, "the comment %d came back as something else", i)
	}
	assert.Equal(t, yaml.LiteralStyle, nodeAt(t, held[0], "text").Style)
	assert.Equal(t, yaml.DoubleQuotedStyle, nodeAt(t, held[1], "text").Style,
		"a carriage return keeps the text of a comment out of a literal block")

	assert.Equal(t, []string{short}, commentIDs(t, dev, append(slices.Clone(show), "--comments=1")...),
		"--comments=N is the last N by the moment they were written")

	none := runWith(t, dev.env(), append(slices.Clone(show), "--comments=0")...)
	require.Equal(t, 0, none.code, "stderr: %s", none.stderr)
	assert.Equal(t, []string{"idReadable"}, keysOf(requireMapping(t, "stdout", none.stdout)))

	removed := runWith(t, dev.env(), "comment", "delete", article, quoted)
	require.Equal(t, 0, removed.code, "stderr: %s", removed.stderr)
	assert.Equal(t, []string{block, short}, commentIDs(t, dev, show...))
}

func TestIssueShowReadsTheCommentsWrittenOnAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	block := commentOn(t, dev, issue, blockComment)
	quoted := commentOn(t, dev, issue, quotedComment)
	short := commentOn(t, dev, issue, shortComment)
	vote := commentOn(t, dev, issue, voteComment)
	show := []string{"issue", "show", issue, "--fields", "idReadable"}

	held := readComments(t, dev, show...)

	require.Len(t, held, 4)
	requireInCreationOrder(t, held)
	assert.Equal(t, []string{block, quoted, short, vote}, idsOfComments(t, held))
	for i, text := range []string{blockComment, quotedComment, shortComment, voteComment} {
		assert.Equal(t, "admin", nodeAt(t, held[i], "author", "login").Value)
		assert.Equal(t, text, nodeAt(t, held[i], "text").Value, "the comment %d came back as something else", i)
	}
	assert.Equal(t, yaml.LiteralStyle, nodeAt(t, held[0], "text").Style)
	assert.Equal(t, yaml.DoubleQuotedStyle, nodeAt(t, held[1], "text").Style)

	assert.Equal(t, []string{short, vote}, commentIDs(t, dev, append(slices.Clone(show), "--comments=2")...))

	const rewritten = "ytrack contract правка первого"
	written := runWith(t, dev.env(), "comment", "update", issue, block, "--text", rewritten)
	require.Equal(t, 0, written.code, "stderr: %s", written.stderr)

	after := readComments(t, dev, show...)
	assert.Equal(t, []string{block, quoted, short, vote}, idsOfComments(t, after),
		"a write moves no comment: the list stands by created and not by updated")
	assert.Equal(t, rewritten, nodeAt(t, after[0], "text").Value)

	removed := runWith(t, dev.env(), "comment", "delete", issue, short)
	require.Equal(t, 0, removed.code, "stderr: %s", removed.stderr)
	assert.Equal(t, []string{block, quoted, vote}, commentIDs(t, dev, show...))
}
