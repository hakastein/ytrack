package cli_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func onPath(body string) map[string]string {
	return map[string]string{"run.js": lines(
		`const fs = require("fs");`,
		`exports.definition = { short: "Run", long: "Run it.", args: [{ name: "at", type: "path", usage: "path" }] };`,
		body,
	)}
}

func TestScriptReadsAFileAsNodeDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "bytes when no encoding is named",
			body: `exports.command = (at) => { const read = fs.readFileSync(at); ` +
				`return { buffer: Buffer.isBuffer(read), length: read.length, first: read[0] }; };`,
			want: "buffer: true\nlength: 6\nfirst: 121\n",
		},
		{
			name: "text in the encoding named",
			body: `exports.command = (at) => ({ text: fs.readFileSync(at, "utf8") });`,
			want: "text: \"ytrack\"\n",
		},
		{
			name: "text in the encoding named by options",
			body: `exports.command = (at) => ({ text: fs.readFileSync(at, { encoding: "base64" }) });`,
			want: "text: \"eXRyYWNr\"\n",
		},
		{
			name: "bytes through the node name of the module",
			body: `exports.command = (at) => ({ text: require("node:fs").readFileSync(at).toString() });`,
			want: "text: \"ytrack\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), onPath(tc.body), "run", aFileToAttach(t))

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestScriptWritesAFileAsNodeDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "a string", body: `fs.writeFileSync(at, "written")`, want: "written"},
		{name: "a string in the encoding named", body: `fs.writeFileSync(at, "eXRyYWNr", "base64")`, want: "ytrack"},
		{name: "a string in the encoding named by options", body: `fs.writeFileSync(at, "7974", { encoding: "hex" })`,
			want: "yt"},
		{name: "a buffer", body: `fs.writeFileSync(at, Buffer.from([121, 116]))`, want: "yt"},
		{name: "a buffer of the buffer module", body: `fs.writeFileSync(at, require("node:buffer").Buffer.from("yt"))`,
			want: "yt"},
		{name: "a byte array", body: `fs.writeFileSync(at, new Uint8Array([121, 116]))`, want: "yt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "written.txt")

			got := runScripts(t, fake.ServeNothing(t), onPath(`exports.command = (at) => { `+tc.body+`; return { done: true }; };`), "run", path)

			assert.Equal(t, outcome{stdout: "done: true\n"}, got)
			written, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(written))
		})
	}
}

func TestScriptWritesOverAFileThatHoldsMore(t *testing.T) {
	t.Parallel()
	path := fileWith(t, "older.txt", []byte("older text"))

	got := runScripts(t, fake.ServeNothing(t), onPath(`exports.command = (at) => { fs.writeFileSync(at, "new"); return { done: true }; };`), "run", path)

	assert.Equal(t, outcome{stdout: "done: true\n"}, got)
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(written))
}

func TestScriptMakesADirectoryAsNodeDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "one level",
			body: `exports.command = (at) => ({ made: fs.mkdirSync(at + "/one") === undefined, ` +
				`there: fs.statSync(at + "/one").isDirectory() });`,
			want: "made: true\nthere: true\n",
		},
		{
			name: "every missing level, naming the first one made",
			body: `exports.command = (at) => ({ first: fs.mkdirSync(at + "/one/two", { recursive: true }) === at + "/one", ` +
				`there: fs.existsSync(at + "/one/two") });`,
			want: "first: true\nthere: true\n",
		},
		{
			name: "none, when every level is there",
			body: `exports.command = (at) => ({ made: fs.mkdirSync(at, { recursive: true }) === undefined });`,
			want: "made: true\n",
		},
		{
			name: "a directory that is there already",
			body: `exports.command = (at) => { try { fs.mkdirSync(at); } catch (e) { return { code: e.code }; } };`,
			want: "code: \"EEXIST\"\n",
		},
		{
			name: "every missing level over a file in the way",
			body: `exports.command = (at) => { fs.writeFileSync(at + "/file", "x"); ` +
				`try { fs.mkdirSync(at + "/file", { recursive: true }); } catch (e) { return { code: e.code }; } };`,
			want: "code: \"EEXIST\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runScripts(t, fake.ServeNothing(t), onPath(tc.body), "run", t.TempDir())

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestScriptLooksAtADirectoryAsNodeDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "the names in it, sorted",
			body: `exports.command = (at) => ({ names: fs.readdirSync(at) });`,
			want: "names:\n  - \"a.txt\"\n  - \"b\"\n",
		},
		{
			name: "the names in it, in the encoding named",
			body: `exports.command = (at) => ({ names: fs.readdirSync(at, "utf8") });`,
			want: "names:\n  - \"a.txt\"\n  - \"b\"\n",
		},
		{
			name: "whether a path is there",
			body: `exports.command = (at) => ({ file: fs.existsSync(at + "/a.txt"), none: fs.existsSync(at + "/none"), ` +
				`notAPath: fs.existsSync(1) });`,
			want: "file: true\nnone: false\nnotAPath: false\n",
		},
		{
			name: "the stats of a file",
			body: `exports.command = (at) => { const stats = fs.statSync(at + "/a.txt"); ` +
				`return { size: stats.size, file: stats.isFile(), directory: stats.isDirectory(), ` +
				`link: stats.isSymbolicLink(), changed: stats.mtime.getTime() === Math.floor(stats.mtimeMs) }; };`,
			want: "size: 6\nfile: true\ndirectory: false\nlink: false\nchanged: true\n",
		},
		{
			name: "no stats of a path that is not there, when asked not to throw",
			body: `exports.command = (at) => ({ stats: typeof fs.statSync(at + "/none", { throwIfNoEntry: false }) });`,
			want: "stats: \"undefined\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("ytrack"), 0o600))
			require.NoError(t, os.Mkdir(filepath.Join(dir, "b"), 0o755))

			got := runScripts(t, fake.ServeNothing(t), onPath(tc.body), "run", dir)

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestScriptCatchesAFailureOfFsAsNodeThrowsIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		call    string
		at      func(t *testing.T) string
		code    string
		syscall string
		under   string
	}{
		{name: "reading a file that is not there", call: `fs.readFileSync(at)`, at: nothingAt, code: "ENOENT", syscall: "open"},
		{name: "reading a directory", call: `fs.readFileSync(at)`, at: func(t *testing.T) string { return t.TempDir() },
			code: "EISDIR", syscall: "read"},
		{name: "writing into a directory", call: `fs.writeFileSync(at, "x")`,
			at: func(t *testing.T) string { return t.TempDir() }, code: "EISDIR", syscall: "open"},
		{name: "writing under a directory that is not there", call: `fs.writeFileSync(at + "/x.txt", "x")`, at: nothingAt,
			code: "ENOENT", syscall: "open", under: "/x.txt"},
		{name: "listing a file", call: `fs.readdirSync(at)`, at: aFileToAttach, code: "ENOTDIR", syscall: "scandir"},
		{name: "the stats of a path that is not there", call: `fs.statSync(at)`, at: nothingAt, code: "ENOENT", syscall: "stat"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			at := tc.at(t)
			body := `exports.command = (at) => { try { ` + tc.call + `; } catch (e) { ` +
				`return { error: e instanceof Error, code: e.code, syscall: e.syscall, path: e.path }; } };`

			got := runScripts(t, fake.ServeNothing(t), onPath(body), "run", at)

			want := "error: true\ncode: " + strconv.Quote(tc.code) + "\nsyscall: " + strconv.Quote(tc.syscall) + "\npath: "
			assert.Equal(t, outcome{stdout: want + strconv.Quote(at+tc.under) + "\n"}, got)
		})
	}
}

func nothingAt(t *testing.T) string {
	return filepath.Join(t.TempDir(), "nothing")
}

func TestUncaughtFailureOfFsIsBadUsageAboutThePath(t *testing.T) {
	t.Parallel()
	at := nothingAt(t)

	got := runScripts(t, fake.ServeNothing(t), onPath(`exports.command = (at) => fs.readFileSync(at);`), "run", at)

	assert.Equal(t, faultDocument{code: "bad_usage", details: []detail{{"path", at}}}, requireFault(t, got))
}

func TestScriptCallingFsWrongIsScriptFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call string
	}{
		{name: "a path that is no string", call: `fs.readFileSync(1)`},
		{name: "data of no kind of bytes", call: `fs.writeFileSync(at, {})`},
		{name: "an encoding node does not have", call: `fs.readFileSync(at, "klingon")`},
		{name: "an option ytrack does not take", call: `fs.readFileSync(at, { flag: "r" })`},
		{name: "options of mkdir ytrack does not take", call: `fs.mkdirSync(at, { mode: 0o700 })`},
		{name: "options of readdir", call: `fs.readdirSync(at, { withFileTypes: true })`},
		{name: "an array buffer, which node does not write", call: `fs.writeFileSync(at, new ArrayBuffer(2))`},
		{name: "names in an encoding other than utf8", call: `fs.readdirSync(at, "hex")`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := scriptsHome(t, onPath(`exports.command = (at) => { `+tc.call+`; return {}; };`))

			got := runWith(t, atHome(fake.ServeNothing(t), home), "run", aFileToAttach(t))

			assert.Equal(t, "script_failed", requireFault(t, got).code)
		})
	}
}

func TestFailureOfFsHoldsItsKeysInTheOrderOfNode(t *testing.T) {
	t.Parallel()
	at := nothingAt(t)
	body := `exports.command = (at) => { try { fs.statSync(at); } catch (e) { return { ...e }; } };`

	got := runScripts(t, fake.ServeNothing(t), onPath(body), "run", at)

	assert.Equal(t, outcome{stdout: "code: \"ENOENT\"\nsyscall: \"stat\"\npath: " + strconv.Quote(at) + "\n"}, got)
}
