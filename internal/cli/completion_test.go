package cli_test

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The commands of the root, in the order the answer prints them, which is the order cobra keeps them in.
func rootCommands() []string {
	return []string{"activity", "article", "attachment", "auth", "comment", "completion", "field", "issue",
		"link", "project", "tag", "time", "user"}
}

// completed is what the protocol answered: the suggestions in order, and the directive of the last line.
type completed struct {
	suggestions []string
	directive   string
}

// requireCompleted is the answer of a call that completed something: exit code 0, nothing on stderr, and a
// last line of a colon and a number. The shells read stdout alone and throw stderr away (ADR-0009), so an
// answer that said anything there would be an answer the caller never sees.
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

// names is what each line offers, without the text a shell prints beside it.
func (c completed) names() []string {
	names := []string{}
	for _, suggestion := range c.suggestions {
		name, _, _ := strings.Cut(suggestion, "\t")
		names = append(names, name)
	}
	return names
}

// The first word of a command line is an entity of YouTrack, and what the caller has begun to write is
// what is offered back. The text beside a command is its Short, which is where a shell reads the line it
// prints under the name.
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
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, ":4", answered.directive)
			assert.Empty(t, server.requests())
		})
	}
}

// A command carries its own line into the answer, so the shell has something to print beside the name.
func TestCompleteCarriesTheTextOfACommand(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	answered := requireCompleted(t, runWith(t, server.env(), "__complete", "attachment"))

	require.Len(t, answered.suggestions, 1)
	name, text, found := strings.Cut(answered.suggestions[0], "\t")
	assert.Equal(t, "attachment", name)
	assert.True(t, found, "the command came back with nothing beside it")
	assert.NotEmpty(t, text)
	assert.Empty(t, server.requests())
}

// The word after an entity is a verb, and the verbs offered are the ones that entity has.
func TestCompleteOffersTheVerbsOfAGroup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "nothing written yet", argv: []string{"__complete", "project", ""}, names: []string{"list", "show"}},
		{name: "a verb begun", argv: []string{"__complete", "project", "sh"}, names: []string{"show"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, ":4", answered.directive)
			assert.Empty(t, server.requests())
		})
	}
}

// A word begun with a dash is a flag, and the flags offered are the command's own together with --help,
// which cobra puts on a command only as it runs it.
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
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, ":4", answered.directive)
			assert.Empty(t, server.requests())
		})
	}
}

// Pflag writes the name of a flag's value in backquotes inside the usage; what reaches the shell is the
// text without them, since a shell prints the line as it comes and quotes nothing back out.
func TestCompleteOffersAFlagWithoutTheBackquotesOfPflag(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	answered := requireCompleted(t, runWith(t, server.env(), "__complete", "project", "show", "--f"))

	require.Len(t, answered.suggestions, 1)
	name, text, found := strings.Cut(answered.suggestions[0], "\t")
	assert.Equal(t, "--fields", name)
	assert.True(t, found, "the flag came back with nothing beside it")
	assert.NotEmpty(t, text)
	assert.NotContains(t, text, "`")
	assert.Empty(t, server.requests())
}

// The second name of the protocol asks for the names alone: a shell that prints no descriptions asks for
// none, and nothing beside a name comes back to be cut off on the far side.
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
		{name: "the verbs of a group", argv: []string{"__completeNoDesc", "attachment", ""},
			names: []string{"create", "delete", "list"}},
		{name: "the flags of a command", argv: []string{"__completeNoDesc", "project", "show", "--"},
			names: []string{"--fields", "--help"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, tc.names, answered.suggestions, "a name came back with something beside it")
			assert.Empty(t, server.requests())
		})
	}
}

// A flag written before the word being completed does not hide the command: the command line is read as
// the call itself is read, flags and their values stripped out, in either form the flag is written in.
func TestCompleteFindsTheCommandPastTheFlagsAlreadyTyped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "a flag and its value, then a flag", argv: []string{"__complete", "project", "list", "--fields", "idReadable", "--"},
			names: []string{"--fields", "--help", "--limit", "--skip"}},
		// The command is found, and a list takes no argument at all, so there is nothing to offer under it.
		{name: "a flag and its value, then an argument", argv: []string{"__complete", "project", "list", "--fields", "idReadable", ""},
			names: []string{}},
		{name: "a flag joined to its value", argv: []string{"__complete", "project", "--fields=x", ""},
			names: []string{"list", "show"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, ":4", answered.directive)
			assert.Empty(t, server.requests())
		})
	}
}

