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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aBlockDevice is one the machine already has, stated by the scenario and opened by nobody: a block device is
// the kernel's to hand out and none of the five kinds is worth making one for. A machine with none of them —
// a container given no disk — has no such path to name, and the scenario has nothing to run against.
func aBlockDevice(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir("/dev")
	require.NoError(t, err)
	for _, entry := range entries {
		// The entry rather than what it may point at: a symlink of /dev leads to a device the scenario would
		// then name by the wrong path.
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

// The two files a check after the open would not save the caller from. Opening a named pipe for reading
// waits for a writer that may never come, and this command would hang with nothing printed and nothing sent —
// the deadline of the test is what says it does not. /dev/null opens and reads as zero bytes, which would go
// out as a real upload of an empty file the caller never named.
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
			server := serveNothing(t)
			path := tc.path(t)

			got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)

			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

// Х. What the order of the two calls is there for: the path is stated before it is opened, and the handle is
// then stated again and held against the first. Between them the path may come to stand for something else —
// here a symlink swung between two files of the same length — and the attachment would be the bytes of a file
// the caller never named.
//
// Nothing of the tool runs between the two calls, so the window is the kernel's and the scenario cannot place
// itself inside it: it swings the link as fast as the machine allows and sends the command until one of them
// lands in the window. Both files are named by the link, so an attempt that missed it is an ordinary upload
// that the server answers and the run passes.
func TestAttachmentCreateRefusesAPathThatNoLongerPointsToTheCheckedFile(t *testing.T) {
	dir := t.TempDir()
	const length = 4
	one, two := filepath.Join(dir, "one"), filepath.Join(dir, "two")
	require.NoError(t, os.WriteFile(one, []byte("aaaa"), 0o600))
	require.NoError(t, os.WriteFile(two, []byte("bbbb"), 0o600))
	const name = "file.bin"
	path := filepath.Join(dir, name)
	require.NoError(t, os.Symlink(one, path))
	flipSymlink(t, path, one, two)
	server := serve(t, respondWith(http.StatusOK, filed(name, length)))

	// One attempt is some sixty milliseconds and the window is caught in the first few of them, measured under
	// -race as well, so ten seconds is the run of attempts that says the branch is gone rather than unlucky.
	deadline := time.Now().Add(10 * time.Second)
	for attempt := 1; ; attempt++ {
		got := runWith(t, server.env(), "attachment", "create", "DEV-1", path)
		if got.code != 0 {
			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			return
		}
		require.False(t, time.Now().After(deadline),
			"%d attempts and the path never changed between the two calls", attempt)
	}
}

// flipSymlink points path at one file and then at the other, over and over, until the test is done: the rename of
// a symlink over another is one step, so the path stands for one of the two files at every moment and never
// for nothing.
func flipSymlink(t *testing.T, path, one, two string) {
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
			if os.Symlink(at, next) != nil || os.Rename(next, path) != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-swung
	})
}
