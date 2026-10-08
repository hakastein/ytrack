package cli_test

import (
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
