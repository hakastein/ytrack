package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

// openLocalFile is the one place ytrack reads a file of the caller's own filesystem, reached from the one
// argument that is a path: the file attachment create sends. Nothing of the tool writes a path a caller wrote.
//
// The path goes to the kernel as it was typed. A relative one is resolved by the kernel against the working
// directory of the process — the directory the caller built the path from — so ytrack opens the file cat of
// that same argument would; PWD is not read and nothing is joined to anything, which is what keeps ".." over a
// symlink from resolving one way here and another way there.
func openLocalFile(path string) (youtrack.AttachedFile, *diag.Fault) {
	// What the path stands for is settled before it is opened: opening a named pipe for reading waits for a
	// writer that may never come, and the command would neither print nor send anything until it did.
	before, err := os.Stat(path)
	if err != nil {
		return youtrack.AttachedFile{}, unreadableFile(path, err)
	}
	if !before.Mode().IsRegular() {
		return youtrack.AttachedFile{}, refusedFile(path, "is "+whatItIs(before.Mode()))
	}
	file, err := os.Open(path)
	if err != nil {
		return youtrack.AttachedFile{}, unreadableFile(path, err)
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return youtrack.AttachedFile{}, unreadableFile(path, err)
	}
	// The check above is about the path and this one is about the handle, which is as close as the two get:
	// between them the path may have come to stand for something else entirely.
	if !os.SameFile(before, opened) {
		_ = file.Close()
		return youtrack.AttachedFile{}, refusedFile(path, "is no longer the file it stood for a moment ago")
	}
	return youtrack.AttachedFile{Name: filepath.Base(path), Body: file}, nil
}

// The words of the operating system pass on as they are: they say which part of the path could not be reached,
// and the path itself is named beside them rather than left inside the sentence twice.
func unreadableFile(path string, err error) *diag.Fault {
	var failed *fs.PathError
	if errors.As(err, &failed) {
		err = failed.Err
	}
	return &diag.Fault{Code: diag.BadUsage, Message: fmt.Sprintf("file %s cannot be read: %s", render.Quote(path), err)}
}

func refusedFile(path, because string) *diag.Fault {
	message := fmt.Sprintf("file %s %s, and an attachment is the bytes of one regular file", render.Quote(path), because)
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

// What the path stands for where it is no regular file, in the words a caller would use.
func whatItIs(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeCharDevice != 0:
		return "a character device"
	case mode&fs.ModeDevice != 0:
		return "a block device"
	}
	return "no regular file"
}
