package render

import (
	"io"

	"github.com/hakastein/go-youtrack"
)

type Renderer interface {
	Render(io.Writer, *youtrack.Node) error
}
