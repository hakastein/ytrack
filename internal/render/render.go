package render

import "io"

type Renderer interface {
	Render(io.Writer, *Node) error
}
