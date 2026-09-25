package render_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/render"
)

func rendered(t *testing.T, document *render.Node) string {
	t.Helper()
	var out strings.Builder
	require.NoError(t, render.YAML{}.Render(&out, document))
	return out.String()
}

func refused(t *testing.T, document *render.Node) {
	t.Helper()
	var out strings.Builder
	assert.Error(t, render.YAML{}.Render(&out, document))
	assert.Empty(t, out.String())
}

func readBack(t *testing.T, document string) map[string]string {
	t.Helper()
	var read map[string]string
	require.NoError(t, yaml.Unmarshal([]byte(document), &read), "%q", document)
	return read
}

func lines(printed ...string) string {
	return strings.Join(printed, "\n") + "\n"
}

func TestYAMLPrintsEachScalarSoNullEmptyAndEmptyListDiffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value *render.Node
		want  string
	}{
		{name: "string", value: render.NewString("First"), want: lines(`value: "First"`)},
		{name: "empty string", value: render.NewString(""), want: lines(`value: ""`)},
		{name: "string that reads as null", value: render.NewString("null"), want: lines(`value: "null"`)},
		{name: "string that reads as a bool", value: render.NewString("true"), want: lines(`value: "true"`)},
		{name: "string that reads as a number", value: render.NewString("12"), want: lines(`value: "12"`)},
		{name: "string that reads as an empty list", value: render.NewString("[]"), want: lines(`value: "[]"`)},
		{name: "null", value: render.NewNull(), want: lines(`value: null`)},
		{name: "empty list", value: render.NewList(), want: lines(`value: []`)},
		{name: "empty mapping", value: render.NewMap(), want: lines(`value: {}`)},
		{name: "true", value: render.NewBool(true), want: lines(`value: true`)},
		{name: "false", value: render.NewBool(false), want: lines(`value: false`)},
		{name: "number", value: render.NewNumber("-1.5"), want: lines(`value: -1.5`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rendered(t, render.NewMap(render.Pair{Key: "value", Value: tc.value})))
		})
	}
}

func TestYAMLNestsMappingsInBlocksAndWritesEachRecordOnOneLine(t *testing.T) {
	t.Parallel()
	first := render.NewMap(render.Pair{Key: "id", Value: render.NewString("1")}, render.Pair{Key: "name", Value: render.NewString("First")})
	second := render.NewMap(render.Pair{Key: "id", Value: render.NewString("2")}, render.Pair{Key: "name", Value: render.NewString("Second")})
	tests := []struct {
		name     string
		document *render.Node
		want     string
	}{
		{name: "empty document", document: render.NewMap(), want: ""},
		{
			name:     "keys in their order",
			document: render.NewMap(render.Pair{Key: "second", Value: render.NewString("Second")}, render.Pair{Key: "first", Value: render.NewString("First")}),
			want:     lines(`second: "Second"`, `first: "First"`),
		},
		{
			name:     "mapping under a key",
			document: render.NewMap(render.Pair{Key: "owner", Value: first}),
			want:     lines(`owner:`, `  id: "1"`, `  name: "First"`),
		},
		{
			name: "mapping two levels down",
			document: render.NewMap(render.Pair{Key: "outer", Value: render.NewMap(
				render.Pair{Key: "inner", Value: first},
				render.Pair{Key: "after", Value: render.NewNull()},
			)}),
			want: lines(`outer:`, `  inner:`, `    id: "1"`, `    name: "First"`, `  after: null`),
		},
		{
			name:     "list of strings",
			document: render.NewMap(render.Pair{Key: "items", Value: render.NewList(render.NewString("First"), render.NewString("Second"))}),
			want:     lines(`items:`, `  - "First"`, `  - "Second"`),
		},
		{
			name:     "list of scalars of every kind",
			document: render.NewMap(render.Pair{Key: "items", Value: render.NewList(render.NewNull(), render.NewNumber("1"), render.NewBool(true))}),
			want:     lines(`items:`, `  - null`, `  - 1`, `  - true`),
		},
		{
			name:     "list of records",
			document: render.NewMap(render.Pair{Key: "records", Value: render.NewList(first, second)}),
			want:     lines(`records:`, `  - {id: "1", name: "First"}`, `  - {id: "2", name: "Second"}`),
		},
		{
			name: "record holding mappings and lists",
			document: render.NewMap(render.Pair{Key: "records", Value: render.NewList(render.NewMap(
				render.Pair{Key: "owner", Value: first},
				render.Pair{Key: "tags", Value: render.NewList(render.NewString("First"), render.NewString("Second"))},
				render.Pair{Key: "none", Value: render.NewList()},
				render.Pair{Key: "empty", Value: render.NewMap()},
				render.Pair{Key: "gone", Value: render.NewNull()},
			))}),
			want: lines(`records:`, `  - {owner: {id: "1", name: "First"}, tags: ["First", "Second"], none: [], empty: {}, gone: null}`),
		},
		{
			name:     "record with a key from data",
			document: render.NewMap(render.Pair{Key: "records", Value: render.NewList(render.NewMap(render.FromData("First field", render.NewString("First"))))}),
			want:     lines(`records:`, `  - {"First field": "First"}`),
		},
		{
			name:     "list of lists",
			document: render.NewMap(render.Pair{Key: "items", Value: render.NewList(render.NewList(render.NewString("First")), render.NewList())}),
			want:     lines(`items:`, `  - ["First"]`, `  - []`),
		},
		{
			name:     "list under a nested mapping",
			document: render.NewMap(render.Pair{Key: "outer", Value: render.NewMap(render.Pair{Key: "records", Value: render.NewList(first)})}),
			want:     lines(`outer:`, `  records:`, `    - {id: "1", name: "First"}`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rendered(t, tc.document))
		})
	}
}

func TestYAMLRefusesADocumentItCannotPrintBeforeTheFirstByte(t *testing.T) {
	t.Parallel()
	printable := render.Pair{Key: "first", Value: render.NewString("First")}
	tests := []struct {
		name     string
		document *render.Node
	}{
		{name: "no document", document: nil},
		{name: "list as the document", document: render.NewList(render.NewMap(printable))},
		{name: "string as the document", document: render.NewString("First")},
		{name: "text as the document", document: render.NewText("First")},
		{name: "null as the document", document: render.NewNull()},
		{name: "nil value", document: render.NewMap(printable, render.Pair{Key: "second", Value: nil})},
		{name: "nil value in a record", document: render.NewMap(printable, render.Pair{Key: "records", Value: render.NewList(render.NewMap(render.Pair{Key: "second", Value: nil}))})},
		{name: "nil list item", document: render.NewMap(printable, render.Pair{Key: "items", Value: render.NewList(render.NewString("Second"), nil)})},
		{name: "value of no kind", document: render.NewMap(printable, render.Pair{Key: "second", Value: &render.Node{}})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refused(t, tc.document)
		})
	}
}

var errWriteFailed = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWriteFailed
}

func TestYAMLReturnsTheFailureOfTheWriter(t *testing.T) {
	t.Parallel()
	document := render.NewMap(render.Pair{Key: "first", Value: render.NewString("First")})
	assert.ErrorIs(t, render.YAML{}.Render(failingWriter{}, document), errWriteFailed)
}
