package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A write moves more than what it wrote: closing an issue resolves it, and resolved is a field of the
// issue nobody named. It is printed because the document is the issue after the write and not a copy of the
// body, and it is not held against anything, because nothing was written into it.
func TestIssueUpdateMovesTheFieldOfTheDevInstanceNobodyWrote(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)

	filed := runWith(t, dev.env(), append(argv, "--field", "State=Новая")...)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	document := requireMapping(t, "stdout", filed.stdout)
	readable := nodeAt(t, document, "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	require.Nil(t, requireValue(t, nodeAt(t, document, "resolved")), "stdout: %q", filed.stdout)

	got := runWith(t, dev.env(), "issue", "update", readable, "--field", "State=Закрыта")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "Закрыта", nodeAt(t, mapping, "customFields", "State").Value)
	assert.Regexp(t, instantForm, nodeAt(t, mapping, "resolved").Value)
}

// A workflow of the project may undo a value in the same breath the write puts it there: the issue is
// reopened and the build it was fixed in is cleared, and the build the call wrote is cleared with it. The
// answer says so, and the check of the write refuses on it — but the write happened, and both halves of it
// stand on the issue afterwards. That is what the exit code of a refusal after an answer to a write is for:
// there is nothing here for the caller to send again.
func TestIssueUpdateTellsOfTheWriteAWorkflowOfTheDevInstanceUndid(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev, "--field", "State=Закрыта")

	got := runWith(t, dev.env(), "issue", "update", readable,
		"--field", "State=Новая", "--field", "Fixed in build=13757")

	want := refusal{
		code: "upstream_lied",
		details: []detail{
			{"request", updateRequest(dev.url, readable, askedIssueFields)},
			{"issue", readable},
			{"mismatch", []any{[]detail{{"field", "Fixed in build"}, {"written", "13757"}, {"arrived", nil}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))

	read := runWith(t, dev.env(), "issue", "show", readable, "--comments=0", "--fields", "customFields")

	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	block := nodeAt(t, requireMapping(t, "stdout", read.stdout), "customFields")
	assert.Equal(t, "Новая", nodeAt(t, block, "State").Value)
	assert.NotContains(t, keysOf(block), "Fixed in build", "stdout: %q", read.stdout)
}
