package script

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

type irregularFile struct {
	mode fs.FileMode
}

func (i *irregularFile) Error() string {
	return "is " + describeFileMode(i.mode)
}

var errSwapped = errors.New("is no longer the file it stood for a moment ago")

func openRegular(path string) (*os.File, error) {
	// Stat before Open: opening a named pipe blocks until a writer appears.
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, &irregularFile{mode: before.Mode()}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, errSwapped
	}
	return file, nil
}

func openLocalFile(path string) (*os.File, *diag.Fault) {
	file, err := openRegular(path)
	var irregular *irregularFile
	switch {
	case err == nil:
		return file, nil
	case errors.As(err, &irregular), errors.Is(err, errSwapped):
		message := fmt.Sprintf("file %s %s, and an attachment is the bytes of one regular file", render.Quote(path), err)
		return nil, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return nil, &diag.Fault{Code: youtrack.CodeBadUsage, Message: fmt.Sprintf("file %s cannot be read: %s",
		render.Quote(path), unwrapPath(err))}
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
