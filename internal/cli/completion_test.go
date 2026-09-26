package cli_test

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rootCommands() []string {
	return []string{"activity", "article", "attachment", "auth", "comment", "completion", "field", "issue",
		"link", "project", "tag", "time", "user"}
}

const (
	shellOffersFileNames   = ":0"
	shellOffersNoFileNames = ":4"
)

type completed struct {
	suggestions []string
	directive   string
}

func requireCompleted(t *testing.T, got outcome) completed {
	t.Helper()
	require.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	require.True(t, strings.HasSuffix(got.stdout, "\n"), "the answer ends in no newline: %q", got.stdout)
	lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
	directive := lines[len(lines)-1]
	require.True(t, strings.HasPrefix(directive, ":"), "the last line is no directive: %q", got.stdout)
	return completed{suggestions: lines[:len(lines)-1], directive: directive}
}

func completing(t *testing.T, words ...string) completed {
	t.Helper()
	return completingWith(t, envOf(fake.ServeNothing(t)), words...)
}

func completingWith(t *testing.T, env []string, words ...string) completed {
	t.Helper()
	return requireCompleted(t, runWith(t, env, append([]string{"__complete"}, words...)...))
}

func (c completed) names() []string {
	names := []string{}
	for _, suggestion := range c.suggestions {
		name, _, _ := strings.Cut(suggestion, "\t")
		names = append(names, name)
	}
	return names
}

func TestCompleteOffersTheCommandsUnderTheWordsTyped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
		names []string
	}{
		{name: "the root, nothing written yet", words: []string{""}, names: rootCommands()},
		{name: "the root, a name begun", words: []string{"att"}, names: []string{"attachment"}},
		{name: "a command, nothing written yet", words: []string{"project", ""}, names: []string{"list", "show"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.names, completing(t, tc.words...).names())
		})
	}
}

func TestCompleteOffersTheFlagsOfACommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
		names []string
	}{
		{name: "two dashes", words: []string{"auth", "logout", "--"}, names: []string{"--global", "--help"}},
		{name: "a flag begun", words: []string{"auth", "logout", "--g"}, names: []string{"--global"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.names, completing(t, tc.words...).names())
		})
	}
}

func TestCompleteOffersAFlagWithoutTheBackquotesOfPflag(t *testing.T) {
	t.Parallel()
	answered := completing(t, "project", "show", "--f")

	require.Equal(t, []string{"--fields"}, answered.names())
	_, text, _ := strings.Cut(answered.suggestions[0], "\t")
	assert.NotEmpty(t, text)
	assert.NotContains(t, text, "`")
}

func TestCompleteNoDescOffersTheNamesWithoutTheText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
	}{
		{name: "the subcommands of a command", words: []string{"attachment", ""}},
		{name: "the flags of a command", words: []string{"project", "show", "--"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			described := completing(t, tc.words...)
			require.NotEqual(t, described.names(), described.suggestions)

			got := runWith(t, envOf(fake.ServeNothing(t)), append([]string{"__completeNoDesc"}, tc.words...)...)

			assert.Equal(t, described.names(), requireCompleted(t, got).suggestions)
		})
	}
}

func TestCompleteOffersTheSubcommandsPastAFlagAndItsValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
	}{
		{name: "a flag and its value", words: []string{"project", "--fields", "x", ""}},
		{name: "a flag joined to its value", words: []string{"project", "--fields=x", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, completing(t, "project", "").names(), completing(t, tc.words...).names())
		})
	}
}

func TestCompleteOffersNothingForACommandLineThatNamesNoCommand(t *testing.T) {
	t.Parallel()
	got := runWith(t, envOf(fake.ServeNothing(t)), "__complete", "bogus", "")

	assert.Equal(t, outcome{stdout: shellOffersNoFileNames + "\n"}, got)
}

func TestCompleteWithNoCommandLineIsRefused(t *testing.T) {
	t.Parallel()
	got := runWith(t, envOf(fake.ServeNothing(t)), "__complete")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
}

func TestCompleteReadsNoEnvironmentOfTheProcess(t *testing.T) {
	unset := completing(t, "attachment", "")
	log := filepath.Join(t.TempDir(), "completion.log")
	t.Setenv("COBRA_COMPLETION_DESCRIPTIONS", "false")
	t.Setenv("YTRACK_COMPLETION_DESCRIPTIONS", "false")
	t.Setenv("BASH_COMP_DEBUG_FILE", log)

	assert.Equal(t, unset, completing(t, "attachment", ""))
	assert.NoFileExists(t, log)
}

