package cli_test

import (
	"regexp"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const checkDeclaration = `exports.command = { short: "Check an issue", long: "Check an issue against a report.", ` +
	`args: [{ name: "id", type: "string" }, { name: "report", type: "path" }], flags: {` +
	` mode: { type: "string", usage: "a ` + "`mode`" + `", choices: ["fast", "slow"] },` +
	` count: { type: "int", usage: "how many" },` +
	` verbose: { type: "bool", usage: "say more" },` +
	` tag: { type: "strings", usage: "a tag; repeatable", choices: ["a", "b", "c"] } } };`

var checkReaching = lines(
	`const { project } = require("ytrack/v1");`,
	checkDeclaration,
	`exports.run = () => project.list({ fields: "shortName", limit: 1, skip: 0 });`,
)

var checkEchoing = lines(
	checkDeclaration,
	`exports.run = (input) => ({ input });`,
)

func TestScriptCallIsRefusedByItsDeclarationBeforeItRuns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an unknown flag", argv: []string{"check", "DEV-1", "r.txt", "--bogus"}},
		{name: "a value outside the choices", argv: []string{"check", "DEV-1", "r.txt", "--mode", "medium"}},
		{name: "a value outside the choices of a repeatable flag", argv: []string{"check", "DEV-1", "r.txt", "--tag", "d"}},
		{name: "an argument too many", argv: []string{"check", "DEV-1", "r.txt", "extra"}},
		{name: "an argument too few", argv: []string{"check", "DEV-1"}},
		{name: "an int that is no number", argv: []string{"check", "DEV-1", "r.txt", "--count", "two"}},
		{name: "an int wider than 32 bits", argv: []string{"check", "DEV-1", "r.txt", "--count", "3000000000"}},
		{name: "a flag given twice", argv: []string{"check", "DEV-1", "r.txt", "--mode", "fast", "--mode", "slow"}},
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

func TestScriptRunIsGivenOnlyTheDeclaredInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "the arguments alone",
			argv: []string{"check", "DEV-1", "r.txt"},
			want: "input:\n  id: \"DEV-1\"\n  report: \"r.txt\"",
		},
		{
			name: "every flag",
			argv: []string{"check", "DEV-1", "r.txt", "--mode", "slow", "--count", "3", "--verbose", "--tag", "b", "--tag", "a"},
			want: "input:\n  id: \"DEV-1\"\n  report: \"r.txt\"\n  mode: \"slow\"\n  count: 3\n  verbose: true\n  tag:\n    - \"b\"\n    - \"a\"",
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
			names: []string{"--count", "--help", "--mode", "--tag", "--verbose"}, directive: shellOffersNoFileNames},
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
		`require("ytrack/v1").project.list({ fields: "shortName", limit: 1, skip: 0 });`,
		`exports.command = { short: "Reach the instance on load", long: "Reach the instance when loaded." };`,
		`exports.run = () => ({});`,
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

func TestHelpOfAScriptPrintsItsExample(t *testing.T) {
	t.Parallel()
	source := lines(
		`exports.command = { short: "Show", long: "Show it.", example: { id: "DEV-1", text: "one\ntwo", "Odd key": [1, true, null] } };`,
		`exports.run = () => ({});`,
	)

	got := runScripts(t, fake.ServeNothing(t), map[string]string{"show.js": source}, "show", "--help")

	require.Equal(t, 0, got.code)
	assert.Contains(t, got.stdout, "  id: \"DEV-1\"\n  text: |-\n    one\n    two\n  \"Odd key\":\n    - 1\n    - true\n    - null\n")
}