// A command line naming no command of ytrack is answered with the directive and nothing else. It is no
// refusal: the shell throws stderr away, so a document written there would be a refusal nobody reads, and an
// empty answer is what "nothing to offer" looks like to every one of the four scripts.
func TestCompleteOffersNothingForACommandLineThatNamesNoCommand(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "__complete", "bogus", "")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, ":4\n", got.stdout)
	assert.Empty(t, got.stderr)
	assert.Empty(t, server.requests())
}

// The protocol without a command line is no call a shell makes, and it is the caller who wrote it: the
// answer is a document on stderr like any other refusal.
func TestCompleteWithNoCommandLineIsRefused(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"__complete", "__completeNoDesc"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), protocol)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// Cobra's own answer to the protocol reads two variables of the process's environment and drops every
// description when either says false, though Run was handed an environment of its own. ytrack's answer reads
// neither, so this stands green only while the protocol is answered before cobra.
func TestCompleteReadsNoEnvironmentOfTheProcess(t *testing.T) {
	t.Setenv("COBRA_COMPLETION_DESCRIPTIONS", "false")
	t.Setenv("YTRACK_COMPLETION_DESCRIPTIONS", "false")
	server := serveNothing(t)

	answered := requireCompleted(t, runWith(t, server.env(), "__complete", "attachment", ""))

	assert.Equal(t, []string{"create", "delete", "list"}, answered.names())
	for _, suggestion := range answered.suggestions {
		assert.Contains(t, suggestion, "\t", "the description was dropped, which only the process's environment does")
	}
	assert.Empty(t, server.requests())
}

// Cobra's own answer writes the reason it found nothing into the file named by BASH_COMP_DEBUG_FILE.
// ytrack's writes to the stdout it was handed and nowhere else, so no file appears.
func TestCompleteWritesNoFileNamedByTheEnvironment(t *testing.T) {
	log := filepath.Join(t.TempDir(), "completion.log")
	t.Setenv("BASH_COMP_DEBUG_FILE", log)
	server := serveNothing(t)

	got := runWith(t, server.env(), "__complete", "bogus", "")

	assert.Equal(t, 0, got.code)
	assert.NoFileExists(t, log)
	assert.Empty(t, server.requests())
}

// The shell is left to offer file names at the argument that is a path, and told not to anywhere
// else: a list of files where an identifier is wanted is a suggestion that is never right. Which argument is
// the path is what the command itself says, so the answer goes by the place the word stands in, not by the
// command it stands under.
func TestCompleteLeavesFileNamesToTheShellOnlyWhereTheArgumentIsAPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		argv      []string
		directive string
	}{
		{name: "the argument that is a path", argv: []string{"__complete", "attachment", "create", "DEV-1", ""},
			directive: ":0"},
		{name: "the argument before it, which is an identifier", argv: []string{"__complete", "attachment", "create", ""},
			directive: ":4"},
		{name: "past the last argument the command takes", argv: []string{"__complete", "attachment", "create", "DEV-1", "report.pdf", ""},
			directive: ":4"},
		{name: "an argument that is an identifier", argv: []string{"__complete", "project", "show", ""}, directive: ":4"},
		{name: "an argument of a verb beside it", argv: []string{"__complete", "attachment", "list", ""}, directive: ":4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.directive, answered.directive)
			assert.Empty(t, answered.names())
			assert.Empty(t, server.requests())
		})
	}
}

// A flag is not an argument, and the value of a flag is not one either: at the one command that
// takes a path, file names are offered where the path stands and nowhere near the flags. A flag written
// before the path does not move it — the place is counted in arguments, as the command counts them.
func TestCompleteLeavesNoFileNamesWhereAFlagOrItsValueIsCompleted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		argv      []string
		names     []string
		directive string
	}{
		{name: "the name of a flag where the path would stand", argv: []string{"__complete", "attachment", "create", "DEV-1", "--"},
			names: []string{"--fields", "--help"}, directive: ":4"},
		{name: "the value of a flag where the path would stand", argv: []string{"__complete", "attachment", "create", "DEV-1", "--fields", ""},
			names: []string{}, directive: ":4"},
		{name: "the value of a flag where a verb would stand", argv: []string{"__complete", "project", "--fields", ""},
			names: []string{}, directive: ":4"},
		{name: "the path past a flag and its value", argv: []string{"__complete", "attachment", "create", "--fields", "name", "DEV-1", ""},
			names: []string{}, directive: ":0"},
		{name: "the path past a flag carrying its value", argv: []string{"__complete", "attachment", "create", "--fields=name", "DEV-1", ""},
			names: []string{}, directive: ":0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, tc.directive, answered.directive)
			assert.Empty(t, server.requests())
		})
	}
}

