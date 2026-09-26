package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const apiHead = `const { projects, issues, comments, tags, workItems, users, customFields, fail, warn } = require("ytrack/v1");`

func running(body string) map[string]string {
	return map[string]string{"run.js": lines(apiHead, `exports.definition = { short: "Run", long: "Run it." };`, body)}
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
			body:   `exports.command = () => projects.show({ project: "DEV", fields: "shortName,name" });`,
			want:   "shortName: \"DEV\"\nname: \"DEVELOPMENT\"\n",
		},
		{
			name:   "an answer put inside a value of its own",
			answer: `[` + listedDEV + `]`,
			body:   `exports.command = () => ({ found: projects.list({ fields: "shortName,name", limit: 5, skip: 0 }).projects });`,
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
			body: `exports.command = () => ({ text: "one\ntwo", line: "one", "Odd key": 1.5, left: undefined, none: null, ` +
				`big: 2n ** 64n, list: [true, 2] });`,
			want: "text: |-\n  one\n  two\nline: \"one\"\n\"Odd key\": 1.5\nnone: null\nbig: 18446744073709551616\n" +
				"list:\n  - true\n  - 2\n",
		},
		{
			name: "values built after the script replaced Object and Error",
			body: `exports.command = () => { globalThis.Object = undefined; globalThis.Error = undefined; ` +
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
		`exports.command = () => {`,
		`  const read = projects.show({ project: "DEV", fields: "shortName,name,startingNumber" });`,
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
		`exports.command = () => {`,
		`  try { projects.show({ project: "DEV", fields: "shortName" }); } catch (e) {`,
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
	body := `exports.command = () => projects.show({ project: "DEV", fields: "shortName" });`

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
		{name: "fail", body: `exports.command = () => fail("not_found", "no such issue", { id: "DEV-1" });`,
			want: faultDocument{code: "not_found", details: []detail{{"id", "DEV-1"}}}, exit: 1},
		{name: "fail with a code that may have written", body: `exports.command = () => fail("write_uncertain", "unknown");`,
			want: faultDocument{code: "write_uncertain"}, exit: 2},
		{name: "fail caught and thrown again",
			body: `exports.command = () => { try { fail("denied", "no"); } catch (e) { throw e; } };`,
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

func TestFaultAfterAWriteOfAScriptExitsTwo(t *testing.T) {
	t.Parallel()
	const written = `comments.create({ owner: "DEV-7", text: "Text", fields: "id,text" })`
	tests := []struct {
		name string
		body string
		want faultDocument
		exit int
	}{
		{name: "a fault after a write", body: `exports.command = () => { ` + written + `; fail("rejected", "no"); };`,
			want: faultDocument{code: "rejected"}, exit: 2},
		{name: "a defect after a write", body: `exports.command = () => { ` + written + `; missing(); };`,
			want: faultDocument{code: "script_failed"}, exit: 2},
		{name: "a fault after a read", body: `exports.command = () => { projects.show({ project: "DEV", fields: "shortName" }); ` +
			`fail("rejected", "no"); };`,
			want: faultDocument{code: "rejected"}, exit: 1},
		{name: "a fault after a write refused before the network",
			body: `exports.command = () => { try { workItems.create({ issue: "DEV-7", minutes: -1, fields: "id" }); } catch (e) {} ` +
				`fail("rejected", "no"); };`,
			want: faultDocument{code: "rejected"}, exit: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					fake.JSON(http.StatusOK, createdComment("7-12", "Text"))(w, r)
					return
				}
				fake.JSON(http.StatusOK, listedDEV)(w, r)
			})

			got := runScripts(t, server, running(tc.body), "run")

			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.want.code, requireFaultDocument(t, got).code)
		})
	}
}

