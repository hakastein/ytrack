package cli_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The texts written on an owner of the polygon, each for what it does to the document that reads them back.
// Hostile runes stand as bytes, since a typed \uXXXX reaches a Go literal as the rune itself.
const (
	// Prose a literal block still has to carry: an empty first line, a line beginning with a space, trailing
	// spaces, the lines the emitter's own indicators trip over, a rune outside the basic plane, and no line
	// ending at the end.
	blockComment = "\n первая   \n---\n~~~\nсмайл \xf0\x9f\x98\x80\nпоследняя"
	// Prose no block can carry: a carriage return and a line separator are line endings YAML writes in a block
	// as a plain line ending or not at all.
	quotedComment = "первая\r\nвторая\xe2\x80\xa8третья"
	// A line of plain words, the one a reader of the list would call ordinary.
	shortComment = "ytrack contract short"
	// The text a workflow of the polygon reads as a vote, which is a comment like any other to the show.
	voteComment = "+1"
)

// heldComments is every comment the owner holds, as the show of the owner prints them, in the order it printed
// them: the node of each, so a scenario reads the text and the style the emitter gave it and not only the id.
// Comments stand last in the document whatever the caller asked of the owner, and that is asserted here rather
// than in each scenario.
func heldComments(t *testing.T, dev *upstream, show ...string) []*yaml.Node {
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

// requireWrittenInOrder holds a list of comments to standing by the moment each was written, earliest first.
// The order is the server's to give and the show's to keep, and nothing in the list itself says it: the ids
// are the server's counters and no promise about time at all.
func requireWrittenInOrder(t *testing.T, held []*yaml.Node) {
	t.Helper()
	for i := 1; i < len(held); i++ {
		earlier, later := writtenAt(t, held[i-1]), writtenAt(t, held[i])
		require.True(t, earlier.Before(later), "the comment written at %s stands before the one written at %s",
			later, earlier)
	}
}

func writtenAt(t *testing.T, comment *yaml.Node) time.Time {
	t.Helper()
	written, err := time.Parse(time.RFC3339, nodeAt(t, comment, "created").Value)
	require.NoError(t, err, "created is no instant: %q", nodeAt(t, comment, "created").Value)
	return written
}

// Comments come with their owner and nowhere else, so what an article of the polygon holds is read by the
// show of the article: three written for real, each answered as it was written, in the order they were
// written. The prose is the emitter's problem as much as the server's — one text a block carries and one it
// cannot — and --comments says how many of the last the caller wants, none of them included.
func TestArticleShowReadsTheCommentsWrittenOnAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := commentedArticle(t, dev, "article")
	block := commentOn(t, dev, article, blockComment)
	quoted := commentOn(t, dev, article, quotedComment)
	short := commentOn(t, dev, article, shortComment)
	show := []string{"article", "show", article, "--fields", "idReadable"}

	held := heldComments(t, dev, show...)

	require.Len(t, held, 3)
	requireWrittenInOrder(t, held)
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

// The same on an issue of the polygon, with four comments and a write among them: the list stands by the
// moment each comment was written, so rewriting the oldest leaves it the oldest, and the text that comes back
// is the new one. The last of the four is what a workflow of the polygon reads as a vote, and the show tells
// it from the others in nothing at all.
func TestIssueShowReadsTheCommentsWrittenOnAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	block := commentOn(t, dev, issue, blockComment)
	quoted := commentOn(t, dev, issue, quotedComment)
	short := commentOn(t, dev, issue, shortComment)
	vote := commentOn(t, dev, issue, voteComment)
	show := []string{"issue", "show", issue, "--fields", "idReadable"}

	held := heldComments(t, dev, show...)

	require.Len(t, held, 4)
	requireWrittenInOrder(t, held)
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

	after := heldComments(t, dev, show...)
	assert.Equal(t, []string{block, quoted, short, vote}, idsOfComments(t, after),
		"a write moves no comment: the list stands by created and not by updated")
	assert.Equal(t, rewritten, nodeAt(t, after[0], "text").Value)

	removed := runWith(t, dev.env(), "comment", "delete", issue, short)
	require.Equal(t, 0, removed.code, "stderr: %s", removed.stderr)
	assert.Equal(t, []string{block, quoted, vote}, commentIDs(t, dev, show...))
}
