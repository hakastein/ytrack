package diag

import "github.com/hakastein/ytrack/internal/render"

type document struct {
	Code    Code
	Message string
	Details []render.Pair
}

type Fault struct {
	Code       Code
	Message    string
	Details    []render.Pair
	AfterWrite bool
}

const (
	exitFailed         = 1
	exitMayHaveWritten = 2
)

func (f *Fault) Error() string {
	return string(f.Code) + ": " + f.Message
}

func (f *Fault) ExitCode() int {
	if f.Code == WriteUncertain || f.AfterWrite {
		return exitMayHaveWritten
	}
	return exitFailed
}

func (f *Fault) Node() *render.Node {
	return f.document().node()
}

func (f *Fault) document() document {
	return document{Code: f.Code, Message: f.Message, Details: f.Details}
}

func (d document) node() *render.Node {
	pairs := []render.Pair{
		{Key: "code", Value: render.NewString(string(d.Code))},
		{Key: "message", Value: render.NewString(d.Message)},
	}
	return render.NewMap(append(pairs, d.Details...)...)
}
