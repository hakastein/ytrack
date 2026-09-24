package diag

import (
	"bytes"
	"fmt"
	"io"

	"github.com/hakastein/ytrack/internal/render"
)

// Stream is the only writer of stderr, which is how stderr stays one YAML stream.
type Stream struct {
	stderr   io.Writer
	renderer render.Renderer
	// Whether anything has been printed, which is what settles the separator of the next document.
	begun bool
}

func NewStream(stderr io.Writer, renderer render.Renderer) *Stream {
	return &Stream{stderr: stderr, renderer: renderer}
}

// Warn prints what a command has to say about a call it goes on with. It leaves the exit code alone, and a
// command that warns and then refuses prints the warning first: it is about the call that was sent.
func (s *Stream) Warn(w *Warning) {
	s.print(document(*w))
}

func (s *Stream) Refuse(f *Fault) {
	s.print(f.printed())
}

// print still prints a document when the renderer rejects the node: the code, the
// message and the renderer's error. Plain text is left only if those fail too.
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
	// The separator goes out with the document it heads, so a stream never ends on a --- that nothing follows.
	head := ""
	if s.begun {
		head = "---\n"
	}
	s.begun = true
	// A failed write is dropped: stderr is the last channel it could be reported on.
	_, _ = io.WriteString(s.stderr, head+doc.String())
}
