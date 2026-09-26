package diag

import (
	"errors"

	"github.com/hakastein/go-youtrack"
)

type document struct {
	Code    youtrack.Code
	Message string
	Details []youtrack.Pair
}

type Fault struct {
	Code       youtrack.Code
	Message    string
	Details    []youtrack.Pair
	AfterWrite bool
}

func FromError(err error) *Fault {
	var failed *youtrack.Error
	if !errors.As(err, &failed) {
		return &Fault{Code: youtrack.CodeUpstreamFailed, Message: err.Error()}
	}
	return &Fault{Code: failed.Code, Message: failed.Message, Details: failed.Details, AfterWrite: failed.AfterWrite}
}

const (
	exitFailed         = 1
	exitMayHaveWritten = 2
)

func (f *Fault) Error() string {
	return string(f.Code) + ": " + f.Message
}

func (f *Fault) ExitCode() int {
	if f.Code == youtrack.CodeWriteUncertain || f.AfterWrite {
		return exitMayHaveWritten
	}
	return exitFailed
}

func (f *Fault) Node() *youtrack.Node {
	return f.document().node()
}

func (f *Fault) document() document {
	return document{Code: f.Code, Message: f.Message, Details: f.Details}
}

func (d document) node() *youtrack.Node {
	pairs := []youtrack.Pair{
		{Key: "code", Value: youtrack.NewString(string(d.Code))},
		{Key: "message", Value: youtrack.NewString(d.Message)},
	}
	return youtrack.NewMap(append(pairs, d.Details...)...)
}
