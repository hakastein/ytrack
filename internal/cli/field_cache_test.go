package cli_test

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	metadataPath   = "/api/admin/projects/DEV"
	firstFieldPath = metadataPath + "/customFields/180-1"
)

var showType = []string{"field", "show", "DEV", "Type"}

func typeOnlyProject(t *testing.T) *fake.Server {
	t.Helper()
	return serveTheProject(t, projectMetadata(projectField("180-1", "Type", "Kind")),
		fake.JSON(http.StatusOK, oneField("Type", "Kind", false)))
}

func atHome(server *fake.Server, home string) []string {
	return append(envOf(server), "HOME="+home)
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
			env := append(envOf(server), tc.home...)

			first := runWith(t, env, showType...)
			runWith(t, env, showType...)

			require.Equal(t, 0, first.code, "stderr: %s", first.stderr)
			assert.Equal(t, 2, timesAsked(server, metadataPath), "paths: %v", server.Paths())
		})
	}
}

func TestFieldShowWritesTheCacheUnderTheYtrackDirectoryOfHome(t *testing.T) {
	t.Parallel()
	server, home := typeOnlyProject(t), t.TempDir()

	got := runWith(t, atHome(server, home), showType...)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{filepath.Join(".ytrack", "cache")}, directoriesHoldingFilesUnder(t, home))
}

func timesAsked(server *fake.Server, path string) int {
	asked := 0
	for _, sent := range server.Paths() {
		if sent == path {
			asked++
		}
	}
	return asked
}

// The two directories of HOME each file lies under, once each.
func directoriesHoldingFilesUnder(t *testing.T, home string) []string {
	t.Helper()
	var held []string
	require.NoError(t, filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(home, path)
		parts := strings.SplitN(relative, string(filepath.Separator), 3)
		if under := filepath.Join(parts[:min(2, len(parts))]...); !slices.Contains(held, under) {
			held = append(held, under)
		}
		return err
	}))
	return held
}
