package cli_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lines(source ...string) string {
	return strings.Join(source, "\n") + "\n"
}

var greeting = lines(
	`exports.command = { short: "Greet", long: "Say hello." };`,
	`exports.run = () => ({ said: "hello" });`,
)

const greeted = `said: "hello"` + "\n"

func scriptsRoot(dir string) string {
	return filepath.Join(dir, ".ytrack", "scripts")
}

func writeScripts(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, source := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	}
}

func scriptsHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	writeScripts(t, scriptsRoot(home), files)
	return home
}

func runScripts(t *testing.T, server *fake.Server, files map[string]string, argv ...string) outcome {
	t.Helper()
	return runWith(t, atHome(server, scriptsHome(t, files)), argv...)
}

func stderrCodes(t *testing.T, got outcome) []string {
	t.Helper()
	codes := []string{}
	for _, document := range documentsOf(t, got.stderr) {
		codes = append(codes, nodeAt(t, document, "code").Value)
	}
	return codes
}

func scriptFailedIn(home, name string, place ...detail) faultDocument {
	return faultDocument{code: "script_failed", details: append([]detail{fileDetail(filepath.Join(scriptsRoot(home), name))}, place...)}
}

func TestScriptIsCalledByThePathOfItsFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
		argv  []string
	}{
		{name: "a file at the root", files: map[string]string{"hello.js": greeting}, argv: []string{"hello"}},
		{name: "a file in a directory", files: map[string]string{"docs/map.js": greeting}, argv: []string{"docs", "map"}},
		{name: "a file two directories down", files: map[string]string{"acme/docs/map.js": greeting},
			argv: []string{"acme", "docs", "map"}},
		{name: "a file beside a library module",
			files: map[string]string{"hello.js": greeting, "lib.js": `exports.x = 1;`}, argv: []string{"hello"}},
		{name: "a file named as a library module of ytrack", files: map[string]string{"page.js": greeting},
			argv: []string{"page"}},
		{name: "a file starting with #!", files: map[string]string{"hello.js": "#!/usr/bin/env ytrack\n" + greeting},
			argv: []string{"hello"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), tc.files, tc.argv...)

			assert.Equal(t, outcome{stdout: greeted}, got)
		})
	}
}

func TestLibraryModuleIsNoCommand(t *testing.T) {
	t.Parallel()
	got := runScripts(t, fake.ServeNothing(t), map[string]string{"lib.js": `exports.x = 1;`}, "lib")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
}

func TestScriptIsFoundThroughASymlinkedDirectory(t *testing.T) {
	t.Parallel()
	plugin, home := t.TempDir(), t.TempDir()
	writeScripts(t, plugin, map[string]string{"acme/hello.js": greeting})
	require.NoError(t, os.MkdirAll(scriptsRoot(home), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(plugin, "acme"), filepath.Join(scriptsRoot(home), "acme")))

	got := runWith(t, atHome(fake.ServeNothing(t), home), "acme", "hello")

	assert.Equal(t, outcome{stdout: greeted}, got)
}

// The project root is looked up from PWD, and PWD counts only while it names the working directory.
func calledFromAProject(t *testing.T) []string {
	t.Helper()
	project := t.TempDir()
	writeScripts(t, scriptsRoot(project), map[string]string{"outer.js": greeting})
	nested := filepath.Join(project, "nested")
	writeScripts(t, scriptsRoot(nested), map[string]string{"x/a.js": greeting})
	called := filepath.Join(nested, "deep")
	require.NoError(t, os.MkdirAll(called, 0o755))
	t.Chdir(called)
	home := scriptsHome(t, map[string]string{"x/b.js": greeting, "y.js": greeting})
	return append(atHome(fake.ServeNothing(t), home), "PWD="+called)
}

func TestScriptOfTheNearestProjectRootOrOfTheUserIsACommand(t *testing.T) {
	env := calledFromAProject(t)
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a command of the nearest project root", argv: []string{"x", "a"}},
		{name: "a command of the user under a word of its own", argv: []string{"y"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, outcome{stdout: greeted}, runWith(t, env, tc.argv...))
		})
	}
}

func TestScriptHiddenByAnotherRootIsNoCommand(t *testing.T) {
	env := calledFromAProject(t)
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a command of the user under a word the project holds", argv: []string{"x", "b"}},
		{name: "a command of a project root further up", argv: []string{"outer"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, runWith(t, env, tc.argv...)))
		})
	}
}

func TestBuiltinCommandHidesAScriptUnderItsWordAndWarnsOfIt(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+listedDEV+`]`))
	env := atHome(server, scriptsHome(t, map[string]string{"issue/close.js": greeting, "project.js": greeting}))
	tests := []struct {
		name  string
		argv  []string
		code  int
		codes []string
	}{
		{name: "a script under the word of a builtin command", argv: []string{"issue", "close", "DEV-1"}, code: 1,
			codes: []string{"script_failed", "bad_usage"}},
		{name: "a builtin script under a hidden word", argv: []string{"project", "list"}, code: 0,
			codes: []string{"script_failed"}},
		{name: "the help of the command", argv: []string{"issue", "--help"}, code: 0,
			codes: []string{"script_failed"}},
		{name: "the help of ytrack", argv: []string{"--help"}, code: 0,
			codes: []string{"script_failed", "script_failed"}},
		{name: "another command", argv: []string{"auth", "--help"}, code: 0, codes: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, env, tc.argv...)

			assert.Equal(t, tc.code, got.code)
			assert.Equal(t, tc.codes, stderrCodes(t, got))
		})
	}
}

