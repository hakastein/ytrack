package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueUpdateMovesTheFieldOfTheDevInstanceNobodyWrote(t *testing.T) {
	t.Parallel()
	const resolvedState = "Закрыта"
	dev := devInstance(t)
	argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)

	filed := runWith(t, dev.env(), append(argv, "--field", "State=Новая")...)

	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	document := requireMapping(t, "stdout", filed.stdout)
	readable := nodeAt(t, document, "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	require.Nil(t, requireValue(t, nodeAt(t, document, "resolved")), "stdout: %q", filed.stdout)

	got := runWith(t, dev.env(), "issue", "update", readable, "--field", "State="+resolvedState)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, resolvedState, nodeAt(t, mapping, "customFields", "State").Value)
	assert.Regexp(t, instantForm, nodeAt(t, mapping, "resolved").Value)
}

func TestIssueUpdateTellsOfTheWriteAWorkflowOfTheDevInstanceUndid(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	readable := filedForUpdate(t, dev, "--field", "State=Закрыта")

	got := runWith(t, dev.env(), "issue", "update", readable,
		"--field", "State=Новая", "--field", "Fixed in build=13757")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", updateRequest(dev.url, readable, askedIssueFields)},
			{"issue", readable},
			{"mismatch", []any{[]detail{{"field", "Fixed in build"}, {"expected", "13757"}, {"actual", nil}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))

	read := runWith(t, dev.env(), "issue", "show", readable, "--comments=0", "--fields", "customFields")

	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	block := nodeAt(t, requireMapping(t, "stdout", read.stdout), "customFields")
	assert.Equal(t, "Новая", nodeAt(t, block, "State").Value)
	assert.NotContains(t, keysOf(block), "Fixed in build", "stdout: %q", read.stdout)
}
