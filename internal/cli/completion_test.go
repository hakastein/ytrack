package cli_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
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

func (c completed) names() []string {
	names := []string{}
	for _, suggestion := range c.suggestions {
		name, _, _ := strings.Cut(suggestion, "\t")
		names = append(names, name)
	}
	return names
}

func TestCompleteOffersTheCommandsOfTheRoot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "nothing written yet", argv: []string{"__complete", ""}, names: rootCommands()},
		{name: "a letter several names begin with", argv: []string{"__complete", "a"},
			names: []string{"activity", "article", "attachment", "auth"}},
		{name: "a name begun", argv: []string{"__complete", "att"}, names: []string{"attachment"}},
		{name: "a name no command answers to", argv: []string{"__complete", "zz"}, names: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, shellOffersNoFileNames, answered.directive)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersTheSubcommandsOfACommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "nothing written yet", argv: []string{"__complete", "project", ""}, names: []string{"list", "show"}},
		{name: "a subcommand begun", argv: []string{"__complete", "project", "sh"}, names: []string{"show"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, shellOffersNoFileNames, answered.directive)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersTheFlagsOfACommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "a dash alone", argv: []string{"__complete", "project", "show", "-"}, names: []string{"--fields", "--help"}},
		{name: "two dashes", argv: []string{"__complete", "project", "show", "--"}, names: []string{"--fields", "--help"}},
		{name: "a flag begun", argv: []string{"__complete", "project", "list", "--l"}, names: []string{"--limit"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, shellOffersNoFileNames, answered.directive)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersAFlagWithoutTheBackquotesOfPflag(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	answered := requireCompleted(t, runWith(t, server.Env(), "__complete", "project", "show", "--f"))

	require.Len(t, answered.suggestions, 1)
	name, text, found := strings.Cut(answered.suggestions[0], "\t")
	assert.Equal(t, "--fields", name)
	assert.True(t, found, "the flag came back with nothing beside it")
	assert.NotEmpty(t, text)
	assert.NotContains(t, text, "`")
	assert.Empty(t, server.Requests())
}

func TestCompleteNoDescPrintsNoTextBesideAName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "the commands of the root", argv: []string{"__completeNoDesc", ""}, names: rootCommands()},
		{name: "a letter several names begin with", argv: []string{"__completeNoDesc", "a"},
			names: []string{"activity", "article", "attachment", "auth"}},
		{name: "the subcommands of a command", argv: []string{"__completeNoDesc", "attachment", ""},
			names: []string{"create", "delete", "list"}},
		{name: "the flags of a command", argv: []string{"__completeNoDesc", "project", "show", "--"},
			names: []string{"--fields", "--help"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, tc.names, answered.suggestions, "a name came back with something beside it")
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteFindsTheCommandPastTheFlagsAlreadyTyped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "a flag and its value, then a flag", argv: []string{"__complete", "project", "list", "--fields", "idReadable", "--"},
			names: []string{"--fields", "--help", "--limit", "--skip"}},
		{name: "a flag and its value, then an argument", argv: []string{"__complete", "project", "list", "--fields", "idReadable", ""},
			names: []string{}},
		{name: "a flag joined to its value", argv: []string{"__complete", "project", "--fields=x", ""},
			names: []string{"list", "show"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, shellOffersNoFileNames, answered.directive)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersNothingForACommandLineThatNamesNoCommand(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "__complete", "bogus", "")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, shellOffersNoFileNames+"\n", got.stdout)
	assert.Empty(t, got.stderr)
	assert.Empty(t, server.Requests())
}

func TestCompleteWithNoCommandLineIsRefused(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"__complete", "__completeNoDesc"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), protocol)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteReadsNoEnvironmentOfTheProcess(t *testing.T) {
	t.Setenv("COBRA_COMPLETION_DESCRIPTIONS", "false")
	t.Setenv("YTRACK_COMPLETION_DESCRIPTIONS", "false")
	server := fake.ServeNothing(t)

	answered := requireCompleted(t, runWith(t, server.Env(), "__complete", "attachment", ""))

	assert.Equal(t, []string{"create", "delete", "list"}, answered.names())
	for _, suggestion := range answered.suggestions {
		assert.Contains(t, suggestion, "\t", "the description was dropped, which only the process's environment does")
	}
	assert.Empty(t, server.Requests())
}

func TestCompleteWritesNoFileNamedByTheEnvironment(t *testing.T) {
	log := filepath.Join(t.TempDir(), "completion.log")
	t.Setenv("BASH_COMP_DEBUG_FILE", log)
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "__complete", "bogus", "")

	assert.Equal(t, 0, got.code)
	assert.NoFileExists(t, log)
	assert.Empty(t, server.Requests())
}

