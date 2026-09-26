package diag_test

import (
	"github.com/hakastein/go-youtrack"

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
		{name: "bad usage", fault: diag.Fault{Code: youtrack.CodeBadUsage}, want: 1},
		{name: "unknown name", fault: diag.Fault{Code: youtrack.CodeUnknownName}, want: 1},
		{name: "missing required", fault: diag.Fault{Code: youtrack.CodeMissingRequired}, want: 1},
		{name: "not found", fault: diag.Fault{Code: youtrack.CodeNotFound}, want: 1},
		{name: "denied", fault: diag.Fault{Code: youtrack.CodeDenied}, want: 1},
		{name: "rejected", fault: diag.Fault{Code: youtrack.CodeRejected}, want: 1},
		{name: "upstream failed", fault: diag.Fault{Code: youtrack.CodeUpstreamFailed}, want: 1},
		{name: "upstream invalid", fault: diag.Fault{Code: youtrack.CodeUpstreamInvalid}, want: 1},
		{name: "write uncertain", fault: diag.Fault{Code: youtrack.CodeWriteUncertain}, want: 2},
		{name: "fault after a write", fault: diag.Fault{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true}, want: 2},
		{name: "write uncertain after a write", fault: diag.Fault{Code: youtrack.CodeWriteUncertain, AfterWrite: true}, want: 2},
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
				stream.Fail(&diag.Fault{Code: youtrack.CodeUpstreamInvalid, Message: "First", AfterWrite: true, Details: []youtrack.Pair{
					{Key: "upstream_status", Value: youtrack.NewNumber("200")},
					{Key: "request", Value: youtrack.NewString("GET /api/issues/DEV-1")},
				}})
			},
			want: lines(`code: "upstream_invalid"`, `message: "First"`, `upstream_status: 200`, `request: "GET /api/issues/DEV-1"`),
		},
		{
			name: "warning",
			print: func(stream *diag.Stream) {
				stream.Warn(&youtrack.Warning{Code: youtrack.CodeUnknownName, Message: "First", Details: []youtrack.Pair{{Key: "query", Value: youtrack.NewString("First")}}})
			},
			want: lines(`code: "unknown_name"`, `message: "First"`, `query: "First"`),
		},
		{
			name:  "fault without details",
			print: func(stream *diag.Stream) { stream.Fail(&diag.Fault{Code: youtrack.CodeBadUsage, Message: "First"}) },
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
	stream.Warn(&youtrack.Warning{Code: youtrack.CodeUnknownName, Message: "First", Details: []youtrack.Pair{{Key: "query", Value: youtrack.NewString("First")}}})
	stream.Warn(&youtrack.Warning{Code: youtrack.CodeUnknownName, Message: "Second"})
	stream.Fail(&diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: "Third"})
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

func (r *scriptedRenderer) Render(w io.Writer, n *youtrack.Node) error {
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
	fault := &diag.Fault{Code: youtrack.CodeUpstreamInvalid, Message: "First", Details: []youtrack.Pair{{Key: "upstream_status", Value: youtrack.NewNumber("200")}}}
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
				stream.Warn(&youtrack.Warning{Code: youtrack.CodeUnknownName, Message: "First", Details: []youtrack.Pair{{Key: "query", Value: youtrack.NewString("First")}}})
			},
			want: lines(`code: "unknown_name"`, `message: "First"`, `render_error: "first refusal"`),
		},
		{
			name:     "fault the renderer refuses after a warning",
			verdicts: []error{nil, errFirstRefusal},
			print: func(stream *diag.Stream) {
				stream.Warn(&youtrack.Warning{Code: youtrack.CodeUnknownName, Message: "First"})
				stream.Fail(fault)
			},
			want: lines(`code: "unknown_name"`, `message: "First"`, `---`, `code: "upstream_invalid"`, `message: "First"`, `render_error: "first refusal"`),
		},
		{
			name:     "fault the renderer refuses in either form after a warning",
			verdicts: []error{nil, errFirstRefusal, errSecondRefusal},
			print: func(stream *diag.Stream) {
				stream.Warn(&youtrack.Warning{Code: youtrack.CodeUnknownName, Message: "First"})
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