// Nothing is offered that the same call would then refuse: past the arguments a command counts, past
// a word it never routed to, and past a flag the root keeps to itself, there is nothing to write at all. A
// suggestion taken from the shell there walks the caller into a refusal ytrack put in their hands itself.
func TestCompleteOffersNothingWhereTheCommandWouldTakeNoWord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "past the one value of a closed set", argv: []string{"__complete", "completion", "bash", ""}},
		{name: "past a word that is no verb of the group", argv: []string{"__complete", "project", "bogus", ""}},
		{name: "past the one argument of a command", argv: []string{"__complete", "project", "show", "DEV", ""}},
		{name: "past a flag of the root that no command under it takes", argv: []string{"__complete", "--version", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, []string{}, answered.names())
			assert.Equal(t, ":4", answered.directive)
			assert.Empty(t, server.requests())
		})
	}
}

// The stand-in cobra insists on for a help command stands in the tree the protocol answers from, and it
// is hidden there: what is kept out of the help is kept from the shell too. `ytrack no-help` is refused as any
// unknown command, so a name offered for it would be a name the call it completes then refuses.
func TestCompleteOffersNoCommandHiddenInTheTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "nothing written yet", argv: []string{"__complete", ""}, names: rootCommands()},
		{name: "the name of the stand-in begun", argv: []string{"__complete", "no-"}, names: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Empty(t, server.requests())
		})
	}
}

// theShellsYtrackHasAScriptFor is the one list of shells as a test outside the package sees it: the closed
// set of values the protocol offers for the argument of completion, which the command reads off that list.
// Every assertion below goes by it, so a shell added to the list is a shell these tests hold ytrack to.
func theShellsYtrackHasAScriptFor(t *testing.T) []string {
	t.Helper()
	answered := requireCompleted(t, runWith(t, serveNothing(t).env(), "__complete", "completion", ""))
	require.NotEmpty(t, answered.names())
	return answered.names()
}

