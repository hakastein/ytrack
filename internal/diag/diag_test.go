package diag_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

func lines(printed ...string) string {
	return strings.Join(printed, "\n") + "\n"
}

func TestFaultExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		fault diag.Fault
		want  int
	}{
		{name: "bad usage", fault: diag.Fault{Code: diag.BadUsage}, want: 1},
		{name: "unknown name", fault: diag.Fault{Code: diag.UnknownName}, want: 1},
		{name: "missing required", fault: diag.Fault{Code: diag.MissingRequired}, want: 1},
		{name: "not found", fault: diag.Fault{Code: diag.NotFound}, want: 1},
		{name: "denied", fault: diag.Fault{Code: diag.Denied}, want: 1},
		{name: "rejected", fault: diag.Fault{Code: diag.Rejected}, want: 1},
		{name: "upstream failed", fault: diag.Fault{Code: diag.UpstreamFailed}, want: 1},
		{name: "upstream invalid", fault: diag.Fault{Code: diag.UpstreamInvalid}, want: 1},
		{name: "write uncertain", fault: diag.Fault{Code: diag.WriteUncertain}, want: 2},
		{name: "fault after a write", fault: diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true}, want: 2},
		{name: "write uncertain after a write", fault: diag.Fault{Code: diag.WriteUncertain, AfterWrite: true}, want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.fault.ExitCode())
		})
	}
}

func TestStreamPrintsOneDocumentWithoutASeparator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		print func(*diag.Stream)
		want  string
	}{
		{
			name: "fault",
			print: func(stream *diag.Stream) {
				stream.Fail(&diag.Fault{Code: diag.UpstreamInvalid, Message: "First", AfterWrite: true, Details: []render.Pair{
					{Key: "upstream_status", Value: render.NewNumber("200")},
					{Key: "request", Value: render.NewString("GET /api/issues/DEV-1")},
				}})
			},
			want: lines(`code: "upstream_invalid"`, `message: "First"`, `upstream_status: 200`, `request: "GET /api/issues/DEV-1"`),
		},
		{
			name: "warning",
			print: func(stream *diag.Stream) {
				stream.Warn(&diag.Warning{Code: diag.UnknownName, Message: "First", Details: []render.Pair{{Key: "query", Value: render.NewString("First")}}})
			},
			want: lines(`code: "unknown_name"`, `message: "First"`, `query: "First"`),
		},
		{
			name:  "fault without details",
			print: func(stream *diag.Stream) { stream.Fail(&diag.Fault{Code: diag.BadUsage, Message: "First"}) },
			want:  lines(`code: "bad_usage"`, `message: "First"`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr strings.Builder
			tc.print(diag.NewStream(&stderr, render.YAML{}))
			assert.Equal(t, tc.want, stderr.String())
		})
	}
}

func TestStreamOpensEachLaterDocumentWithASeparator(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	stream := diag.NewStream(&stderr, render.YAML{})
	stream.Warn(&diag.Warning{Code: diag.UnknownName, Message: "First", Details: []render.Pair{{Key: "query", Value: render.NewString("First")}}})
	stream.Warn(&diag.Warning{Code: diag.UnknownName, Message: "Second"})
	stream.Fail(&diag.Fault{Code: diag.UpstreamFailed, Message: "Third"})
	assert.Equal(t, lines(
		`code: "unknown_name"`, `message: "First"`, `query: "First"`,
		`---`,
		`code: "unknown_name"`, `message: "Second"`,
		`---`,
		`code: "upstream_failed"`, `message: "Third"`,
	), stderr.String())
}

// A refused document is written before the refusal, as by a renderer that fails partway through.
type scriptedRenderer struct {
	verdicts []error
}

func (r *scriptedRenderer) Render(w io.Writer, n *render.Node) error {
	if err := (render.YAML{}).Render(w, n); err != nil {
		return err
	}
	if len(r.verdicts) == 0 {
		return nil
	}
	verdict := r.verdicts[0]
	r.verdicts = r.verdicts[1:]
	return verdict
}

var (
	errFirstRefusal  = errors.New("first refusal")
	errSecondRefusal = errors.New("second refusal")
)

func TestStreamPrintsWhatItCanOfADocumentTheRendererRefuses(t *testing.T) {
	t.Parallel()
	fault := &diag.Fault{Code: diag.UpstreamInvalid, Message: "First", Details: []render.Pair{{Key: "upstream_status", Value: render.NewNumber("200")}}}
	tests := []struct {
		name     string
		verdicts []error
		print    func(*diag.Stream)
		want     string
	}{
		{
			name:     "fault the renderer refuses",
			verdicts: []error{errFirstRefusal},
			print:    func(stream *diag.Stream) { stream.Fail(fault) },
			want:     lines(`code: "upstream_invalid"`, `message: "First"`, `render_error: "first refusal"`),
		},
		{
			name:     "fault the renderer refuses in either form",
			verdicts: []error{errFirstRefusal, errSecondRefusal},
			print:    func(stream *diag.Stream) { stream.Fail(fault) },
			want:     lines(`upstream_invalid: First second refusal`),
		},
		{
			name:     "warning the renderer refuses",
			verdicts: []error{errFirstRefusal},
			print: func(stream *diag.Stream) {
				stream.Warn(&diag.Warning{Code: diag.UnknownName, Message: "First", Details: []render.Pair{{Key: "query", Value: render.NewString("First")}}})
			},
			want: lines(`code: "unknown_name"`, `message: "First"`, `render_error: "first refusal"`),
		},
		{
			name:     "fault the renderer refuses after a warning",
			verdicts: []error{nil, errFirstRefusal},
			print: func(stream *diag.Stream) {
				stream.Warn(&diag.Warning{Code: diag.UnknownName, Message: "First"})
				stream.Fail(fault)
			},
			want: lines(`code: "unknown_name"`, `message: "First"`, `---`, `code: "upstream_invalid"`, `message: "First"`, `render_error: "first refusal"`),
		},
		{
			name:     "fault the renderer refuses in either form after a warning",
			verdicts: []error{nil, errFirstRefusal, errSecondRefusal},
			print: func(stream *diag.Stream) {
				stream.Warn(&diag.Warning{Code: diag.UnknownName, Message: "First"})
				stream.Fail(fault)
			},
			want: lines(`code: "unknown_name"`, `message: "First"`, `---`, `upstream_invalid: First second refusal`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr strings.Builder
			tc.print(diag.NewStream(&stderr, &scriptedRenderer{verdicts: tc.verdicts}))
			assert.Equal(t, tc.want, stderr.String())
		})
	}
}
