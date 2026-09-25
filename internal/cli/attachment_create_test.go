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

const uploadedField = "files[0]"

func attachmentWriteRequest(address, owner, fields string) string {
	return "POST " + address + "/api/issues/" + owner + "/attachments?fields=" + fields
}

func filed(name string, size int) string {
	return filedAs(strconv.Quote(name), strconv.Itoa(size))
}

func filedAs(name, size string) string {
	return `[{"$type":"IssueAttachment","id":"12-9","name":` + name +
		`,"size":` + size + `,"mimeType":"application/octet-stream",` +
		`"url":"/api/files/12-9?sign=s&updated=1"}]`
}

func fileWith(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, content, 0o600))
	return path
}

func aFileToAttach(t *testing.T) string {
	t.Helper()
	return fileWith(t, "attached.txt", []byte("ytrack"))
}

func requireOnePart(t *testing.T, server *upstream) formPart {
	t.Helper()
	parts := sentParts(t, server, 0)
	require.Len(t, parts, 1, "an upload is one part and nothing else")
	assert.Equal(t, uploadedField, parts[0].field)
	return parts[0]
}

func TestAttachmentCreateRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: nil},
		{name: "an owner alone", argv: []string{"DEV-1"}},
		{name: "a third argument", argv: []string{"DEV-1", "a.txt", "b.txt"}},
		{name: "a path beginning with a dash", argv: []string{"DEV-1", "-x.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"attachment", "create"}, tc.argv...)...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

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
				path := fileWith(t, "locked.txt", []byte("x"))
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

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestAttachmentCreateReadsALoneDashAsAFileOfThatName(t *testing.T) {
	t.Parallel()

	t.Run("no file of that name here", func(t *testing.T) {
		t.Parallel()
		server := serveNothing(t)
		stdin, err := os.Open(fileWith(t, "standard-input.bin", []byte("the bytes of standard input")))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, stdin.Close()) })

		got := runOn(t, stdin, server.env(), "attachment", "create", "DEV-1", "-")

		assert.Equal(t, "bad_usage", requireFault(t, got).code)
		assert.Empty(t, server.requests())
		read, err := stdin.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		assert.Zero(t, read, "a byte of standard input was read")
	})

	t.Run("a file of that name goes out under it", func(t *testing.T) {
		t.Parallel()
		server := serve(t, respondWith(http.StatusOK, filed("-", 1)))
		path := fileWith(t, "-", []byte("x"))

		got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		sent := requireOnePart(t, server)
		assert.Equal(t, "-", sent.file)
		assert.Equal(t, "x", sent.content)
		assert.Equal(t, "-", requireAttachmentPrinted(t, got.stdout).Name)
	})
}

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
			path := fileWith(t, tc.file, []byte("x"))

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestAttachmentCreateHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"attachment", "create", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, attachmentFields)
}

func TestAttachmentCreateSendsTheFileAsOneStreamedPart(t *testing.T) {
	t.Parallel()
	const name = "[bug] заметка; 100%.bin"
	content := repeatedBytes(300 * 1024)
	server := serve(t, respondWith(http.StatusOK, filed(name, len(content))))
	path := fileWith(t, name, content)

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

func TestAttachmentCreateSendsEveryOtherNameUnchanged(t *testing.T) {
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
		{name: "a leading dash", file: "-x.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, filed(tc.file, 1)))
			path := fileWith(t, tc.file, []byte("x"))

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			sent := requireOnePart(t, server)
			assert.Equal(t, tc.file, sent.file)
			assert.Equal(t, `form-data; name="`+uploadedField+`"; filename="`+tc.file+`"`, sent.disposition)
			assert.Equal(t, tc.file, requireAttachmentPrinted(t, got.stdout).Name)
		})
	}
}

func TestAttachmentCreateReadsARelativePathAgainstTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	const name = "relative.bin"
	content := repeatedBytes(1024)
	absolute := fileWith(t, name, content)
	here, err := os.Getwd()
	require.NoError(t, err)
	relative, err := filepath.Rel(here, absolute)
	require.NoError(t, err)
	require.False(t, filepath.IsAbs(relative))

	for _, path := range []string{absolute, relative} {
		t.Run(map[bool]string{true: "absolute", false: "relative"}[filepath.IsAbs(path)], func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, filed(name, len(content))))

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			sent := requireOnePart(t, server)
			assert.Equal(t, name, sent.file)
			assert.Equal(t, string(content), sent.content)
		})
	}
}

