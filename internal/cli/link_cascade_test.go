package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func targetsUnder(t *testing.T, stdout, phrase string) []string {
	t.Helper()
	link := nodeAt(t, requireMapping(t, "stdout", stdout), "links", phrase)
	require.Equal(t, yaml.SequenceNode, link.Kind, "stdout: %q", stdout)
	readable := make([]string, 0, len(link.Content))
	for _, record := range link.Content {
		readable = append(readable, nodeAt(t, record, "idReadable").Value)
	}
	return readable
}

func TestLinkAddOfTheDevInstanceTakesASubtaskFromItsFormerParent(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	child := aContractIssue(t, dev, "subtask")
	former := aContractIssue(t, dev, "the parent it starts under")
	parent := aContractIssue(t, dev, "the parent it ends under")

	filed := runWith(t, dev.env(), "link", "add", child, "subtask of", former)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{former}, targetsUnder(t, filed.stdout, "subtask of"))

	got := runWith(t, dev.env(), "link", "add", child, "subtask of", parent)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{{"total", 1}, {"returned", 1}, {"truncated", false}},
		requireDocument(t, got.stdout)[:3])
	assert.Equal(t, []string{parent}, targetsUnder(t, got.stdout, "subtask of"))

	left := runWith(t, dev.env(), "link", "list", former)

	require.Equal(t, 0, left.code, "stderr: %s", left.stderr)
	assert.Equal(t, []detail{{"total", 0}, {"returned", 0}, {"truncated", false}},
		requireDocument(t, left.stdout)[:3])
	assert.NotContains(t, keysOf(nodeAt(t, requireMapping(t, "stdout", left.stdout), "links")), "parent for")
}

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

	written := runWith(t, dev.env(), "link", "add", second, "duplicates", original,
		"--fields", "+resolved,customFields")

	require.Equal(t, 0, written.code, "stderr: %s", written.stderr)
	assert.Empty(t, written.stderr)
	document := requireMapping(t, "stdout", written.stdout)
	target := nodeAt(t, document, "links", "duplicates")
	assert.Equal(t, original, nodeAt(t, target, "idReadable").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, target, "resolved")), "stdout: %q", written.stdout)
	assert.Equal(t, "Новая", nodeAt(t, target, "customFields", "State").Value)
	assert.Equal(t, []string{"total", "returned", "truncated", "links"}, keysOf(document))
	assert.NotContains(t, written.stdout, "Duplicate", "the write printed a state of the issue it was made on")

	moved := runWith(t, dev.env(), "issue", "show", second, "--fields", "customFields", "--comments=0")

	require.Equal(t, 0, moved.code, "stderr: %s", moved.stderr)
	assert.Equal(t, "Duplicate",
		nodeAt(t, requireMapping(t, "stdout", moved.stdout), "customFields", "State").Value)
}

func TestLinkRemoveOfTheDevInstanceLeavesTheDuplicateWhereTheWorkflowPutIt(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	original := aContractIssue(t, dev, "the one they are all of")
	duplicate := aContractIssue(t, dev, "the one the workflow moves")

	filed := runWith(t, dev.env(), "link", "add", original, "is duplicated by", duplicate)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{duplicate}, targetsUnder(t, filed.stdout, "is duplicated by"))
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

func stateOf(t *testing.T, dev *upstream, issue string) string {
	t.Helper()
	got := runWith(t, dev.env(), "issue", "show", issue, "--fields", "customFields", "--comments=0")
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "customFields", "State").Value
}

func TestLinkAddRefusesTheCycleTheDevInstanceWillNotWrite(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	child := aContractIssue(t, dev, "subtask")
	parent := aContractIssue(t, dev, "parent")

	filed := runWith(t, dev.env(), "link", "add", child, "subtask of", parent)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{parent}, targetsUnder(t, filed.stdout, "subtask of"))
	before := runWith(t, dev.env(), "link", "list", child)
	require.Equal(t, 0, before.code, "stderr: %s", before.stderr)

	got := runWith(t, dev.env(), "link", "add", parent, "subtask of", child)

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, []detail{{"issue", parent}, {"phrase", "subtask of"}, {"target", child}}, found.details[1:4])
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

func TestLinkAddOfTheDevInstanceGathersTheDuplicatesOfTheIssueItTakesOver(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	duplicate := aContractIssue(t, dev, "the duplicate that moves")
	original := aContractIssue(t, dev, "the original that becomes a duplicate")
	second := aContractIssue(t, dev, "the original they both end under")

	filed := runWith(t, dev.env(), "link", "add", duplicate, "duplicates", original)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{original}, targetsUnder(t, filed.stdout, "duplicates"))

	got := runWith(t, dev.env(), "link", "add", second, "is duplicated by", original)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{{"total", 2}, {"returned", 2}, {"truncated", false}},
		requireDocument(t, got.stdout)[:3])
	assert.ElementsMatch(t, []string{original, duplicate}, targetsUnder(t, got.stdout, "is duplicated by"))

	moved := runWith(t, dev.env(), "link", "list", duplicate)

	require.Equal(t, 0, moved.code, "stderr: %s", moved.stderr)
	assert.Equal(t, []string{second}, targetsUnder(t, moved.stdout, "duplicates"))
}