func TestWarningOfAScriptGoesToStderr(t *testing.T) {
	t.Parallel()
	body := `exports.command = () => { warn("unknown_name", "no such tag", { name: "urgent" }); return { done: true }; };`

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
		{name: "a name defined nowhere", body: `exports.command = () => missing();`, column: 32},
		{name: "an error thrown", body: `exports.command = () => { throw new Error("broken"); };`, column: 33},
		{name: "a value thrown", body: `exports.command = () => { throw "broken"; };`, column: 27},
		{name: "a value thrown with no string form", body: `exports.command = () => { throw Object.create(null); };`, column: 27},
		{name: "a getter that throws in the value returned",
			body: `exports.command = () => ({ get x() { throw new Error("broken"); } });`, column: 44},
		{name: "a key of the wrong type", body: `exports.command = () => projects.show({ project: 1, fields: "id" });`, column: 38},
		{name: "a required key left out", body: `exports.command = () => projects.show({ project: "DEV" });`, column: 38},
		{name: "a key it does not take", body: `exports.command = () => projects.show({ project: "DEV", fields: "id", limit: 1 });`, column: 38},
		{name: "a number of the wrong type", body: `exports.command = () => projects.list({ fields: "id", limit: "1", skip: 0 });`, column: 38},
		{name: "a list that is no array",
			body: `exports.command = () => tags.create({ name: "x", visibleFor: "g", fields: "name" });`, column: 36},
		{name: "a list that holds no string",
			body: `exports.command = () => tags.create({ name: "x", visibleFor: [1], fields: "name" });`, column: 36},
		{name: "flags given in place of an object", body: `exports.command = () => projects.show("DEV");`, column: 38},
		{name: "custom fields of the wrong shape",
			body: `exports.command = () => issues.update({ id: "DEV-1", customFields: { Type: 1 }, fields: "id" });`, column: 38},
		{name: "comments that are neither all nor a count",
			body: `exports.command = () => issues.show({ id: "DEV-1", fields: "id", comments: "5" });`, column: 36},
		{name: "fields that add to the default", body: `exports.command = () => projects.show({ project: "DEV", fields: "+id" });`, column: 38},
		{name: "fields that name none", body: `exports.command = () => projects.show({ project: "DEV", fields: "" });`, column: 38},
		{name: "a code out of the dictionary", body: `exports.command = () => fail("broken", "no");`, column: 29},
		{name: "a fail posing as a defect of a builtin script",
			body: `exports.command = () => fail("script_failed", "no", { file: "builtin:project/show.js", line: 1, column: 1 });`, column: 29},
		{name: "a warning posing as a defect of a script", body: `exports.command = () => warn("script_failed", "no");`, column: 29},
		{name: "a warning with a code out of the dictionary", body: `exports.command = () => warn("broken", "no");`, column: 29},
		{name: "details that hold a code", body: `exports.command = () => fail("denied", "no", { code: "other" });`, column: 29},
		{name: "details that hold a message", body: `exports.command = () => warn("denied", "no", { message: "other" });`, column: 29},
		{name: "a declaration the module wrapper holds", body: `const module = 1; exports.command = () => ({});`, column: 7},
		{name: "a value written into an answer",
			body: `exports.command = () => { projects.list({ fields: "id", limit: 1, skip: 0 }).total = 5; };`, column: 78},
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
		{name: "nothing", body: `exports.command = () => {};`},
		{name: "a string", body: `exports.command = () => "done";`},
		{name: "a list", body: `exports.command = () => [];`},
		{name: "a function", body: `exports.command = () => ({ f: () => 1 });`},
		{name: "NaN", body: `exports.command = () => ({ n: 0 / 0 });`},
		{name: "a Date", body: `exports.command = () => ({ at: new Date(0) });`},
		{name: "bytes", body: `exports.command = () => ({ data: new Uint8Array(2) });`},
		{name: "a symbol", body: `exports.command = () => ({ s: Symbol("s") });`},
		{name: "undefined in a list", body: `exports.command = () => ({ list: [undefined] });`},
		{name: "a list longer than its items", body: `exports.command = () => { const a = []; a.length = 4294967295; return { a }; };`},
		{name: "a value that holds itself", body: `exports.command = () => { const a = {}; a.a = a; return a; };`},
		{name: "a promise", body: `exports.command = async () => ({});`},
		{name: "a proxy that breaks its own rules", body: `exports.command = () => new Proxy({}, { ownKeys() { return [1]; } });`},
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
			`exports.definition = { short: "Greet", long: "Say hello." };`, `exports.command = () => ({ said: word });`),
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
		{name: "a module by name", files: map[string]string{"greet.js": lines(`require("child_process");`, greeting)}},
		{name: "a command script", files: map[string]string{"greet.js": lines(`require("./other.js");`, greeting),
			"other.js": greeting}},
		{name: "a module with an unreadable declaration", files: map[string]string{
			"greet.js": lines(`require("./other.js");`, greeting), "other.js": `exports.definition = { short: s };`}},
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
	body := `exports.command = () => ({ address: require("ytrack/v1").address });`
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
	got := runWith(t, []string{"HOME=" + scriptsHome(t, running(`exports.command = () => ({ done: true });`))}, "run")

	assert.Equal(t, outcome{stdout: "done: true\n"}, got)
}

