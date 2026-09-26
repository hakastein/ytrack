package cli_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func filed(name string, size int) string {
	return `[{"$type":"IssueAttachment","id":"12-9","name":` + strconv.Quote(name) +
		`,"size":` + strconv.Itoa(size) + `,"mimeType":"application/octet-stream",` +
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

func requireSentFile(t *testing.T, server *fake.Server) formPart {
	t.Helper()
	parts := sentParts(t, server, len(server.Requests())-1)
	require.Len(t, parts, 1, "an upload is one part and nothing else")
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

			got := runWith(t, envOf(server), "attachment", "create", "DEV-1", path)

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

	got := runWith(t, envOf(server), "attachment", "create", "DEV-1", path)

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestAttachmentCreateReadsNoStandardInputForALoneDash(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)
	stdin, err := os.Open(fileWith(t, "standard-input.bin", []byte("the bytes of standard input")))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stdin.Close()) })

	got := runOn(t, stdin, envOf(server), "attachment", "create", "DEV-1", "-")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
	read, err := stdin.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	assert.Zero(t, read, "a byte of standard input was read")
}

func TestAttachmentCreateSendsAFileNamedDashUnderThatName(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, filed("-", 1)))
	path := fileWith(t, "-", []byte("x"))

	got := runWith(t, envOf(server), "attachment", "create", "DEV-1", path)

	assert.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	sent := requireSentFile(t, server)
	assert.Equal(t, "-", sent.file)
	assert.Equal(t, "x", sent.content)
}

func TestAttachmentCreateSendsTheFileAndPrintsTheAttachment(t *testing.T) {
	t.Parallel()
	const answer = `[{"$type":"IssueAttachment","id":"12-9","name":"one.bin","size":6}]`
	server := fake.Serve(t, fake.JSON(http.StatusOK, answer))
	path := fileWith(t, "one.bin", []byte("ytrack"))

	got := runWith(t, envOf(server), "attachment", "create", "DEV-1", path, "--fields", "id,name,size")

	assert.Equal(t, outcome{stdout: `id: "12-9"` + "\n" + `name: "one.bin"` + "\nsize: 6\n"}, got)
	assert.Contains(t, server.Routes(), "POST /api/issues/DEV-1/attachments")
	sent := requireSentFile(t, server)
	assert.Equal(t, "one.bin", sent.file)
	assert.Equal(t, "ytrack", sent.content)
}

func TestAttachmentCreateReadsARelativePathFromTheWorkingDirectoryAndNotFromPWD(t *testing.T) {
	here, elsewhere := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(here, "relative.txt"), []byte("here"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "relative.txt"), []byte("elsewhere"), 0o600))
	t.Chdir(here)
	t.Setenv("PWD", elsewhere)
	server := fake.Serve(t, fake.JSON(http.StatusOK, filed("relative.txt", len("here"))))

	got := runWith(t, append(envOf(server), "PWD="+elsewhere), "attachment", "create", "DEV-1", "relative.txt")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "here", requireSentFile(t, server).content)
}
