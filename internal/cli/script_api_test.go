package cli_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const apiHead = `const { project, fail, warn } = require("ytrack/v1");`

func running(body string) map[string]string {
	return map[string]string{"run.js": lines(apiHead, `exports.command = { short: "Run" };`, body)}
}

func TestScriptPrintsWhatItReturns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "the answer of a function, as its command prints it",
			body: `exports.run = () => project.show("DEV", { fields: "shortName,name" });`,
			want: "shortName: \"DEV\"\nname: \"DEVELOPMENT\"\n",
		},
		{
			name: "an answer put inside a value of its own",
			body: `exports.run = () => ({ found: project.list({ fields: "shortName,name", limit: 5, skip: 0 }).projects });`,
			want: "found:\n  - {shortName: \"DEV\", name: \"DEVELOPMENT\"}\n",
		},
		{
			name: "a value it built",
			body: `exports.run = () => ({ text: "one\ntwo", line: "one", "Odd key": 1.5, left: undefined, none: null, ` +
				`big: 2n ** 64n, list: [true, 2] });`,
			want: "text: |-\n  one\n  two\nline: \"one\"\n\"Odd key\": 1.5\nnone: null\nbig: 18446744073709551616\n" +
				"list:\n  - true\n  - 2\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/admin/projects" {
					fake.JSON(http.StatusOK, `[`+listedDEV+`]`)(w, r)
					return
				}
				fake.JSON(http.StatusOK, listedDEV)(w, r)
			})

			got := runScripts(t, server, running(tc.body), "run")

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestScriptReadsAnAnswerAsReadOnlyValues(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK,
		`{"shortName":"DEV","name":null,"startingNumber":9007199254740993,"$type":"Project"}`))
	body := lines(
		`exports.run = () => {`,
		`  const read = project.show("DEV", { fields: "shortName,name,startingNumber" });`,
		`  let written = true;`,
		`  try { read.shortName = "OPS"; } catch (e) { written = false; }`,
		`  return { absent: typeof read.leader, isNull: read.name === null, number: typeof read.startingNumber,`,
		`    keys: Object.keys(read), written, shortName: read.shortName };`,
		`};`,
	)

	got := runScripts(t, server, running(body), "run")

	want := "absent: \"undefined\"\nisNull: true\nnumber: \"bigint\"\n" +
		"keys:\n  - \"shortName\"\n  - \"name\"\n  - \"startingNumber\"\nwritten: false\nshortName: \"DEV\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestFaultOfAFunctionReachesTheScript(t *testing.T) {
	t.Parallel()
	body := lines(
		`exports.run = () => {`,
		`  try { project.show("DEV", { fields: "shortName" }); } catch (e) {`,
		`    return { code: e.code, wrote: e.wrote, error: e instanceof Error, request: typeof e.details.request };`,
		`  }`,
		`};`,
	)

	got := runScripts(t, fake.Serve(t, fake.JSON(http.StatusNotFound, `{"error":"Not Found"}`)), running(body), "run")

	want := "code: \"not_found\"\nwrote: false\nerror: true\nrequest: \"string\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestUncaughtFaultOfAFunctionIsPrintedAsItIs(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusNotFound, `{"error":"Not Found"}`))
	body := `exports.run = () => project.show("DEV", { fields: "shortName" });`

	fromScript := runScripts(t, server, running(body), "run")
	fromCommand := runWith(t, envOf(server), "project", "show", "DEV", "--fields", "shortName")

	assert.Equal(t, "not_found", requireFault(t, fromScript).code)
	assert.Equal(t, fromCommand, fromScript)
}

func TestScriptFailsAndWarnsWithACodeOfTheDictionary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want faultDocument
		exit int
	}{
		{name: "fail", body: `exports.run = () => fail("not_found", "no such issue", { id: "DEV-1" });`,
			want: faultDocument{code: "not_found", details: []detail{{"id", "DEV-1"}}}, exit: 1},
		{name: "fail with a code that may have written", body: `exports.run = () => fail("write_uncertain", "unknown");`,
			want: faultDocument{code: "write_uncertain"}, exit: 2},
		{name: "fail caught and thrown again",
			body: `exports.run = () => { try { fail("denied", "no"); } catch (e) { throw e; } };`,
			want: faultDocument{code: "denied"}, exit: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), running(tc.body), "run")

			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.want, requireFaultDocument(t, got))
		})
	}
}

func TestWarningOfAScriptGoesToStderrBeforeTheAnswer(t *testing.T) {
	t.Parallel()
	body := `exports.run = () => { warn("unknown_name", "no such tag", { name: "urgent" }); return { done: true }; };`

	got := runScripts(t, fake.ServeNothing(t), running(body), "run")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, "done: true\n", got.stdout)
	assert.Equal(t, "code: \"unknown_name\"\nmessage: \"no such tag\"\nname: \"urgent\"\n", got.stderr)
}