func TestAttachmentCreateAsksForTheNameAndTheSizeItChecks(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, filed("one.txt", 1)))
	path := fileWith(t, "one.txt", []byte("x"))

	got := runWith(t, server.env(), "attachment", "create", "DEV-1", path, "--fields", "id")

	assert.Equal(t, outcome{stdout: `id: "12-9"` + "\n"}, got)
	assert.Equal(t, []string{"id,name,size"}, server.sentFields())
}

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
			details: []detail{{"actual_count", 0}, {"upstream_body", `[]`}},
		},
		{
			name:    "two attachments",
			body:    twice(filed(name, len(content))),
			details: []detail{{"actual_count", 2}},
		},
		{
			name: "a name the server kept as another",
			body: filed(`"b.txt`, len(content)),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "name"}, {"expected", name}, {"actual", `"b.txt`}}}},
			},
		},
		{
			name: "a size that is not the count of bytes that went out",
			body: filed(name, len(content)+1),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "size"}, {"expected", len(content)}, {"actual", len(content) + 1}}}},
			},
		},
		{
			name: "a size that arrived as text",
			body: filedAs(strconv.Quote(name), strconv.Quote(strconv.Itoa(len(content)))),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "size"}, {"expected", len(content)}, {"actual", "6"}}}},
			},
		},
		{
			name: "a name that arrived as a number",
			body: filedAs("7", strconv.Itoa(len(content))),
			details: []detail{
				{"attachment", "12-9"},
				{"mismatch", []any{[]detail{{"field", "name"}, {"expected", name}, {"actual", 7}}}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, tc.body))
			path := fileWith(t, name, content)

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, detail{"request", attachmentWriteRequest(server.url, "DEV-1", attachmentFields)},
				found.details[0])
			assert.Subset(t, found.details, tc.details)
			assert.Len(t, server.requests(), 1)
		})
	}
}

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
			server := serve(t, respondWith(tc.status, tc.body))
			path := aFileToAttach(t)

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.message, detailNamed(t, found, "upstream_message"))
			assert.Equal(t, detail{"request", attachmentWriteRequest(server.url, "DEV-1", attachmentFields)},
				found.details[0])
			assert.Len(t, server.requests(), 1)
		})
	}
}

func twice(one string) string {
	inside := strings.TrimSuffix(strings.TrimPrefix(one, "["), "]")
	return "[" + inside + "," + inside + "]"
}

func repeatedBytes(size int) []byte {
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 256)
	}
	return content
}

func everyLatin1RuneAsUTF8(times int) []byte {
	runes := make([]rune, 0, 0xFF)
	for r := rune(1); r <= 0xFF; r++ {
		runes = append(runes, r)
	}
	return bytes.Repeat([]byte(string(runes)), times)
}

func TestAttachmentCreateAttachesAFileToAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := attachedIssue(t, dev)
	const name = "заметка контракта; 100%.bin"
	content := everyLatin1RuneAsUTF8(2)
	path := fileWith(t, name, content)

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

func TestAttachmentCreateAttachesAnEmptyFileToAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := attachedIssue(t, dev)
	path := fileWith(t, "пусто.bin", nil)

	got := runWith(t, dev.env(), "attachment", "create", issue, path)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	written := requireAttachmentPrinted(t, got.stdout)
	assert.Equal(t, "пусто.bin", written.Name)
	assert.Equal(t, 0, written.Size)
}

func TestAttachmentCreateRefusesAnIssueTheDevInstanceHasNoneOf(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	path := fileWith(t, "заметка.bin", []byte("ytrack"))

	got := runWith(t, dev.env(), "attachment", "create", "DEV-99999", path)

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Entity with id DEV-99999 not found", detailNamed(t, found, "upstream_message"))
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/issues/DEV-99999/attachments", dev.sentPaths()[0])
}

func TestAttachmentCreateRefusesAnIssueTheTokenIsNotAnsweredFor(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := attachedIssue(t, dev)
	path := fileWith(t, "заметка.bin", []byte("ytrack"))
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"attachment", "create", issue, path)

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Entity with id "+issue+" not found", detailNamed(t, found, "upstream_message"))
	assert.Len(t, dev.requests()[before:], 1)

	listed := requireAttachmentListing(t, runWith(t, dev.env(), "attachment", "list", issue))
	assert.Equal(t, 0, listed.Total)
	assert.Empty(t, listed.Attachments)
}

func attachedIssue(t *testing.T, dev *upstream) string {
	t.Helper()
	summary := "ytrack contract " + t.Name()
	argv := append([]string{"issue", "create", "DEV", "--summary", summary}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

func requireAttachmentPrinted(t *testing.T, stdout string) printedAttachment {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(stdout))
	decoder.KnownFields(true)
	var printed printedAttachment
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", stdout)
	return printed
}
