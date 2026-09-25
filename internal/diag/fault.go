package diag

import "github.com/hakastein/ytrack/internal/render"

// Every document of the stream reads the same way: what to do next, what happened, and then whatever the one
// who raised it gave to follow. A Warning is this shape and nothing besides, so a member added here is printed
// by both.
type document struct {
	Code    Code
	Message string
	Details []render.Pair
}

// A Fault is that document and one thing more, which it carries rather than prints: a warning is about a call
// that goes on, so it has no write behind it to answer for.
type Fault struct {
	Code    Code
	Message string
	Details []render.Pair
	// AfterWrite is a refusal that follows a write the server answered 2xx: whatever the refusal is about, the
	// instance changed, so sending the call again would write a second time.
	AfterWrite bool
}

// Only *Fault is an error, so errors.As into a *Fault finds every Fault.
func (f *Fault) Error() string {
	return string(f.Code) + ": " + f.Message
}

// ExitCode is 1 when the state is what it was and 2 when the instance may have changed:
// a write whose answer never arrived, and any refusal about a write the server carried
// out. Only the second tells the caller whether sending the call again is safe.
func (f *Fault) ExitCode() int {
	if f.Code == WriteUncertain || f.AfterWrite {
		return 2
	}
	return 1
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
