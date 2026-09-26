package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const apiHead = `const { project, fail, warn } = require("ytrack/v1");`

func running(body string) map[string]string {
	return map[string]string{"run.js": lines(apiHead, `exports.command = { short: "Run", long: "Run it." };`, body)}
}

func TestScriptPrintsAnAnswerAsItsCommandDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		answer string
		body   string
		want   string
	}{
		{
			name:   "the answer of a function",
			answer: listedDEV,
			body:   `exports.run = () => project.show("DEV", { fields: "shortName,name" });`,
			want:   "shortName: \"DEV\"\nname: \"DEVELOPMENT\"\n",
		},
		{
			name:   "an answer put inside a value of its own",
			answer: `[` + listedDEV + `]`,
			body:   `exports.run = () => ({ found: project.list({ fields: "shortName,name", limit: 5, skip: 0 }).projects });`,
			want:   "found:\n  - {shortName: \"DEV\", name: \"DEVELOPMENT\"}\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.Serve(t, fake.JSON(http.StatusOK, tc.answer)), running(tc.body), "run")

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestScriptPrintsAValueItBuilt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "values of every kind",
			body: `exports.run = () => ({ text: "one\ntwo", line: "one", "Odd key": 1.5, left: undefined, none: null, ` +
				`big: 2n ** 64n, list: [true, 2] });`,
			want: "text: |-\n  one\n  two\nline: \"one\"\n\"Odd key\": 1.5\nnone: null\nbig: 18446744073709551616\n" +
				"list:\n  - true\n  - 2\n",
		},
		{
			name: "values built after the script replaced Object and Error",
			body: `exports.run = () => { globalThis.Object = undefined; globalThis.Error = undefined; ` +
				`try { fail("denied", "no"); } catch (e) { return { code: e.code, list: [1] }; } };`,
			want: "code: \"denied\"\nlist:\n  - 1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), running(tc.body), "run")

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

func TestScriptFailsWithACodeOfTheDictionary(t *testing.T) {
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

func TestWarningOfAScriptGoesToStderr(t *testing.T) {
	t.Parallel()
	body := `exports.run = () => { warn("unknown_name", "no such tag", { name: "urgent" }); return { done: true }; };`

	got := runScripts(t, fake.ServeNothing(t), running(body), "run")

	want := outcome{stdout: "done: true\n", stderr: "code: \"unknown_name\"\nmessage: \"no such tag\"\nname: \"urgent\"\n"}
	assert.Equal(t, want, got)
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
		{name: "a value thrown with no string form", body: `exports.run = () => { throw Object.create(null); };`, column: 23},
		{name: "a getter that throws in the value returned",
			body: `exports.run = () => ({ get x() { throw new Error("broken"); } });`, column: 40},
		{name: "an argument of the wrong type", body: `exports.run = () => project.show(1, { fields: "id" });`, column: 33},
		{name: "a flag left out", body: `exports.run = () => project.show("DEV");`, column: 33},
		{name: "a flag it does not take", body: `exports.run = () => project.show("DEV", { fields: "id", limit: 1 });`, column: 33},
		{name: "a flag of the wrong type", body: `exports.run = () => project.list({ fields: "id", limit: "1", skip: 0 });`, column: 33},
		{name: "fields that add to the default", body: `exports.run = () => project.show("DEV", { fields: "+id" });`, column: 33},
		{name: "fields that name none", body: `exports.run = () => project.show("DEV", { fields: "" });`, column: 33},
		{name: "a code out of the dictionary", body: `exports.run = () => fail("broken", "no");`, column: 25},
		{name: "a fail posing as a defect of a builtin script",
			body: `exports.run = () => fail("script_failed", "no", { file: "builtin:project/show.js", line: 1, column: 1 });`, column: 25},
		{name: "a warning posing as a defect of a script", body: `exports.run = () => warn("script_failed", "no");`, column: 25},
		{name: "a warning with a code out of the dictionary", body: `exports.run = () => warn("broken", "no");`, column: 25},
		{name: "details that hold a code", body: `exports.run = () => fail("denied", "no", { code: "other" });`, column: 25},
		{name: "details that hold a message", body: `exports.run = () => warn("denied", "no", { message: "other" });`, column: 25},
		{name: "a declaration the module wrapper holds", body: `const module = 1; exports.run = () => ({});`, column: 7},
		{name: "a value written into an answer",
			body: `exports.run = () => { project.list({ fields: "id", limit: 1, skip: 0 }).total = 5; };`, column: 73},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := scriptsHome(t, running(tc.body))

			got := runWith(t, atHome(fake.Serve(t, fake.JSON(http.StatusOK, `[]`)), home), "run")

			assert.Equal(t, scriptFailedIn(home, "run.js", detail{"line", 3}, detail{"column", tc.column}), requireFault(t, got))
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
		{name: "a list longer than its items", body: `exports.run = () => { const a = []; a.length = 4294967295; return { a }; };`},
		{name: "a value that holds itself", body: `exports.run = () => { const a = {}; a.a = a; return a; };`},
		{name: "a promise", body: `exports.run = async () => ({});`},
		{name: "a proxy that breaks its own rules", body: `exports.run = () => new Proxy({}, { ownKeys() { return [1]; } });`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := scriptsHome(t, running(tc.body))

			got := runWith(t, atHome(fake.ServeNothing(t), home), "run")

			assert.Equal(t, scriptFailedIn(home, "run.js"), requireFault(t, got))
		})
	}
}

func TestScriptRequiresALibraryModuleOfItsRoot(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"acme/greet.js": lines(`const { word } = require("./lib/words");`,
			`exports.command = { short: "Greet", long: "Say hello." };`, `exports.run = () => ({ said: word });`),
		"acme/lib/words.js": `exports.word = require("../../shared.js").word;`,
		"shared.js":         `exports.word = "hello";`,
	}

	assert.Equal(t, outcome{stdout: greeted}, runScripts(t, fake.ServeNothing(t), files, "acme", "greet"))
}

