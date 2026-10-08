package cli_test

import (
	"regexp"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const checkDeclaration = `exports.definition = { short: "Check an issue", long: "Check an issue " + "against a report.", ` +
	`args: [{ name: "id", type: "string", usage: "the issue" }, { name: "report", type: "path", usage: "the report" }],` +
	` flags: [` +
	` { name: "mode", type: "string", usage: "a ` + "`mode`" + `", choices: ["fast", "slow"], default: "fast" },` +
	` { name: "count", type: "int", usage: "how many" },` +
	` { name: "verbose", type: "bool", usage: "say more" },` +
	` { name: "tag", type: "string", multiple: true, usage: "a tag; repeatable", choices: ["a", "b", "c"] },` +
	` { name: "set", type: "pair", multiple: true, usage: "a ` + "`Name=value`" + `; repeatable" },` +
	` { name: "spent", type: "duration", usage: "how long" },` +
	` { name: "fields", type: "fields", default: "id,summary" } ] };`

var checkReaching = lines(
	`const { projects } = require("ytrack/v1");`,
	checkDeclaration,
	`exports.command = () => projects.list({ fields: "shortName", limit: 1, skip: 0 });`,
)

var checkEchoing = lines(
	checkDeclaration,
	`exports.command = (id, report, flags) => ({ id, report, flags });`,
)

func TestScriptCallIsRefusedByItsDeclarationBeforeItRuns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a value outside the choices", argv: []string{"check", "DEV-1", "r.txt", "--mode", "medium"}},
		{name: "a value outside the choices of a repeatable flag", argv: []string{"check", "DEV-1", "r.txt", "--tag", "d"}},
		{name: "an argument too many", argv: []string{"check", "DEV-1", "r.txt", "extra"}},
		{name: "an argument too few", argv: []string{"check", "DEV-1"}},
		{name: "an int wider than 32 bits", argv: []string{"check", "DEV-1", "r.txt", "--count", "3000000000"}},
		{name: "a flag given twice", argv: []string{"check", "DEV-1", "r.txt", "--mode", "fast", "--mode", "slow"}},
		{name: "a pair with no =", argv: []string{"check", "DEV-1", "r.txt", "--set", "A"}},
		{name: "a pair with no name", argv: []string{"check", "DEV-1", "r.txt", "--set", "=1"}},
		{name: "a duration of days", argv: []string{"check", "DEV-1", "r.txt", "--spent", "P1D"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runScripts(t, server, map[string]string{"check.js": checkReaching}, tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestScriptRunIsGivenTheArgumentsInOrderAndTheFlagsLast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "the arguments and the flags with a default",
			argv: []string{"check", "DEV-1", "r.txt"},
			want: "id: \"DEV-1\"\nreport: \"r.txt\"\nflags:\n  mode: \"fast\"\n  fields: \"id,summary\"",
		},
		{
			name: "every flag",
			argv: []string{"check", "DEV-1", "r.txt", "--mode", "slow", "--count", "3", "--verbose", "--tag", "b", "--tag", "a",
				"--set", "A=1", "--set", "B=x=y", "--set", "A=2", "--spent", "PT1H30M", "--fields", "key"},
			want: "id: \"DEV-1\"\nreport: \"r.txt\"\nflags:\n  mode: \"slow\"\n  count: 3\n  verbose: true\n  tag:\n" +
				"    - \"b\"\n    - \"a\"\n  set:\n    A:\n      - \"1\"\n      - \"2\"\n    B: \"x=y\"\n  spent: 90\n" +
				"  fields: \"key\"",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), map[string]string{"check.js": checkEchoing}, tc.argv...)

			assert.Equal(t, outcome{stdout: tc.want + "\n"}, got)
		})
	}
}

func TestFieldsFlagAddsToItsDefault(t *testing.T) {
	t.Parallel()
	source := lines(checkDeclaration, `exports.command = (id, report, flags) => ({ fields: flags.fields });`)
	tests := []struct {
		name  string
		given []string
		want  string
	}{
		{name: "an expression added to the default", given: []string{"--fields", "+key"}, want: "id,summary,key"},
		{name: "an empty expression", given: []string{"--fields", ""}, want: "id,summary"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			argv := append([]string{"check", "DEV-1", "r.txt"}, tc.given...)

			got := runScripts(t, fake.ServeNothing(t), map[string]string{"check.js": source}, argv...)

			assert.Equal(t, outcome{stdout: "fields: \"" + tc.want + "\"\n"}, got)
		})
	}
}

func TestCompleteOffersWhatTheDeclarationOfAScriptTakes(t *testing.T) {
	t.Parallel()
	env := atHome(fake.ServeNothing(t), scriptsHome(t, map[string]string{"acme/check.js": checkReaching}))
	tests := []struct {
		name      string
		words     []string
		names     []string
		directive string
	}{
		{name: "the first word of the script", words: []string{"acm"}, names: []string{"acme"},
			directive: shellOffersNoFileNames},
		{name: "the script under its word", words: []string{"acme", ""}, names: []string{"check"},
			directive: shellOffersNoFileNames},
		{name: "its flags", words: []string{"acme", "check", "--"},
			names: []string{"--count", "--fields", "--help", "--mode", "--set", "--spent", "--tag", "--verbose"}, directive: shellOffersNoFileNames},
		{name: "the choices of a flag", words: []string{"acme", "check", "--mode", ""}, names: []string{"fast", "slow"},
			directive: shellOffersNoFileNames},
		{name: "the choices of a repeatable flag", words: []string{"acme", "check", "--tag", "a", "--tag", ""},
			names: []string{"a", "b", "c"}, directive: shellOffersNoFileNames},
		{name: "an argument that is a string", words: []string{"acme", "check", ""}, names: []string{},
			directive: shellOffersNoFileNames},
		{name: "an argument that is a path", words: []string{"acme", "check", "DEV-1", ""}, names: []string{},
			directive: shellOffersFileNames},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := completingWith(t, env, tc.words...)

			assert.Equal(t, tc.directive, got.directive)
			assert.Equal(t, tc.names, got.names())
		})
	}
}

func TestHelpAndCompletionRunNoScript(t *testing.T) {
	t.Parallel()
	source := lines(
		`require("ytrack/v1").projects.list({ fields: "shortName", limit: 1, skip: 0 });`,
		`exports.definition = { short: "Reach the instance on load", long: "Reach the instance when loaded." };`,
		`exports.command = () => ({});`,
	)
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the help of the script", argv: []string{"reach", "--help"}},
		{name: "the help of ytrack", argv: []string{"--help"}},
		{name: "a completion", argv: []string{"__complete", "reach", "--"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runScripts(t, server, map[string]string{"reach.js": source}, tc.argv...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestHelpListsTheScriptsUnderTheirRoot(t *testing.T) {
	t.Parallel()
	home := scriptsHome(t, map[string]string{"acme/check.js": checkReaching})

	got := runWith(t, atHome(fake.ServeNothing(t), home), "--help")

	require.Equal(t, 0, got.code)
	assert.Regexp(t, regexp.QuoteMeta(scriptsRoot(home))+`:\n  acme\s`, got.stdout)
}
