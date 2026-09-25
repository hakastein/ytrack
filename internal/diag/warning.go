package diag

import "github.com/hakastein/ytrack/internal/render"

type Warning document

func (w *Warning) Node() *render.Node {
	return document(*w).node()
}
