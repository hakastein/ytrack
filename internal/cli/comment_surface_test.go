package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listedCommand is one line of the block: the name a caller writes, and the line printed beside it. cobra
// pads the name out to a column, so the two stand apart at the first space.
type listedCommand struct {
	name string
	text string
}

// commandsListed is what cobra prints under Available Commands, which is the whole of what a group offers: a
// command ytrack does not build stands nowhere in it, and a caller reads the group by this block.
func commandsListed(t *testing.T, help string) []listedCommand {
	t.Helper()
	_, listed, found := strings.Cut(help, "Available Commands:\n")
	require.True(t, found, "the help prints no commands at all: %q", help)
	var commands []listedCommand
	for _, line := range strings.Split(listed, "\n") {
		if strings.TrimSpace(line) == "" {
			break
		}
		name, text, _ := strings.Cut(strings.TrimSpace(line), " ")
		commands = append(commands, listedCommand{name: name, text: strings.TrimSpace(text)})
	}
	return commands
}

// availableCommands is the names alone, for a scenario that holds the group to the verbs it offers.
func availableCommands(t *testing.T, help string) []string {
	t.Helper()
	var names []string
	for _, command := range commandsListed(t, help) {
		names = append(names, command.name)
	}
	return names
}

func TestCommentGroupHasNoCommandThatShowsOneComment(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "comment", "show", "DEV-1", "7-1")

	want := faultDocument{code: "bad_usage"}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

func TestCommentHelpNamesItsVerbsAndWhereCommentsAreRead(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"comment", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"create", "delete", "list", "update"}, availableCommands(t, got.stdout))
}

// The group itself stands among the commands of ytrack, so a caller who knows nothing of it finds it
// where they find the rest.
func TestRootHelpNamesTheCommentGroup(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"--help"})

	assert.Equal(t, 0, got.code)
	assert.Contains(t, availableCommands(t, got.stdout), "comment")
}