func TestDefectOfAScriptIsScriptFailedAtItsLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		body   string
		column int
	}{
		{name: "a name defined nowhere", body: `exports.run = () => missing();`, column: 28},
		{name: "an error thrown", body: `exports.run = () => { throw new Error("broken"); };`, column: 29},
		{name: "a value thrown", body: `exports.run = () => { throw "broken"; };`, column: 23},
		{name: "an argument of the wrong type", body: `exports.run = () => project.show(1, { fields: "id" });`, column: 33},
		{name: "a flag left out", body: `exports.run = () => project.show("DEV");`, column: 33},
		{name: "a flag it does not take", body: `exports.run = () => project.show("DEV", { fields: "id", limit: 1 });`, column: 33},
		{name: "a flag of the wrong type", body: `exports.run = () => project.list({ fields: "id", limit: "1", skip: 0 });`, column: 33},
		{name: "fields that add to the default", body: `exports.run = () => project.show("DEV", { fields: "+id" });`, column: 33},
		{name: "fields that name none", body: `exports.run = () => project.show("DEV", { fields: "" });`, column: 33},
		{name: "a code out of the dictionary", body: `exports.run = () => fail("broken", "no");`, column: 25},
		{name: "a warning with a code out of the dictionary", body: `exports.run = () => warn("broken", "no");`, column: 25},
		{name: "a value written into an answer",
			body: `exports.run = () => { project.list({ fields: "id", limit: 1, skip: 0 }).total = 5; };`, column: 73},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := scriptsHome(t, running(tc.body))
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

			got := runWith(t, scriptEnv(server, home), "run")

			want := faultDocument{code: "script_failed", details: []detail{
				scriptFile(filepath.Join(scriptsRoot(home), "run.js")), {"line", 3}, {"column", tc.column},
			}}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
}

func TestValueAScriptReturnsIsScriptFailedWhenYAMLHoldsNoSuch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "no run", body: `exports.go = () => ({});`},
		{name: "no exports", body: `module.exports = null;`},
		{name: "nothing", body: `exports.run = () => {};`},
		{name: "a string", body: `exports.run = () => "done";`},
		{name: "a list", body: `exports.run = () => [];`},
		{name: "a function", body: `exports.run = () => ({ f: () => 1 });`},
		{name: "NaN", body: `exports.run = () => ({ n: 0 / 0 });`},
		{name: "a Date", body: `exports.run = () => ({ at: new Date(0) });`},
		{name: "bytes", body: `exports.run = () => ({ data: new Uint8Array(2) });`},
		{name: "a symbol", body: `exports.run = () => ({ s: Symbol("s") });`},
		{name: "undefined in a list", body: `exports.run = () => ({ list: [undefined] });`},
		{name: "a value that holds itself", body: `exports.run = () => { const a = {}; a.a = a; return a; };`},
		{name: "a promise", body: `exports.run = async () => ({});`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := scriptsHome(t, running(tc.body))

			got := runWith(t, scriptEnv(fake.ServeNothing(t), home), "run")

			want := faultDocument{code: "script_failed", details: []detail{scriptFile(filepath.Join(scriptsRoot(home), "run.js"))}}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
}

func TestScriptRequiresModulesOfItsRootAndAVersionOfTheAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		files   map[string]string
		argv    []string
		want    string
		refused bool
	}{
		{
			name: "a library module beside it",
			files: map[string]string{"acme/greet.js": lines(`const { word } = require("./lib/words");`,
				`exports.command = { short: "Greet" };`, `exports.run = () => ({ said: word });`),
				"acme/lib/words.js": `exports.word = require("../../shared.js").word;`, "shared.js": `exports.word = "hello";`},
			argv: []string{"acme", "greet"}, want: greeted,
		},
		{name: "a module outside the root", files: map[string]string{"greet.js": lines(`require("../outside.js");`, greeting)},
			argv: []string{"greet"}, refused: true},
		{name: "a version of the API ytrack does not have",
			files: map[string]string{"greet.js": lines(`require("ytrack/v2");`, greeting)}, argv: []string{"greet"}, refused: true},
		{name: "a module by name", files: map[string]string{"greet.js": lines(`require("fs");`, greeting)},
			argv: []string{"greet"}, refused: true},
		{name: "a command script", files: map[string]string{"greet.js": lines(`require("./other.js");`, greeting),
			"other.js": greeting}, argv: []string{"greet"}, refused: true},
		{name: "a module with a syntax error", files: map[string]string{"greet.js": lines(`require("./lib.js");`, greeting),
			"lib.js": `exports.x = ;`}, argv: []string{"greet"}, refused: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), tc.files, tc.argv...)

			if tc.refused {
				assert.Equal(t, "script_failed", requireFault(t, got).code)
				return
			}
			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestScriptReadsTheAddressOfTheLogin(t *testing.T) {
	t.Parallel()
	body := `exports.run = () => ({ address: require("ytrack/v1").address });`
	tests := []struct {
		name string
		env  func(address string) []string
		want string
	}{
		{name: "an address under a path", env: func(address string) []string {
			return []string{"YTRACK_URL=" + address + "/youtrack/", "YTRACK_TOKEN=" + fake.Token}
		}, want: "/youtrack"},
		{name: "an address with a password", env: func(address string) []string {
			return []string{"YTRACK_URL=http://svc:secret@" + address[len("http://"):], "YTRACK_TOKEN=" + fake.Token}
		}, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			env := append(tc.env(server.URL), "HOME="+scriptsHome(t, running(body)))

			got := runWith(t, env, "run")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.NotContains(t, got.stdout, "secret")
			assert.Contains(t, got.stdout, tc.want+"\"\n")
		})
	}
}

func TestScriptWithoutALoginFailsOnlyWhenItReachesTheInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want faultDocument
		out  string
	}{
		{name: "a script that reaches no instance", body: `exports.run = () => ({ done: true });`, out: "done: true\n"},
		{name: "a function", body: `exports.run = () => project.show("DEV", { fields: "id" });`,
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN", "settings")}}},
		{name: "the address", body: `exports.run = () => ({ address: require("ytrack/v1").address });`,
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN", "settings")}}},
		{name: "a limit refused before the login", body: `exports.run = () => project.list({ fields: "id", limit: 0, skip: 0 });`,
			want: faultDocument{code: "bad_usage"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, []string{"HOME=" + scriptsHome(t, running(tc.body))}, "run")

			if tc.out != "" {
				assert.Equal(t, outcome{stdout: tc.out}, got)
				return
			}
			assert.Equal(t, tc.want, requireFault(t, got))
		})
	}
}
