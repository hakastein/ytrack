package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type listedCommand struct {
	name string
	text string
}

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

func availableCommands(t *testing.T, help string) []string {
	t.Helper()
	var names []string
	for _, command := range commandsListed(t, help) {
		names = append(names, command.name)
	}
	return names
}

func TestCommentHasNoSubcommandThatShowsOneComment(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "comment", "show", "DEV-1", "7-1")

	want := faultDocument{code: "bad_usage"}
	assert.Equal(t, want, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestCommentHelpNamesItsSubcommandsAndWhereCommentsAreRead(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"comment", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"create", "delete", "list", "update"}, availableCommands(t, got.stdout))
}

func TestRootHelpNamesTheCommentCommand(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"--help"})

	assert.Equal(t, 0, got.code)
	assert.Contains(t, availableCommands(t, got.stdout), "comment")
}
