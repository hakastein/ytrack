package diag

import "github.com/hakastein/ytrack/internal/render"

// A Warning is what a command has to say about a call it goes on with. It travels as no Go error and carries no
// exit code: what it says is true of a command that succeeds, and the code is the command's own.
type Warning document

func (w *Warning) Node() *render.Node {
	return document(*w).node()
}
