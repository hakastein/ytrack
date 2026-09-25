//go:build unix

package cli_test

import (
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func aBlockDevice(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir("/dev")
	require.NoError(t, err)
	for _, entry := range entries {
		found, err := entry.Info()
		if err != nil {
			continue
		}
		if found.Mode()&fs.ModeDevice != 0 && found.Mode()&fs.ModeCharDevice == 0 {
			return filepath.Join("/dev", entry.Name())
		}
	}
	t.Skip("the machine holds no block device")
	return ""
}

func TestAttachmentCreateRefusesAFileThatIsNoOrdinaryOneOnUnix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "a named pipe",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "pipe")
				require.NoError(t, syscall.Mkfifo(path, 0o600))
				return path
			},
		},
		{
			name: "a character device",
			path: func(*testing.T) string { return "/dev/null" },
		},
		{
			name: "a socket",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "s")
				listening, err := net.Listen("unix", path)
				require.NoError(t, err)
				t.Cleanup(func() { assert.NoError(t, listening.Close()) })
				return path
			},
		},
		{
			name: "a block device",
			path: aBlockDevice,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			path := tc.path(t)

			got := runWith(t, server.Env(), "attachment", "create", "DEV-1", path)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestAttachmentCreateRefusesAPathThatNoLongerPointsToTheCheckedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one"), filepath.Join(dir, "two")
	require.NoError(t, os.WriteFile(one, []byte("aaaa"), 0o600))
	require.NoError(t, os.WriteFile(two, []byte("bbbb"), 0o600))
	path := filepath.Join(dir, "file.bin")
	require.NoError(t, os.Symlink(one, path))
	keepFlippingSymlink(t, path, one, two)
	server := fake.Serve(t, fake.JSON(http.StatusOK, filed("file.bin", len("aaaa"))))
	var got outcome

	require.Eventually(t, func() bool {
		got = runWith(t, server.Env(), "attachment", "create", "DEV-1", path, "--fields", "name")
		return got.code != 0
	}, 10*time.Second, time.Millisecond, "the path never changed between the two calls")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
}

func keepFlippingSymlink(t *testing.T, path, one, two string) {
	t.Helper()
	stop := make(chan struct{})
	swung := make(chan struct{})
	go func() {
		defer close(swung)
		next := filepath.Join(filepath.Dir(path), "next")
		for turn := 0; ; turn++ {
			select {
			case <-stop:
				return
			default:
			}
			at := one
			if turn%2 == 1 {
				at = two
			}
			if repointSymlinkAtomically(path, at, next) != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-swung
	})
}

func repointSymlinkAtomically(link, target, staged string) error {
	if err := os.Symlink(target, staged); err != nil {
		return err
	}
	return os.Rename(staged, link)
}
