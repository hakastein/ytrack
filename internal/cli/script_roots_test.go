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
	`exports.command = { short: "Greet" };`,
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

func scriptEnv(server *fake.Server, home string) []string {
	return append(envOf(server), "HOME="+home)
}

func runScripts(t *testing.T, server *fake.Server, files map[string]string, argv ...string) outcome {
	t.Helper()
	return runWith(t, scriptEnv(server, scriptsHome(t, files)), argv...)
}

func stderrCodes(t *testing.T, got outcome) []string {
	t.Helper()
	codes := []string{}
	for _, document := range documentsOf(t, got.stderr) {
		codes = append(codes, nodeAt(t, document, "code").Value)
	}
	return codes
}

func scriptFile(path string) detail {
	return detail{"file", path}
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
	plugin := t.TempDir()
	writeScripts(t, plugin, map[string]string{"acme/hello.js": greeting})
	home := scriptsHome(t, nil)
	require.NoError(t, os.MkdirAll(scriptsRoot(home), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(plugin, "acme"), filepath.Join(scriptsRoot(home), "acme")))

	got := runWith(t, scriptEnv(fake.ServeNothing(t), home), "acme", "hello")

	assert.Equal(t, outcome{stdout: greeted}, got)
}

// Sequential: the project root is looked up from PWD, which must be the directory of the process.
func TestFirstWordBelongsToTheNearestProjectRootThenToTheUser(t *testing.T) {
	project := t.TempDir()
	writeScripts(t, scriptsRoot(project), map[string]string{"outer.js": greeting})
	nested := filepath.Join(project, "nested")
	writeScripts(t, scriptsRoot(nested), map[string]string{"x/a.js": greeting})
	called := filepath.Join(nested, "deep")
	require.NoError(t, os.MkdirAll(called, 0o755))
	home := scriptsHome(t, map[string]string{"x/b.js": greeting, "y.js": greeting})
	t.Chdir(called)
	env := append(envOf(fake.ServeNothing(t)), "HOME="+home, "PWD="+called)
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{name: "a command of the nearest project root", argv: []string{"x", "a"}, want: 0},
		{name: "a command of the user under a word the project holds", argv: []string{"x", "b"}, want: 1},
		{name: "a command of the user under a word of its own", argv: []string{"y"}, want: 0},
		{name: "a command of a project root further up", argv: []string{"outer"}, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, runWith(t, env, tc.argv...).code)
		})
	}
}

func TestBuiltinCommandHidesAScriptUnderItsWordAndWarnsOfIt(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+listedDEV+`]`))
	env := scriptEnv(server, scriptsHome(t, map[string]string{"issue/close.js": greeting, "project.js": greeting}))
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
	env := scriptEnv(fake.ServeNothing(t), home)
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
			got := runWith(t, env, tc.argv...)

			want := faultDocument{code: "script_failed", details: []detail{scriptFile(filepath.Join(scriptsRoot(home), "docs.js"))}}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
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
		`exports.command = { short: "Bad", flags: { mode: { type: "string", usage: "m" + "n" } } };`,
	)})

	got := runWith(t, scriptEnv(fake.ServeNothing(t), home), "bad")

	want := faultDocument{code: "script_failed", details: []detail{
		scriptFile(filepath.Join(scriptsRoot(home), "bad.js")), {"line", 2}, {"column", 75},
	}}
	assert.Equal(t, want, requireFault(t, got))
}

func TestDeclarationThatIsNoPureLiteralIsUnreadable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{name: "a name", source: `const s = "Bad"; exports.command = { short: s };`},
		{name: "a call", source: `exports.command = { short: String("Bad") };`},
		{name: "a template with a value", source: "exports.command = { short: `Bad ${1}` };"},
		{name: "a spread", source: `exports.command = { ...{ short: "Bad" } };`},
		{name: "a key it does not know", source: `exports.command = { short: "Bad", hidden: true };`},
		{name: "no short", source: `exports.command = { long: "Bad" };`},
		{name: "a short of two lines", source: `exports.command = { short: "Bad\nworse" };`},
		{name: "an argument of no type", source: `exports.command = { short: "Bad", args: [{ name: "id" }] };`},
		{name: "a flag of a type it does not know",
			source: `exports.command = { short: "Bad", flags: { n: { type: "float", usage: "n" } } };`},
		{name: "a flag with no usage", source: `exports.command = { short: "Bad", flags: { n: { type: "int" } } };`},
		{name: "choices of a number flag",
			source: `exports.command = { short: "Bad", flags: { n: { type: "int", usage: "n", choices: ["1"] } } };`},
		{name: "a flag named as an argument", source: `exports.command = { short: "Bad", ` +
			`args: [{ name: "id", type: "string" }], flags: { id: { type: "string", usage: "id" } } };`},
		{name: "a flag named help", source: `exports.command = { short: "Bad", flags: { help: { type: "bool", usage: "h" } } };`},
		{name: "an example that is no object", source: `exports.command = { short: "Bad", example: [1] };`},
		{name: "a syntax error", source: `exports.command = { short: "Bad" `},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), map[string]string{"bad.js": tc.source}, "bad")

			assert.Equal(t, "script_failed", requireFault(t, got).code)
		})
	}
}
