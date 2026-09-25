package cli_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func requireOnePart(t *testing.T, server *fake.Server) formPart {
	t.Helper()
	parts := sentParts(t, server, 0)
	require.Len(t, parts, 1, "an upload is one part and nothing else")
	assert.Equal(t, uploadedField, parts[0].field)
	return parts[0]
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			path := tc.path(t)

			got := runWith(t, server.Env(), "attachment", "create", "DEV-1", path)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestAttachmentCreateRefusesAFileTheCallerMayNotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of any mode")
	}
	t.Parallel()
	server := fake.ServeNothing(t)
	path := fileWith(t, "locked.txt", []byte("x"))
	require.NoError(t, os.Chmod(path, 0o000))

	got := runWith(t, server.Env(), "attachment", "create", "DEV-1", path)

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestAttachmentCreateReadsALoneDashAsAFileOfThatName(t *testing.T) {
	t.Parallel()

	t.Run("no file of that name here", func(t *testing.T) {
		t.Parallel()
		server := fake.ServeNothing(t)
		stdin, err := os.Open(fileWith(t, "standard-input.bin", []byte("the bytes of standard input")))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, stdin.Close()) })

		got := runOn(t, stdin, server.Env(), "attachment", "create", "DEV-1", "-")

		assert.Equal(t, "bad_usage", requireFault(t, got).code)
		assert.Empty(t, server.Requests())
		read, err := stdin.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		assert.Zero(t, read, "a byte of standard input was read")
	})

	t.Run("a file of that name goes out under it", func(t *testing.T) {
		t.Parallel()
		server := fake.Serve(t, fake.JSON(http.StatusOK, filed("-", 1)))
		path := fileWith(t, "-", []byte("x"))

		got := runWith(t, server.Env(), "attachment", "create", "DEV-1", path, "--fields", "name")

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		sent := requireOnePart(t, server)
		assert.Equal(t, "-", sent.file)
		assert.Equal(t, "x", sent.content)
	})
}

func TestAttachmentCreateSendsTheFileAndPrintsTheAttachment(t *testing.T) {
	t.Parallel()
	const name = "one.bin"
	server := fake.Serve(t, fake.JSON(http.StatusOK, filed(name, len("ytrack"))))
	path := fileWith(t, name, []byte("ytrack"))

	got := runWith(t, server.Env(), "attachment", "create", "DEV-1", path)

	want := `id: "12-9"` + "\n" + `name: "one.bin"` + "\nsize: 6\n" + `mimeType: "application/octet-stream"` + "\n" +
		`url: "` + server.Origin + `/api/files/12-9?sign=s&updated=1"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodPost}, server.Methods())
	assert.Equal(t, []string{"/api/issues/DEV-1/attachments?fields=" + attachmentFields}, server.Targets(t))
	assert.Equal(t, formPart{
		field:       uploadedField,
		file:        name,
		content:     "ytrack",
		disposition: `form-data; name="` + uploadedField + `"; filename="` + name + `"`,
	}, requireOnePart(t, server))
}

func TestAttachmentCreateReadsARelativePathFromTheWorkingDirectoryAndNotFromPWD(t *testing.T) {
	here, elsewhere := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(here, "relative.txt"), []byte("here"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "relative.txt"), []byte("elsewhere"), 0o600))
	t.Chdir(here)
	t.Setenv("PWD", elsewhere)
	server := fake.Serve(t, fake.JSON(http.StatusOK, filed("relative.txt", len("here"))))

	got := runWith(t, append(server.Env(), "PWD="+elsewhere), "attachment", "create", "DEV-1", "relative.txt",
		"--fields", "name")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "here", requireOnePart(t, server).content)
}

func TestAttachmentCreateExitsWith2WhereTheAnswerIsNotTheFileThatWentOut(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, filed("attached.txt", len("ytrack")+1)))
	path := aFileToAttach(t)

	got := runWith(t, server.Env(), "attachment", "create", "DEV-1", path, "--fields", "id,name,size")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", attachmentWriteRequest(server.URL, "DEV-1", "id,name,size")},
			{"attachment", "12-9"},
			{"mismatch", []any{[]detail{{"field", "size"}, {"expected", len("ytrack")}, {"actual", len("ytrack") + 1}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}