func TestScriptRequiresNothingButAModuleOfItsRootAndAVersionOfTheAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
	}{
		{name: "a module outside the root", files: map[string]string{"greet.js": lines(`require("../outside.js");`, greeting)}},
		{name: "a version of the API ytrack does not have",
			files: map[string]string{"greet.js": lines(`require("ytrack/v2");`, greeting)}},
		{name: "a module by name", files: map[string]string{"greet.js": lines(`require("fs");`, greeting)}},
		{name: "a command script", files: map[string]string{"greet.js": lines(`require("./other.js");`, greeting),
			"other.js": greeting}},
		{name: "a module with an unreadable declaration", files: map[string]string{
			"greet.js": lines(`require("./other.js");`, greeting), "other.js": `exports.command = { short: s };`}},
		{name: "a module with a syntax error", files: map[string]string{"greet.js": lines(`require("./lib.js");`, greeting),
			"lib.js": `exports.x = ;`}},
		{name: "a module that failed to load, a second time", files: map[string]string{
			"greet.js": lines(`try { require("./half.js"); } catch (e) {}`, `require("./half.js");`, greeting),
			"half.js":  `exports.a = 1; throw new Error("half");`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), tc.files, "greet")

			assert.Equal(t, "script_failed", requireFault(t, got).code)
		})
	}
}

func TestScriptReadsTheAddressOfTheLogin(t *testing.T) {
	t.Parallel()
	body := `exports.run = () => ({ address: require("ytrack/v1").address });`
	tests := []struct {
		name         string
		userinfo     string
		path         string
		seenUserinfo string
		seenPath     string
	}{
		{name: "an address under a path", path: "/youtrack/", seenPath: "/youtrack"},
		{name: "an address with a password", userinfo: "svc:secret@", seenUserinfo: "svc:xxxxx@"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			host := strings.TrimPrefix(server.URL, "http://")
			env := []string{"YTRACK_URL=http://" + tc.userinfo + host + tc.path, "YTRACK_TOKEN=" + fake.Token,
				"HOME=" + scriptsHome(t, running(body))}

			got := runWith(t, env, "run")

			assert.Equal(t, outcome{stdout: "address: \"http://" + tc.seenUserinfo + host + tc.seenPath + "\"\n"}, got)
		})
	}
}

func TestScriptThatReachesNoInstanceNeedsNoLogin(t *testing.T) {
	t.Parallel()
	got := runWith(t, []string{"HOME=" + scriptsHome(t, running(`exports.run = () => ({ done: true });`))}, "run")

	assert.Equal(t, outcome{stdout: "done: true\n"}, got)
}

func TestScriptWithoutALoginFailsWhenItReachesTheInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want faultDocument
	}{
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

			assert.Equal(t, tc.want, requireFault(t, got))
		})
	}
}
