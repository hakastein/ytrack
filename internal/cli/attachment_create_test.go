package cli_test

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The one part an upload is written as, under the name the specification declares for it.
const uploadedField = "files[0]"

// The request an upload of an issue goes out as, which is the one a refusal about it names.
func attachmentWriteRequest(address, owner, fields string) string {
	return "POST " + address + "/api/issues/" + owner + "/attachments?fields=" + fields
}

// filed is what a server answers an upload with: the array of what the write filed, holding the one
// attachment, with the name and the size of it as the scenario says the server kept them.
func filed(name string, size int) string {
	return filedAs(strconv.Quote(name), strconv.Itoa(size))
}

// filedAs is the same answer for a scenario about the shape of what came back rather than its value: the name
// and the size stand in the body as the JSON written here, whatever kind of value that is.
func filedAs(name, size string) string {
	return `[{"$type":"IssueAttachment","id":"12-9","name":` + name +
		`,"size":` + size + `,"mimeType":"application/octet-stream",` +
		`"url":"/api/files/12-9?sign=s&updated=1"}]`
}

// fileHolding writes a file of that name into a directory of the test's own and gives back its path. The
// bytes are the scenario's, so a name and a content that would each go wrong on their own are told apart.
func fileHolding(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, content, 0o600))
	return path
}

// aFileToAttach is a file whose name and content matter to nothing: a scenario about the owner, the flags or
// the answer of the server still needs one to send.
func aFileToAttach(t *testing.T) string {
	t.Helper()
	return fileHolding(t, "attached.txt", []byte("ytrack"))
}

// requireOnePart holds the body of the upload to being the one part it is written as and gives that part back.
func requireOnePart(t *testing.T, server *upstream) formPart {
	t.Helper()
	parts := sentParts(t, server, 0)
	require.Len(t, parts, 1, "an upload is one part and nothing else")
	assert.Equal(t, uploadedField, parts[0].field)
	return parts[0]
}

// What a creation is given is two arguments, and the refusal names which of the two is missing before any
// file is opened and any request is built.
func TestAttachmentCreateRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: nil},
		{name: "an owner alone", argv: []string{"DEV-1"}},
		{name: "a third argument", argv: []string{"DEV-1", "a.txt", "b.txt"}},
		// pflag reads a leading dash as a flag wherever the word stands, so such a path is written ./-x.txt.
		{name: "a path beginning with a dash", argv: []string{"DEV-1", "-x.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"attachment", "create"}, tc.argv...)...)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// An attachment is the bytes of one regular file, so what the path stands for is settled before anything
// is opened and before anything is sent. The refusal names the path as the caller wrote it.
func TestAttachmentCreateRefusesAPathThatIsNoRegularFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "a directory",
			path: func(t *testing.T) string { return t.TempDir() },
		},
		{
			name: "a file nobody wrote",
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "nothing.txt") },
		},
		{
			name: "a file the caller may not read",
			path: func(t *testing.T) string {
				if os.Geteuid() == 0 {
					t.Skip("root reads a file of any mode")
				}
				path := fileHolding(t, "locked.txt", []byte("x"))
				require.NoError(t, os.Chmod(path, 0o000))
				return path
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			path := tc.path(t)

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// The help says a lone dash is a file of that name and that nothing is ever read from standard input, and
// both halves are the same argument: the word reaches the command as the path it was written as, and the
// stream handed to Run holds a file's worth of bytes it is never asked for.
func TestAttachmentCreateReadsALoneDashAsAFileOfThatName(t *testing.T) {
	t.Parallel()

	t.Run("no file of that name here", func(t *testing.T) {
		t.Parallel()
		server := serveNothing(t)
		stdin, err := os.Open(fileHolding(t, "standard-input.bin", []byte("the bytes of standard input")))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, stdin.Close()) })

		got := runOn(t, stdin, server.env(), "attachment", "create", "DEV-1", "-")

		assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
		assert.Empty(t, server.requests())
		read, err := stdin.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		assert.Zero(t, read, "a byte of standard input was read")
	})

	t.Run("a file of that name goes out under it", func(t *testing.T) {
		t.Parallel()
		server := serve(t, answer(http.StatusOK, filed("-", 1)))
		path := fileHolding(t, "-", []byte("x"))

		got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		sent := requireOnePart(t, server)
		assert.Equal(t, "-", sent.file)
		assert.Equal(t, "x", sent.content)
		assert.Equal(t, "-", requireAttachmentPrinted(t, got.stdout).Name)
	})
}

