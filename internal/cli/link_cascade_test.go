package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// partnersUnder is the issues the document holds at the other end of the links of that phrase, by the id each
// of them is printed as; the order they stand in is the server's own and no scenario reads anything into it.
func partnersUnder(t *testing.T, stdout, phrase string) []string {
	t.Helper()
	slot := nodeAt(t, requireMapping(t, "stdout", stdout), "links", phrase)
	require.Equal(t, yaml.SequenceNode, slot.Kind, "stdout: %q", stdout)
	readable := make([]string, 0, len(slot.Content))
	for _, record := range slot.Content {
		readable = append(readable, nodeAt(t, record, "idReadable").Value)
	}
	return readable
}

// An issue has one parent and no more, so writing a second one takes it away from the first rather than
// standing beside it. Nothing in the call names the link that goes away, and the document says it went: what a
// write is answered with is the whole of the issue's links afterwards, not the slot the call wrote in.
func TestLinkAddOfTheDevInstanceTakesASubtaskFromItsFormerParent(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	child := aContractIssue(t, dev, "subtask")
	former := aContractIssue(t, dev, "the parent it starts under")
	parent := aContractIssue(t, dev, "the parent it ends under")

	filed := runWith(t, dev.env(), "link", "add", child, "subtask of", former)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{former}, partnersUnder(t, filed.stdout, "subtask of"))

	got := runWith(t, dev.env(), "link", "add", child, "subtask of", parent)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{{"total", 1}, {"returned", 1}, {"truncated", false}},
		requireDocument(t, got.stdout)[:3])
	assert.Equal(t, []string{parent}, partnersUnder(t, got.stdout, "subtask of"))

	// The issue that was the parent held the other end of that link, and it holds nothing now: the write took
	// away a link of an issue it never named.
	left := runWith(t, dev.env(), "link", "list", former)

	require.Equal(t, 0, left.code, "stderr: %s", left.stderr)
	assert.Equal(t, []detail{{"total", 0}, {"returned", 0}, {"truncated", false}},
		requireDocument(t, left.stdout)[:3])
	assert.NotContains(t, keysOf(nodeAt(t, requireMapping(t, "stdout", left.stdout), "links")), "parent for")
}

// The Duplicates workflow of DEV moves the issue at the duplicate end of the link the moment the
// link is written: it is resolved, and its state is Duplicate. Where that issue is the partner, the answer to
// the write carries both, under the names the caller asked a partner by. Where it is the issue the call was
// made on, the document does not carry them at all: a write is answered with the links of that issue and not
// with its own fields, so the state the workflow gave it is read with issue show and nowhere else.
func TestLinkAddPrintsWhatTheWorkflowMovedOnThePartnerAndNotOnTheIssue(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	original := aContractIssue(t, dev, "the one they are all of")
	first := aContractIssue(t, dev, "the one named as partner")
	second := aContractIssue(t, dev, "the one named first")

	got := runWith(t, dev.env(), "link", "add", original, "is duplicated by", first,
		"--fields", "+resolved,customFields")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	record := nodeAt(t, requireMapping(t, "stdout", got.stdout), "links", "is duplicated by")
	assert.Equal(t, first, nodeAt(t, record, "idReadable").Value)
	assert.Equal(t, "Duplicate", nodeAt(t, record, "customFields", "State").Value)
	assert.Regexp(t, instantForm, nodeAt(t, record, "resolved").Value)

	// The other end of the same type is the same workflow moving the issue the call was made on instead. The
	// partner is asked for by the very names that showed the move a moment ago, and it stands where it was.
	written := runWith(t, dev.env(), "link", "add", second, "duplicates", original,
		"--fields", "+resolved,customFields")

	require.Equal(t, 0, written.code, "stderr: %s", written.stderr)
	assert.Empty(t, written.stderr)
	document := requireMapping(t, "stdout", written.stdout)
	partner := nodeAt(t, document, "links", "duplicates")
	assert.Equal(t, original, nodeAt(t, partner, "idReadable").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, partner, "resolved")), "stdout: %q", written.stdout)
	assert.Equal(t, "Новая", nodeAt(t, partner, "customFields", "State").Value)
	// Nothing of the issue the call was made on stands in the document: the keys are the counts and the links,
	// and a field of that issue has nowhere to be printed.
	assert.Equal(t, []string{"total", "returned", "truncated", "links"}, keysOf(document))
	assert.NotContains(t, written.stdout, "Duplicate", "the write printed a state of the issue it was made on")

	// What the workflow did to that issue is on the issue, and it takes the command that prints an issue.
	moved := runWith(t, dev.env(), "issue", "show", second, "--fields", "customFields", "--comments=0")

	require.Equal(t, 0, moved.code, "stderr: %s", moved.stderr)
	assert.Equal(t, "Duplicate",
		nodeAt(t, requireMapping(t, "stdout", moved.stdout), "customFields", "State").Value)
}

