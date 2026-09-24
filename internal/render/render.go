// Package render owns only the bytes of output: every decision about the data is
// taken before a Node is built.
package render

import "io"

type Renderer interface {
	Render(io.Writer, *Node) error
}