// Every name here YouTrack would keep as another, and it would do so after the file is stored: the write
// would have happened and the name could not be taken back. So each is refused before anything is sent, and
// the refusal names the rule, quotes the name, and says what to do instead.
func TestAttachmentCreateRefusesANameTheServerWouldRewrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
	}{
		{name: "a double quote", file: `a"b.txt`},
		{name: "a backslash", file: `a\b.txt`},
		{name: "a line feed", file: "nl\n.txt"},
		{name: "a carriage return", file: "cr\r.txt"},
		{name: "a leading space", file: " lead.txt"},
		{name: "a trailing space", file: "trail.txt "},
		{name: "a leading tab", file: "\tlead.txt"},
		{name: "a trailing vertical tab", file: "trail.txt\x0b"},
		{name: "bytes that are no UTF-8", file: "\xff.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			path := fileHolding(t, tc.file, []byte("x"))

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// The help says what a caller gets unasked, so the default is read off it rather than off this repository.
func TestAttachmentCreateHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"attachment", "create", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, attachmentFields)
}

// What goes on the wire for one upload: one part under the name the specification declares, the name of
// the file byte for byte in the header, and the bytes of the file as they stand on disk. The form is written
// as it is sent, so its length is unknown and the request is chunked — a body gathered into a buffer first
// would carry a Content-Length and would have to fit in memory.
func TestAttachmentCreateSendsTheFileAsOneStreamedPart(t *testing.T) {
	t.Parallel()
	const name = "[bug] заметка; 100%.bin"
	content := repeatedBytes(300 * 1024)
	server := serve(t, answer(http.StatusOK, filed(name, len(content))))
	path := fileHolding(t, name, content)

	got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

	want := `id: "12-9"` + "\n" + `name: "` + name + `"` + "\nsize: " + strconv.Itoa(len(content)) + "\n" +
		`mimeType: "application/octet-stream"` + "\n" +
		`url: "` + server.url + `/api/files/12-9?sign=s&updated=1"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)

	sent := requireOnePart(t, server)
	assert.Equal(t, name, sent.file)
	assert.Equal(t, `form-data; name="`+uploadedField+`"; filename="`+name+`"`, sent.disposition)
	assert.Equal(t, string(content), sent.content)

	asked := server.requests()
	require.Len(t, asked, 1)
	assert.Equal(t, http.MethodPost, asked[0].Method)
	assert.Equal(t, "/api/issues/DEV-1/attachments", asked[0].URL.Path)
	assert.Equal(t, []string{"chunked"}, asked[0].TransferEncoding)
	assert.EqualValues(t, -1, asked[0].ContentLength)
	assert.Equal(t, []url.Values{{"fields": {attachmentFields}}}, server.sentQueries())
	assert.True(t, strings.HasPrefix(asked[0].Header.Get("Content-Type"), multipartForm+"; boundary="),
		"the content type names no boundary: %q", asked[0].Header.Get("Content-Type"))
}

// Everything the rules of the name leave alone goes out byte for byte, in the header of the part as well
// as in the document: a space, a semicolon, a tab inside the name, a line separator, a non-breaking space,
// three dots, a name of 254 bytes and a name with no extension at all.
func TestAttachmentCreateSendsEveryOtherNameAsItStands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
	}{
		{name: "a space", file: "заметка с пробелом.txt"},
		{name: "a semicolon", file: "a;b.txt"},
		{name: "a tab inside", file: "tab\tinside.txt"},
		{name: "a line separator", file: "ls\xe2\x80\xa8.txt"},
		{name: "a non-breaking space first", file: "\xc2\xa0nbsp.txt"},
		{name: "three dots", file: "..."},
		{name: "254 bytes of Cyrillic", file: strings.Repeat("я", 127)},
		{name: "no extension", file: "README"},
		// A path whose name begins with a dash reaches the argument through ./, and the name itself
		// keeps the dash.
		{name: "a leading dash", file: "-x.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(http.StatusOK, filed(tc.file, 1)))
			path := fileHolding(t, tc.file, []byte("x"))

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			sent := requireOnePart(t, server)
			assert.Equal(t, tc.file, sent.file)
			assert.Equal(t, `form-data; name="`+uploadedField+`"; filename="`+tc.file+`"`, sent.disposition)
			// The document escapes what a double-quoted string may not carry raw, so the name is read back
			// rather than looked for in the text of stdout.
			assert.Equal(t, tc.file, requireAttachmentPrinted(t, got.stdout).Name)
		})
	}
}

// A relative path is resolved by the kernel against the directory the call was made from, so it reaches
// the very file the absolute one does and the request is the same request.
func TestAttachmentCreateReadsARelativePathAgainstTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	const name = "relative.bin"
	content := repeatedBytes(1024)
	absolute := fileHolding(t, name, content)
	here, err := os.Getwd()
	require.NoError(t, err)
	relative, err := filepath.Rel(here, absolute)
	require.NoError(t, err)
	require.False(t, filepath.IsAbs(relative))

	for _, path := range []string{absolute, relative} {
		t.Run(map[bool]string{true: "absolute", false: "relative"}[filepath.IsAbs(path)], func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(http.StatusOK, filed(name, len(content))))

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			sent := requireOnePart(t, server)
			assert.Equal(t, name, sent.file)
			assert.Equal(t, string(content), sent.content)
		})
	}
}

// The name and the size are the tool's own to ask for: they are what the answer is held against, so they
// go out whatever the caller wrote, and the document still holds only what the caller asked to print.
func TestAttachmentCreateAsksForTheNameAndTheSizeItChecks(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, filed("one.txt", 1)))
	path := fileHolding(t, "one.txt", []byte("x"))

	got := runWith(t, server.env(), "attachment", "create", "DEV-1", path, "--fields", "id")

	assert.Equal(t, outcome{stdout: `id: "12-9"` + "\n"}, got)
	assert.Equal(t, []string{"id,name,size"}, server.sentFields())
}

// A 200 says the server took the file, not that it kept the file that was sent. Every answer here leaves
// an attachment behind, so none of them is a document and the exit code says the instance changed.
func TestAttachmentCreateRefusesAnAnswerThatIsNotTheFileThatWentOut(t *testing.T) {
	t.Parallel()
	const name = "one.txt"
	content := []byte("ytrack")
	tests := []struct {
		name    string
		body    string
		details []detail
	}{
		{
			name:    "no attachment at all",
			body:    `[]`,
			details: []detail{{"arrived_count", 0}, {"upstream_body", `[]`}},
		},
		{
			name:    "two attachments",
			body:    twice(filed(name, len(content))),
			details: []detail{{"arrived_count", 2}},
		},
		{
			name: "a name the server kept as another",
			body: filed(`"b.txt`, len(content)),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "name"}, {"written", name}, {"arrived", `"b.txt`}}}},
			},
		},
		{
			name: "a size that is not the count of bytes that went out",
			body: filed(name, len(content)+1),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "size"}, {"written", len(content)}, {"arrived", len(content) + 1}}}},
			},
		},
		// A size that is no number at all is a disagreement like any other, and what stands under arrived is
		// what the server sent rather than a null of ytrack's own: ADR-0005 asks for what arrived word for word.
		{
			name: "a size that arrived as text",
			body: filedAs(strconv.Quote(name), strconv.Quote(strconv.Itoa(len(content)))),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "size"}, {"written", len(content)}, {"arrived", "6"}}}},
			},
		},
		// The same for the name, which is held to text: a number came back, and the refusal names that number.
		{
			name: "a name that arrived as a number",
			body: filedAs("7", strconv.Itoa(len(content))),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "name"}, {"written", name}, {"arrived", 7}}}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(http.StatusOK, tc.body))
			path := fileHolding(t, name, content)

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, detail{"request", attachmentWriteRequest(server.url, "DEV-1", attachmentFields)},
				found.details[0])
			assert.Subset(t, found.details, tc.details)
			assert.Len(t, server.requests(), 1)
		})
	}
}

// What the server refuses the upload with is read the way a status is read everywhere, and the words it
// used pass on as they are: ytrack sets no limit of its own, so the one the instance holds — and the one the
// project holds on how many files an issue carries — are the server's to name.
func TestAttachmentCreateRefusesWhatTheServerAnswered(t *testing.T) {
	t.Parallel()
	const overTheLimit = "the request was rejected because its size (10485761) exceeds the configured maximum (10485760)"
	const tooManyFiles = "К задаче можно прикрепить только 500 файлов, попробуйте удалить неактуальные."
	tests := []struct {
		name    string
		status  int
		body    string
		code    string
		message string
	}{
		{
			name: "a file over what the instance allows", status: http.StatusBadRequest,
			body: `{"error":"Bad Request","error_description":` + strconv.Quote(overTheLimit) + `}`,
			code: "rejected", message: overTheLimit,
		},
		{
			name: "an issue that already holds the most files it may", status: http.StatusBadRequest,
			body: `{"error":"invalid_properties","error_description":` + strconv.Quote(tooManyFiles) + `}`,
			code: "rejected", message: tooManyFiles,
		},
		{
			name: "an owner the server has none of", status: http.StatusNotFound,
			body: entityNotFound("DEV-1"), code: "not_found", message: "Entity with id DEV-1 not found",
		},
		{
			name: "a token the server does not let through", status: http.StatusForbidden,
			body: `{"error":"Forbidden","error_description":"Access to the project is denied"}`,
			code: "denied", message: "Access to the project is denied",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(tc.status, tc.body))
			path := aFileToAttach(t)

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.message, detailNamed(t, found, "upstream_message"))
			assert.Equal(t, detail{"request", attachmentWriteRequest(server.url, "DEV-1", attachmentFields)},
				found.details[0])
			assert.Len(t, server.requests(), 1)
		})
	}
}

// twice is an array of two of what an answer of one carries: a server that filed the file more than once, or
// answered about something other than the write.
func twice(one string) string {
	inside := strings.TrimSuffix(strings.TrimPrefix(one, "["), "]")
	return "[" + inside + "," + inside + "]"
}

// repeatedBytes is a file of every byte there is, over and over: a content no text encoding would survive, so
// a part that arrived whole is told from one that was read as text somewhere on the way.
func repeatedBytes(size int) []byte {
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 256)
	}
	return content
}

// everyLatinRune is repeatedBytes for a scenario whose request and answer are written into a cassette: every
// rune from U+0001 to U+00FF, which covers both halves of the byte range and breaks under any transcoding,
// while staying valid UTF-8 so the cassette keeps the bodies as text rather than as base64.
func everyLatinRune(times int) []byte {
	runes := make([]rune, 0, 0xFF)
	for r := rune(1); r <= 0xFF; r++ {
		runes = append(runes, r)
	}
	return bytes.Repeat([]byte(string(runes)), times)
}

// A file attached to an issue of the polygon for real: the document names it as it went out, the
// signed link it prints hands the very bytes back to whoever holds it, and the list of the issue holds the
// attachment the write says it filed.
func TestAttachmentCreateAttachesAFileToAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := attachedIssue(t, dev)
	const name = "заметка контракта; 100%.bin"
	content := everyLatinRune(2)
	path := fileHolding(t, name, content)

	got := runWith(t, dev.env(), "attachment", "create", issue, path)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	written := requireAttachmentPrinted(t, got.stdout)
	assert.Regexp(t, attachmentIDForm, written.ID)
	assert.Equal(t, name, written.Name)
	assert.Equal(t, len(content), written.Size)
	assert.Equal(t, "application/octet-stream", written.MimeType)
	reference, whole := strings.CutPrefix(written.URL, dev.url)
	require.True(t, whole, "%q does not begin with the address ytrack was given", written.URL)
	assert.Regexp(t, signedLinkForm, reference)

	response, body := fetched(t, written.URL, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "body: %s", body)
	assert.Equal(t, content, body)

	listed := requireAttachmentListing(t, runWith(t, dev.env(), "attachment", "list", issue))
	assert.Equal(t, 1, listed.Total)
	require.Len(t, listed.Attachments, 1)
	assert.Equal(t, written.ID, listed.Attachments[0].ID)
}

// A file with nothing in it is a file: the polygon keeps it, names it and prints a size of zero, and
// nothing about an empty stream makes the check of the answer say otherwise.
func TestAttachmentCreateAttachesAnEmptyFileToAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := attachedIssue(t, dev)
	path := fileHolding(t, "пусто.bin", nil)

	got := runWith(t, dev.env(), "attachment", "create", issue, path)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	written := requireAttachmentPrinted(t, got.stdout)
	assert.Equal(t, "пусто.bin", written.Name)
	assert.Equal(t, 0, written.Size)
}

// An issue nobody filed is answered 404 by the server itself, and nothing about the file was asked
// beforehand: one request is the whole call.
func TestAttachmentCreateRefusesAnIssueThePolygonHasNoneOf(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	path := fileHolding(t, "заметка.bin", []byte("ytrack"))

	got := runWith(t, dev.env(), "attachment", "create", "DEV-99999", path)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Entity with id DEV-99999 not found", detailNamed(t, found, "upstream_message"))
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/issues/DEV-99999/attachments", dev.sentPaths()[0])
}

// An issue the token may not see is the same 404 as an issue nobody filed, and it is a 404 the server
// gives instead of writing: the issue holds no attachment afterwards.
func TestAttachmentCreateRefusesAnIssueTheTokenIsNotAnsweredFor(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := attachedIssue(t, dev)
	path := fileHolding(t, "заметка.bin", []byte("ytrack"))
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"attachment", "create", issue, path)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Entity with id "+issue+" not found", detailNamed(t, found, "upstream_message"))
	assert.Len(t, dev.requests()[before:], 1)

	listed := requireAttachmentListing(t, runWith(t, dev.env(), "attachment", "list", issue))
	assert.Equal(t, 0, listed.Total)
	assert.Empty(t, listed.Attachments)
}

// attachedIssue is the fixture of a contract test that needs an issue of its own to attach files to: it is
// filed by the command that files issues, with the custom fields DEV requires, and taken away afterwards with
// everything hanging from it.
func attachedIssue(t *testing.T, dev *upstream) string {
	t.Helper()
	summary := "ytrack contract " + t.Name()
	argv := append([]string{"issue", "create", "DEV", "--summary", summary}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

// requireAttachmentPrinted is the one attachment a creation printed, read back off stdout.
func requireAttachmentPrinted(t *testing.T, stdout string) printedAttachment {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(stdout))
	decoder.KnownFields(true)
	var printed printedAttachment
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", stdout)
	return printed
}
