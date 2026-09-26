package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

func openLocalFile(path string) (*os.File, *diag.Fault) {
	// Stat before Open: opening a named pipe blocks until a writer appears.
	before, err := os.Stat(path)
	if err != nil {
		return nil, unreadableFile(path, err)
	}
	if !before.Mode().IsRegular() {
		return nil, invalidFileFault(path, "is "+describeFileMode(before.Mode()))
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, unreadableFile(path, err)
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, unreadableFile(path, err)
	}
	if !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, invalidFileFault(path, "is no longer the file it stood for a moment ago")
	}
	return file, nil
}

func unreadableFile(path string, err error) *diag.Fault {
	var failed *fs.PathError
	if errors.As(err, &failed) {
		err = failed.Err
	}
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: fmt.Sprintf("file %s cannot be read: %s", render.Quote(path), err)}
}

func invalidFileFault(path, because string) *diag.Fault {
	message := fmt.Sprintf("file %s %s, and an attachment is the bytes of one regular file", render.Quote(path), because)
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

func describeFileMode(mode fs.FileMode) string {
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