func TestScriptWithoutALoginFailsWhenItReachesTheInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want faultDocument
	}{
		{name: "a function", body: `exports.command = () => projects.show({ project: "DEV", fields: "id" });`,
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN", "settings")}}},
		{name: "the address", body: `exports.command = () => ({ address: require("ytrack/v1").address });`,
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN", "settings")}}},
		{name: "a limit refused before the login", body: `exports.command = () => projects.list({ fields: "id", limit: 0, skip: 0 });`,
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

func TestScriptReadsTheModelOfTheSDK(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		answer string
		body   string
		want   string
	}{
		{
			name:   "the user of the token",
			answer: `{"$type":"User","id":"1-1","login":"first","fullName":"First","email":null,"banned":false}`,
			body:   `exports.command = () => ({ me: users.me() });`,
			want:   "me:\n  id: \"1-1\"\n  login: \"first\"\n  fullName: \"First\"\n  email: \"\"\n  banned: false\n",
		},
		{
			name:   "the users a search finds",
			answer: `[{"$type":"User","id":"1-1","login":"first","fullName":"First","email":"f@example.com","banned":false}]`,
			body:   `exports.command = () => ({ logins: users.find({ query: "fir", limit: 5 }).map((user) => user.login) });`,
			want:   "logins:\n  - \"first\"\n",
		},
		{
			name: "an issue with its custom fields",
			answer: `{"$type":"Issue","id":"2-1","idReadable":"DEV-1","summary":"Title","description":null,` +
				`"project":{"$type":"Project","id":"0-1","shortName":"DEV","name":"Development"},"links":[],` +
				`"customFields":` + receivedFields(receivedField{name: "Type", valueType: "enum", value: `{"$type":"EnumBundleElement","id":"67-1","name":"Bug","localizedName":null}`}) + `}`,
			body: `exports.command = () => { const issue = issues.get({ id: "DEV-1" }); ` +
				`return { id: issue.idReadable, project: issue.project.shortName, ` +
				`fields: issue.fields.map((field) => field.name + "=" + field.values.map((value) => value.text).join()) }; };`,
			want: "id: \"DEV-1\"\nproject: \"DEV\"\nfields:\n  - \"Type=Bug\"\n",
		},
		{
			name:   "the metadata of a project",
			answer: projectMetadata(projectField("180-1", "Type", "Kind")),
			body: `exports.command = () => { const { fields } = customFields.metadata({ project: "DEV" }); ` +
				`return { names: fields.map((field) => field.name + ":" + field.type.valueType) }; };`,
			want: "names:\n  - \"Type:enum\"\n",
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

func TestScriptAttachesBytesItHolds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
	}{
		{name: "a buffer", content: `Buffer.from("ytrack")`},
		{name: "a string", content: `"ytrack"`},
		{name: "an array buffer", content: `new Uint8Array([121, 116, 114, 97, 99, 107]).buffer`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, filed("note.txt", 6)))
			body := `exports.command = () => require("ytrack/v1").attachments.create({ owner: "DEV-1", name: "note.txt", ` +
				`content: ` + tc.content + `, fields: "id,name" });`

			got := runScripts(t, server, running(body), "run")

			assert.Equal(t, outcome{stdout: "id: \"12-9\"\nname: \"note.txt\"\n"}, got)
			sent := requireSentFile(t, server)
			assert.Equal(t, "note.txt", sent.file)
			assert.Equal(t, "ytrack", sent.content)
		})
	}
}

func TestScriptAttachesEitherAFileOrBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call string
	}{
		{name: "both a path and content", call: `{ owner: "DEV-1", path: "a.txt", name: "a.txt", content: "x", fields: "id" }`},
		{name: "content without a name", call: `{ owner: "DEV-1", content: "x", fields: "id" }`},
		{name: "a name with a path", call: `{ owner: "DEV-1", path: "a.txt", name: "b.txt", fields: "id" }`},
		{name: "neither a path nor content", call: `{ owner: "DEV-1", name: "a.txt", fields: "id" }`},
		{name: "content of no kind of bytes", call: `{ owner: "DEV-1", name: "a.txt", content: 1, fields: "id" }`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `exports.command = () => require("ytrack/v1").attachments.create(` + tc.call + `);`

			got := runScripts(t, fake.ServeNothing(t), running(body), "run")

			assert.Equal(t, "script_failed", requireFault(t, got).code)
		})
	}
}