func TestScriptBesideADirectoryOfTheSameNameIsNoCommand(t *testing.T) {
	t.Parallel()
	home := scriptsHome(t, map[string]string{"docs.js": greeting, "docs/map.js": greeting})
	env := atHome(fake.ServeNothing(t), home)
	want := scriptFailedIn(home, "docs.js")
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the script", argv: []string{"docs"}},
		{name: "a script in the directory", argv: []string{"docs", "map"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, requireFault(t, runWith(t, env, tc.argv...)))
		})
	}
}

func TestDirectoryOfBrokenScriptsAloneIsNoCommand(t *testing.T) {
	t.Parallel()
	env := atHome(fake.ServeNothing(t), scriptsHome(t, map[string]string{"deploy/x.js": `exports.command = 1;`,
		"ok.js": greeting}))

	offered := completingWith(t, env, "").names()

	assert.Contains(t, offered, "ok")
	assert.NotContains(t, offered, "deploy")
}

func TestHelpWarnsOfTheScriptsThatEnterNoCommandTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
		argv  []string
		codes []string
	}{
		{name: "a script beside a directory of the same name", files: map[string]string{"docs.js": greeting,
			"docs/map.js": greeting}, argv: []string{"--help"}, codes: []string{"script_failed"}},
		{name: "an unreadable declaration", files: map[string]string{"bad.js": `exports.command = { short: x };`},
			argv: []string{"--help"}, codes: []string{"script_failed"}},
		{name: "an unreadable declaration under the command", files: map[string]string{"docs/bad.js": `exports.command = 1;`},
			argv: []string{"docs", "--help"}, codes: []string{"script_failed"}},
		{name: "an unreadable declaration under another command", files: map[string]string{"docs/bad.js": `exports.command = 1;`,
			"acme/hello.js": greeting}, argv: []string{"acme", "--help"}, codes: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), tc.files, tc.argv...)

			assert.Equal(t, 0, got.code)
			assert.Equal(t, tc.codes, stderrCodes(t, got))
		})
	}
}

func TestUnreadableDeclarationFailsTheCallAtItsPlace(t *testing.T) {
	t.Parallel()
	home := scriptsHome(t, map[string]string{"bad.js": lines(
		`// a command`,
		`exports.command = { short: "Bad", long: "Bad.", flags: { mode: { type: "string", usage: "m" + "n" } } };`,
	)})

	got := runWith(t, atHome(fake.ServeNothing(t), home), "bad")

	assert.Equal(t, scriptFailedIn(home, "bad.js", detail{"line", 2}, detail{"column", 89}), requireFault(t, got))
}

func TestDeclarationThatIsNoPureLiteralIsUnreadable(t *testing.T) {
	t.Parallel()
	const head = `exports.command = { short: "Bad", long: "Bad.", `
	tests := []struct {
		name   string
		source string
	}{
		{name: "a name", source: `const s = "Bad"; exports.command = { short: s, long: "Bad." };`},
		{name: "a call", source: `exports.command = { short: String("Bad"), long: "Bad." };`},
		{name: "a template with a value", source: "exports.command = { short: `Bad ${1}`, long: \"Bad.\" };"},
		{name: "a spread", source: `exports.command = { ...{ short: "Bad", long: "Bad." } };`},
		{name: "a key it does not know", source: head + `hidden: true };`},
		{name: "no short", source: `exports.command = { long: "Bad." };`},
		{name: "no long", source: `exports.command = { short: "Bad" };`},
		{name: "a short of two lines", source: `exports.command = { short: "Bad\nworse", long: "Bad." };`},
		{name: "an argument of no type", source: head + `args: [{ name: "id" }] };`},
		{name: "an argument named twice", source: head +
			`args: [{ name: "id", type: "string" }, { name: "id", type: "string" }] };`},
		{name: "a flag of a type it does not know", source: head + `flags: { n: { type: "float", usage: "n" } } };`},
		{name: "a flag with no usage", source: head + `flags: { n: { type: "int" } } };`},
		{name: "choices of a number flag", source: head + `flags: { n: { type: "int", usage: "n", choices: ["1"] } } };`},
		{name: "a flag named as an argument", source: head +
			`args: [{ name: "id", type: "string" }], flags: { id: { type: "string", usage: "id" } } };`},
		{name: "a flag named help", source: head + `flags: { help: { type: "bool", usage: "h" } } };`},
		{name: "an example that is no object", source: head + `example: [1] };`},
		{name: "a syntax error", source: head},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), map[string]string{"bad.js": tc.source}, "bad")

			assert.Equal(t, "script_failed", requireFault(t, got).code)
		})
	}
}
