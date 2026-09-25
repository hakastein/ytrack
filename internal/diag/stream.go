package diag

import (
	"bytes"
	"fmt"
	"io"

	"github.com/hakastein/ytrack/internal/render"
)

type Stream struct {
	stderr   io.Writer
	renderer render.Renderer
	begun    bool
}

func NewStream(stderr io.Writer, renderer render.Renderer) *Stream {
	return &Stream{stderr: stderr, renderer: renderer}
}

func (s *Stream) Warn(w *Warning) {
	s.print(document(*w))
}

func (s *Stream) Fail(f *Fault) {
	s.print(f.document())
}

func (s *Stream) print(d document) {
	var doc bytes.Buffer
	if err := s.renderer.Render(&doc, d.node()); err != nil {
		doc.Reset()
		unprinted := render.NewMap(
			render.Pair{Key: "code", Value: render.NewString(string(d.Code))},
			render.Pair{Key: "message", Value: render.NewString(d.Message)},
			render.Pair{Key: "render_error", Value: render.NewString(err.Error())},
		)
		if err := s.renderer.Render(&doc, unprinted); err != nil {
			doc.Reset()
			fmt.Fprintln(&doc, string(d.Code)+": "+d.Message, err)
		}
	}
	head := ""
	if s.begun {
		head = "---\n"
	}
	s.begun = true
	_, _ = io.WriteString(s.stderr, head+doc.String())
}