func TestCompleteLeavesFileNamesToTheShellOnlyWhereTheArgumentIsAPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		argv      []string
		directive string
	}{
		{name: "the argument that is a path", argv: []string{"__complete", "attachment", "create", "DEV-1", ""},
			directive: shellOffersFileNames},
		{name: "the argument before it, which is an identifier", argv: []string{"__complete", "attachment", "create", ""},
			directive: shellOffersNoFileNames},
		{name: "past the last argument the command takes", argv: []string{"__complete", "attachment", "create", "DEV-1", "report.pdf", ""},
			directive: shellOffersNoFileNames},
		{name: "an argument that is an identifier", argv: []string{"__complete", "project", "show", ""}, directive: shellOffersNoFileNames},
		{name: "an argument of a subcommand beside it", argv: []string{"__complete", "attachment", "list", ""}, directive: shellOffersNoFileNames},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.directive, answered.directive)
			assert.Empty(t, answered.names())
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteLeavesNoFileNamesWhereAFlagOrItsValueIsCompleted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		argv      []string
		names     []string
		directive string
	}{
		{name: "the name of a flag where the path would stand", argv: []string{"__complete", "attachment", "create", "DEV-1", "--"},
			names: []string{"--fields", "--help"}, directive: shellOffersNoFileNames},
		{name: "the value of a flag where the path would stand", argv: []string{"__complete", "attachment", "create", "DEV-1", "--fields", ""},
			names: []string{}, directive: shellOffersNoFileNames},
		{name: "the value of a flag where a subcommand would stand", argv: []string{"__complete", "project", "--fields", ""},
			names: []string{}, directive: shellOffersNoFileNames},
		{name: "the path past a flag and its value", argv: []string{"__complete", "attachment", "create", "--fields", "name", "DEV-1", ""},
			names: []string{}, directive: shellOffersFileNames},
		{name: "the path past a flag carrying its value", argv: []string{"__complete", "attachment", "create", "--fields=name", "DEV-1", ""},
			names: []string{}, directive: shellOffersFileNames},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, tc.directive, answered.directive)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersNothingWhereTheCommandWouldTakeNoWord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "past the one value of a closed set", argv: []string{"__complete", "completion", "bash", ""}},
		{name: "past a word that is no subcommand of the command", argv: []string{"__complete", "project", "bogus", ""}},
		{name: "past the one argument of a command", argv: []string{"__complete", "project", "show", "DEV", ""}},
		{name: "past a flag of the root that no command under it takes", argv: []string{"__complete", "--version", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, []string{}, answered.names())
			assert.Equal(t, shellOffersNoFileNames, answered.directive)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersNoCommandHiddenInTheTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "the name of the stand-in begun", argv: []string{"__complete", "no-"}, names: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Empty(t, server.Requests())
		})
	}
}

func theShellsYtrackHasAScriptFor(t *testing.T) []string {
	t.Helper()
	answered := requireCompleted(t, runWith(t, fake.ServeNothing(t).Env(), "__complete", "completion", ""))
	require.NotEmpty(t, answered.names())
	return answered.names()
}

func TestCompletionPrintsAScriptForEveryShellItNames(t *testing.T) {
	t.Parallel()
	for _, shell := range theShellsYtrackHasAScriptFor(t) {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "completion", shell)

			assert.Equal(t, 0, got.code)
			assert.Empty(t, got.stderr)
			require.NotEmpty(t, got.stdout)
			first, _, _ := strings.Cut(got.stdout, "\n")
			assert.Contains(t, first, "ytrack", "the script opens without naming the binary")
			assert.Contains(t, got.stdout, "# "+shell, "the script names no shell of its own")
			assert.Contains(t, got.stdout, "__complete", "the script asks nobody for its suggestions")
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompletionRefusesAnythingButOneShellItNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a shell there is no script for", argv: []string{"completion", "tcsh"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersTheShellsOfTheCompletionCommand(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	answered := requireCompleted(t, runWith(t, server.Env(), "__complete", "completion", ""))

	assert.Equal(t, []string{"bash", "zsh", "fish", "powershell"}, answered.names())
	assert.Equal(t, shellOffersNoFileNames, answered.directive)
	assert.Empty(t, server.Requests())
}

func commandsWithSubcommands() []string {
	groups := []string{}
	for _, name := range rootCommands() {
		if name != "completion" {
			groups = append(groups, name)
		}
	}
	return groups
}

func TestCompleteCarriesTheTextOfEveryCommandItOffers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{{name: "the commands of the root", argv: []string{"__complete", ""}}}
	for _, group := range commandsWithSubcommands() {
		tests = append(tests, struct {
			name string
			argv []string
		}{name: "the subcommands of " + group, argv: []string{"__complete", group, ""}})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			require.NotEmpty(t, answered.suggestions)
			for _, suggestion := range answered.suggestions {
				_, text, found := strings.Cut(suggestion, "\t")
				assert.True(t, found, "%q is offered with nothing beside it", suggestion)
				assert.NotEmpty(t, text, "%q is offered with an empty line beside it", suggestion)
			}
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCompleteOffersTheCategoriesOfTheActivities(t *testing.T) {
	t.Parallel()
	every := strings.Split(activityCategories, ",")
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "the subcommand of activity", argv: []string{"__complete", "activity", ""}, names: []string{"list"}},
		{name: "a category not begun", argv: []string{"__complete", "activity", "list", "DEV-1", "--category", ""},
			names: every},
		{name: "a category begun", argv: []string{"__complete", "activity", "list", "--category", "Vcs"},
			names: []string{"VcsChangeCategory"}},
		{name: "a category past the issue and another category",
			argv:  []string{"__complete", "activity", "list", "DEV-1", "--category", "LinksCategory", "--category", "Com"},
			names: []string{"CommentTextCategory", "CommentsCategory"}},
		{name: "a category carried by the flag", argv: []string{"__complete", "activity", "list", "--category=Li"},
			names: []string{"--category=LinksCategory"}},
		{name: "the value of another flag", argv: []string{"__complete", "activity", "list", "--fields", ""},
			names: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			answered := requireCompleted(t, runWith(t, server.Env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, shellOffersNoFileNames, answered.directive)
			assert.Empty(t, server.Requests())
		})
	}
}