// Every shell the command names gets a script, and the script is cobra's own, written off the live tree:
// it names ytrack, it names the shell it is for, and what it asks for every suggestion is the protocol. The
// shell's own name is looked for in the header rather than the first line, since zsh opens on #compdef ytrack.
func TestCompletionPrintsAScriptForEveryShellItNames(t *testing.T) {
	t.Parallel()
	for _, shell := range theShellsYtrackHasAScriptFor(t) {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "completion", shell)

			assert.Equal(t, 0, got.code)
			assert.Empty(t, got.stderr)
			require.NotEmpty(t, got.stdout)
			first, _, _ := strings.Cut(got.stdout, "\n")
			assert.Contains(t, first, "ytrack", "the script opens without naming the binary")
			assert.Contains(t, got.stdout, "# "+shell, "the script names no shell of its own")
			assert.Contains(t, got.stdout, "__complete", "the script asks nobody for its suggestions")
			assert.Empty(t, server.requests())
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
		{name: "no shell at all", argv: []string{"completion"}},
		{name: "two shells", argv: []string{"completion", "bash", "zsh"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The shells named in the help are the shells there is a script for, and each is named with the
// line that loads its script: both come off the one list, so the help can neither fall behind it nor run
// ahead of it. A name alone would not do — what the caller needs from the help is the line to write.
func TestCompletionHelpNamesEveryShellThereIsAScriptForAndHowToLoadIt(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "completion", "--help")

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	loading := loadingLinesOf(t, got.stdout)
	shells := theShellsYtrackHasAScriptFor(t)
	assert.ElementsMatch(t, shells, slices.Collect(maps.Keys(loading)),
		"the help and the protocol name different shells")
	for _, shell := range shells {
		assert.Contains(t, loading[shell], "ytrack completion "+shell,
			"the help names %s without saying what to write to load its script", shell)
	}
	assert.Empty(t, server.requests())
}

func loadingLinesOf(t *testing.T, help string) map[string]string {
	t.Helper()
	lines := map[string]string{}
	for _, line := range strings.Split(help, "\n") {
		indented, isIndented := strings.CutPrefix(line, "  ")
		name, loads, names := strings.Cut(indented, ": ")
		if isIndented && names && !strings.ContainsAny(name, " \t") {
			lines[name] = loads
		}
	}
	return lines
}

// The argument of completion is a closed set, so the shell offers the four names and no file name beside
// them. This is the list itself, written out once, and everything else about it is checked against this.
func TestCompleteOffersTheShellsOfTheCompletionCommand(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	answered := requireCompleted(t, runWith(t, server.env(), "__complete", "completion", ""))

	assert.Equal(t, []string{"bash", "zsh", "fish", "powershell"}, answered.names())
	assert.Equal(t, ":4", answered.directive)
	assert.Empty(t, server.requests())
}

// theGroupsOfYtrack is every command of the root that holds verbs under it. completion stands apart: what
// follows it is a shell out of a closed set, not a verb, and what it offers is checked by
// TestCompleteOffersTheShellsOfTheCompletionCommand instead.
func theGroupsOfYtrack() []string {
	groups := []string{}
	for _, name := range rootCommands() {
		if name != "completion" {
			groups = append(groups, name)
		}
	}
	return groups
}

// helpCalls is the help of the root and of every group, named for a subtest.
func helpCalls() []struct {
	name string
	argv []string
} {
	calls := []struct {
		name string
		argv []string
	}{{name: "the root", argv: []string{"--help"}}}
	for _, group := range theGroupsOfYtrack() {
		calls = append(calls, struct {
			name string
			argv []string
		}{name: group, argv: []string{group, "--help"}})
	}
	return calls
}

// Every command the protocol offers carries the line that says what it does, so the shell has something
// to print under the name. A name offered with nothing beside it is a name the caller has to try to find out
// what it is for.
func TestCompleteCarriesTheTextOfEveryCommandItOffers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{{name: "the commands of the root", argv: []string{"__complete", ""}}}
	for _, group := range theGroupsOfYtrack() {
		tests = append(tests, struct {
			name string
			argv []string
		}{name: "the verbs of " + group, argv: []string{"__complete", group, ""}})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			require.NotEmpty(t, answered.suggestions)
			for _, suggestion := range answered.suggestions {
				_, text, found := strings.Cut(suggestion, "\t")
				assert.True(t, found, "%q is offered with nothing beside it", suggestion)
				assert.NotEmpty(t, text, "%q is offered with an empty line beside it", suggestion)
			}
			assert.Empty(t, server.requests())
		})
	}
}

// The help lists what may be written and says beside every name what it does. help is not among them:
// ytrack has no such command, and cobra's stand-in for one answers every topic with a refusal. cobra lists a
// command called help even where it is hidden, so what keeps it out of the list is its name.
func TestHelpListsEveryCommandWithItsLineAndNoHelpCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range helpCalls() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			require.Equal(t, 0, got.code)
			assert.Empty(t, got.stderr)
			for _, listed := range commandsListed(t, got.stdout) {
				assert.NotEmpty(t, listed.text, "%q is listed with nothing beside it", listed.name)
				assert.NotEqual(t, "help", listed.name, "a command nobody can call is listed")
			}
			assert.Empty(t, server.requests())
		})
	}
}

// Completion is a command of the root like any other, and it is listed like any other: the caller who
// has not read a word of documentation meets it in the first help they print.
func TestHelpOfTheRootListsCompletion(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "--help")

	listed := commandsListed(t, got.stdout)
	idx := slices.IndexFunc(listed, func(c listedCommand) bool { return c.name == "completion" })
	require.GreaterOrEqual(t, idx, 0, "completion is not listed among the root's commands")
	assert.NotEmpty(t, listed[idx].text)
	assert.Empty(t, server.requests())
}

// The categories of the journal are a closed set of ytrack's own writing, so the value of --category is offered
// from it, in either form the flag is written in. No other value of a flag is: every other one is the server's
// to know, and a TAB sends no request.
func TestCompleteOffersTheCategoriesOfTheJournal(t *testing.T) {
	t.Parallel()
	every := strings.Split(activityCategories, ",")
	tests := []struct {
		name  string
		argv  []string
		names []string
	}{
		{name: "the verb of the journal", argv: []string{"__complete", "activity", ""}, names: []string{"list"}},
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
			server := serveNothing(t)

			answered := requireCompleted(t, runWith(t, server.env(), tc.argv...))

			assert.Equal(t, tc.names, answered.names())
			assert.Equal(t, ":4", answered.directive)
			assert.Empty(t, server.requests())
		})
	}
}
