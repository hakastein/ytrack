package cli_test

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	metadataPath   = "/api/admin/projects/DEV"
	firstFieldPath = metadataPath + "/customFields/180-1"
)

var showType = []string{"field", "show", "DEV", "Type", "--fields", "field(name)"}

func typeOnlyProject(t *testing.T) *fake.Server {
	t.Helper()
	return serveTheProject(t, projectMetadata(projectField("180-1", "Type", "Kind")),
		fake.JSON(http.StatusOK, oneField("Type", "Kind", false)))
}

func atHome(server *fake.Server, home string) []string {
	return append(server.Env(), "HOME="+home)
}

func TestFieldShowTakesTheMetadataTheRunBeforeLeftOnDisk(t *testing.T) {
	t.Parallel()
	server, home := typeOnlyProject(t), t.TempDir()

	first := runWith(t, atHome(server, home), showType...)
	second := runWith(t, atHome(server, home), showType...)

	require.Equal(t, 0, first.code, "stderr: %s", first.stderr)
	assert.Equal(t, first, second)
	assert.Equal(t, []string{metadataPath, firstFieldPath, firstFieldPath}, server.Paths())
}

func TestFieldShowKeepsNoCacheWithoutAnAbsoluteHome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		home []string
	}{
		{name: "no home directory"},
		{name: "a relative home directory", home: []string{"HOME=" + filepath.Join("relative", "home")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := typeOnlyProject(t)
			env := append(server.Env(), tc.home...)

			first := runWith(t, env, showType...)
			second := runWith(t, env, showType...)

			require.Equal(t, 0, first.code, "stderr: %s", first.stderr)
			assert.Equal(t, first, second)
			assert.Equal(t, []string{metadataPath, firstFieldPath, metadataPath, firstFieldPath}, server.Paths())
		})
	}
}

func TestFieldShowWritesTheCacheUnderTheYtrackDirectoryOfHome(t *testing.T) {
	t.Parallel()
	server, home := typeOnlyProject(t), t.TempDir()

	got := runWith(t, atHome(server, home), showType...)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{filepath.Join(".ytrack", "cache")}, cacheRootsOfFilesUnder(t, home))
}

func cacheRootsOfFilesUnder(t *testing.T, home string) []string {
	t.Helper()
	var roots []string
	require.NoError(t, filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(home, path)
		roots = append(roots, filepath.Dir(filepath.Dir(relative)))
		return err
	}))
	return roots
}