// What the Duplicates workflow did when the link was written is not undone when the link is taken away:
// the issue it moved is still Duplicate afterwards, while the link itself is gone from both ends. The state is
// on the issue and no document of a link carries it, so it takes the command that prints an issue to see it.
func TestLinkRemoveOfTheDevInstanceLeavesTheDuplicateWhereTheWorkflowPutIt(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	original := aContractIssue(t, dev, "the one they are all of")
	duplicate := aContractIssue(t, dev, "the one the workflow moves")

	filed := runWith(t, dev.env(), "link", "add", original, "is duplicated by", duplicate)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{duplicate}, partnersUnder(t, filed.stdout, "is duplicated by"))
	require.Equal(t, "Duplicate", stateOf(t, dev, duplicate))

	got := runWith(t, dev.env(), "link", "remove", original, "is duplicated by", duplicate)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []detail{
		{"idReadable", original},
		{"removed", []detail{{"is duplicated by", []any{[]detail{{"idReadable", duplicate}}}}}},
	}, requireDocument(t, got.stdout))

	held := runWith(t, dev.env(), "link", "list", duplicate)

	require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
	assert.Empty(t, keysOf(nodeAt(t, requireMapping(t, "stdout", held.stdout), "links")))
	assert.Equal(t, "Duplicate", stateOf(t, dev, duplicate))
}

// stateOf is the State the polygon holds the issue in, which is where a workflow's doing is read and where no
// document of a link has it.
func stateOf(t *testing.T, dev *upstream, issue string) string {
	t.Helper()
	got := runWith(t, dev.env(), "issue", "show", issue, "--fields", "customFields", "--comments=0")
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "customFields", "State").Value
}

// An issue that would be a subtask of its own subtask is a cycle, and the server is the one that knows
// it. What it said goes on word for word, HTML entities and all, and the refusal is one the caller can answer:
// nothing was written, and the links of the issue stand as they did.
func TestLinkAddRefusesTheCycleTheDevInstanceWillNotWrite(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	child := aContractIssue(t, dev, "subtask")
	parent := aContractIssue(t, dev, "parent")

	filed := runWith(t, dev.env(), "link", "add", child, "subtask of", parent)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{parent}, partnersUnder(t, filed.stdout, "subtask of"))
	before := runWith(t, dev.env(), "link", "list", child)
	require.Equal(t, 0, before.code, "stderr: %s", before.stderr)

	got := runWith(t, dev.env(), "link", "add", parent, "subtask of", child)

	found := requireRefusal(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, []detail{{"issue", parent}, {"phrase", "subtask of"}, {"partner", child}}, found.details[1:4])
	assert.Equal(t, 400, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, "invalid_properties", detailNamed(t, found, "upstream_error"))
	said, ok := detailNamed(t, found, "upstream_message").(string)
	require.True(t, ok, "stderr: %q", got.stderr)
	assert.Contains(t, said, "циклическая связь")
	assert.Contains(t, said, "&mdash;", "the text the server sent was rewritten")

	after := runWith(t, dev.env(), "link", "list", child)

	require.Equal(t, 0, after.code, "stderr: %s", after.stderr)
	assert.Equal(t, before.stdout, after.stdout)
}

// An original that becomes a duplicate itself hands its own duplicates over to the new original: the
// workflow rewrites the link of an issue no call named, and the answer to the write is where that is seen. The
// issue the call was made on holds the issue it named and the duplicate of that issue beside it, because the
// document is the whole of its links after the write rather than the one link the write asked for.
func TestLinkAddOfTheDevInstanceGathersTheDuplicatesOfTheIssueItTakesOver(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	duplicate := aContractIssue(t, dev, "the duplicate that moves")
	original := aContractIssue(t, dev, "the original that becomes a duplicate")
	second := aContractIssue(t, dev, "the original they both end under")

	filed := runWith(t, dev.env(), "link", "add", duplicate, "duplicates", original)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{original}, partnersUnder(t, filed.stdout, "duplicates"))

	got := runWith(t, dev.env(), "link", "add", second, "is duplicated by", original)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{{"total", 2}, {"returned", 2}, {"truncated", false}},
		requireDocument(t, got.stdout)[:3])
	assert.ElementsMatch(t, []string{original, duplicate}, partnersUnder(t, got.stdout, "is duplicated by"))

	// The link that moved is the one of the issue nobody named, and it reads the same from its other end.
	moved := runWith(t, dev.env(), "link", "list", duplicate)

	require.Equal(t, 0, moved.code, "stderr: %s", moved.stderr)
	assert.Equal(t, []string{second}, partnersUnder(t, moved.stdout, "duplicates"))
}