func TestCompleteLeavesFileNamesToTheShellOnlyWhereAPathStands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		words     []string
		directive string
	}{
		{name: "the argument that is a path", words: []string{"attachment", "create", "DEV-1", ""},
			directive: shellOffersFileNames},
		{name: "the argument before it", words: []string{"attachment", "create", ""},
			directive: shellOffersNoFileNames},
		{name: "past the last argument", words: []string{"attachment", "create", "DEV-1", "report.pdf", ""},
			directive: shellOffersNoFileNames},
		{name: "the name of a flag where the path would stand", words: []string{"attachment", "create", "DEV-1", "--"},
			directive: shellOffersNoFileNames},
		{name: "the value of a flag where the path would stand", words: []string{"attachment", "create", "DEV-1", "--fields", ""},
			directive: shellOffersNoFileNames},
		{name: "the path past a flag and its value", words: []string{"attachment", "create", "--fields", "name", "DEV-1", ""},
			directive: shellOffersFileNames},
		{name: "the path past a flag carrying its value", words: []string{"attachment", "create", "--fields=name", "DEV-1", ""},
			directive: shellOffersFileNames},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.directive, completing(t, tc.words...).directive)
		})
	}
}

func TestCompleteOffersNothingWhereTheCommandWouldTakeNoWord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
	}{
		{name: "past the one value of a closed set", words: []string{"completion", "bash", ""}},
		{name: "past a word that is no subcommand of the command", words: []string{"project", "bogus", ""}},
		{name: "past a flag of the root that no command under it takes", words: []string{"--version", ""}},
		{name: "the value of a flag where a subcommand would stand", words: []string{"project", "--fields", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{}, completing(t, tc.words...).names())
		})
	}
}

func TestCompleteOffersNoCommandHiddenInTheTree(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{}, completing(t, "no-").names())
}

func TestCompletionPrintsTheScriptOfTheShellItIsGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		shell    string
		generate func(root *cobra.Command, script io.Writer) error
	}{
		{shell: "bash", generate: func(root *cobra.Command, script io.Writer) error { return root.GenBashCompletionV2(script, true) }},
		{shell: "zsh", generate: func(root *cobra.Command, script io.Writer) error { return root.GenZshCompletion(script) }},
		{shell: "fish", generate: func(root *cobra.Command, script io.Writer) error { return root.GenFishCompletion(script, true) }},
		{shell: "powershell", generate: func(root *cobra.Command, script io.Writer) error {
			return root.GenPowerShellCompletionWithDesc(script)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			t.Parallel()
			var script bytes.Buffer
			require.NoError(t, tc.generate(&cobra.Command{Use: "ytrack"}, &script))

			got := runWith(t, envOf(fake.ServeNothing(t)), "completion", tc.shell)

			assert.Equal(t, outcome{stdout: script.String()}, got)
		})
	}
}

func TestCompletionRefusesAShellItHasNoScriptFor(t *testing.T) {
	t.Parallel()
	got := runWith(t, envOf(fake.ServeNothing(t)), "completion", "tcsh")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
}

func TestCompleteOffersTheShellsOfTheCompletionCommand(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"bash", "zsh", "fish", "powershell"}, completing(t, "completion", "").names())
}

func TestCompleteOffersTheCategoriesOfTheActivities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
		names []string
	}{
		{name: "a category not begun", words: []string{"activity", "list", "DEV-1", "--category", ""},
			names: strings.Split(activityCategories, ",")},
		{name: "a category begun", words: []string{"activity", "list", "--category", "Vcs"},
			names: []string{"VcsChangeCategory"}},
		{name: "a category past the issue and another category",
			words: []string{"activity", "list", "DEV-1", "--category", "LinksCategory", "--category", "Com"},
			names: []string{"CommentTextCategory", "CommentsCategory"}},
		{name: "a category carried by the flag", words: []string{"activity", "list", "--category=Li"},
			names: []string{"--category=LinksCategory"}},
		{name: "the value of another flag", words: []string{"activity", "list", "--fields", ""},
			names: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.names, completing(t, tc.words...).names())
		})
	}
}
