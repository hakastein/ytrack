package cli_test

import (
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

type treeCommand struct {
	line  string
	short string
	flags []string
}

func everyCommand(t *testing.T) []treeCommand {
	t.Helper()
	root := treeCommand{line: "ytrack", flags: completing(t, "--").names()}
	return append([]treeCommand{root}, commandsOfferedUnder(t, nil)...)
}

func commandsOfferedUnder(t *testing.T, words []string) []treeCommand {
	t.Helper()
	// Past a flag of its own a command offers the values of its argument and no subcommand.
	values := completing(t, append(slices.Clone(words), "--help", "")...).names()
	var found []treeCommand
	for _, offered := range completing(t, append(slices.Clone(words), "")...).suggestions {
		name, short, described := strings.Cut(offered, "\t")
		if !described && slices.Contains(values, name) {
			continue
		}
		path := append(slices.Clone(words), name)
		found = append(found, treeCommand{
			line:  strings.Join(append([]string{"ytrack"}, path...), " "),
			short: short,
			flags: completing(t, append(slices.Clone(path), "--")...).names(),
		})
		found = append(found, commandsOfferedUnder(t, path)...)
	}
	return found
}

func declaring(commands []treeCommand, flag string) []string {
	lines := []string{}
	for _, command := range commands {
		if slices.Contains(command.flags, flag) {
			lines = append(lines, command.line)
		}
	}
	return lines
}

func whoseShortBreaks(commands []treeCommand, holds func(short string) bool) []string {
	lines := []string{}
	for _, command := range commands {
		if !holds(command.short) {
			lines = append(lines, command.line+": "+command.short)
		}
	}
	return lines
}

func TestEveryCommandOfferedCarriesAShortOfTheOneForm(t *testing.T) {
	t.Parallel()
	offered := commandsOfferedUnder(t, nil)
	tests := []struct {
		name  string
		holds func(short string) bool
	}{
		{name: "one line", holds: func(short string) bool { return !strings.ContainsAny(short, "\r\n") }},
		{name: "a capital first", holds: func(short string) bool {
			first, _ := utf8.DecodeRuneInString(short)
			return unicode.IsUpper(first)
		}},
		{name: "no full stop at the end", holds: func(short string) bool { return !strings.HasSuffix(short, ".") }},
		{name: "at most 60 characters", holds: func(short string) bool { return utf8.RuneCountInString(short) <= 60 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, whoseShortBreaks(offered, tc.holds))
		})
	}
}

func TestNoCommandTakesTheAddressOrTheTokenAsAFlag(t *testing.T) {
	t.Parallel()
	commands := everyCommand(t)
	for _, flag := range []string{"--base-url", "--token"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, declaring(commands, flag))
		})
	}
}

func TestTheRootAloneTakesVersion(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"ytrack"}, declaring(everyCommand(t), "--version"))
}

func TestVersionHasNoShorthand(t *testing.T) {
	t.Parallel()
	assert.Equal(t, completing(t, "").names(), completing(t, "-v=true", "").names())
}

func TestAListThatCannotAskForAPageTakesNoLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
	}{
		{name: "link list", words: []string{"link", "list", "--"}},
		{name: "field list", words: []string{"field", "list", "--"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.NotContains(t, completing(t, tc.words...).names(), "--limit")
		})
	}
}
