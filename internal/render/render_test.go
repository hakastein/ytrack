package render_test

import (
	"github.com/hakastein/go-youtrack"

	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/render"
)

func rendered(t *testing.T, document *youtrack.Node) string {
	t.Helper()
	var out strings.Builder
	require.NoError(t, render.YAML{}.Render(&out, document))
	return out.String()
}

func refused(t *testing.T, document *youtrack.Node) {
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
		value *youtrack.Node
		want  string
	}{
		{name: "string", value: youtrack.NewString("First"), want: lines(`value: "First"`)},
		{name: "empty string", value: youtrack.NewString(""), want: lines(`value: ""`)},
		{name: "string that reads as null", value: youtrack.NewString("null"), want: lines(`value: "null"`)},
		{name: "string that reads as a bool", value: youtrack.NewString("true"), want: lines(`value: "true"`)},
		{name: "string that reads as a number", value: youtrack.NewString("12"), want: lines(`value: "12"`)},
		{name: "string that reads as an empty list", value: youtrack.NewString("[]"), want: lines(`value: "[]"`)},
		{name: "null", value: youtrack.NewNull(), want: lines(`value: null`)},
		{name: "empty list", value: youtrack.NewList(), want: lines(`value: []`)},
		{name: "empty mapping", value: youtrack.NewMap(), want: lines(`value: {}`)},
		{name: "true", value: youtrack.NewBool(true), want: lines(`value: true`)},
		{name: "false", value: youtrack.NewBool(false), want: lines(`value: false`)},
		{name: "number", value: youtrack.NewNumber("-1.5"), want: lines(`value: -1.5`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rendered(t, youtrack.NewMap(youtrack.Pair{Key: "value", Value: tc.value})))
		})
	}
}

func TestYAMLNestsMappingsInBlocksAndWritesEachRecordOnOneLine(t *testing.T) {
	t.Parallel()
	first := youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString("1")}, youtrack.Pair{Key: "name", Value: youtrack.NewString("First")})
	second := youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString("2")}, youtrack.Pair{Key: "name", Value: youtrack.NewString("Second")})
	tests := []struct {
		name     string
		document *youtrack.Node
		want     string
	}{
		{name: "empty document", document: youtrack.NewMap(), want: ""},
		{
			name:     "keys in their order",
			document: youtrack.NewMap(youtrack.Pair{Key: "second", Value: youtrack.NewString("Second")}, youtrack.Pair{Key: "first", Value: youtrack.NewString("First")}),
			want:     lines(`second: "Second"`, `first: "First"`),
		},
		{
			name:     "mapping under a key",
			document: youtrack.NewMap(youtrack.Pair{Key: "owner", Value: first}),
			want:     lines(`owner:`, `  id: "1"`, `  name: "First"`),
		},
		{
			name: "mapping two levels down",
			document: youtrack.NewMap(youtrack.Pair{Key: "outer", Value: youtrack.NewMap(
				youtrack.Pair{Key: "inner", Value: first},
				youtrack.Pair{Key: "after", Value: youtrack.NewNull()},
			)}),
			want: lines(`outer:`, `  inner:`, `    id: "1"`, `    name: "First"`, `  after: null`),
		},
		{
			name:     "list of strings",
			document: youtrack.NewMap(youtrack.Pair{Key: "items", Value: youtrack.NewList(youtrack.NewString("First"), youtrack.NewString("Second"))}),
			want:     lines(`items:`, `  - "First"`, `  - "Second"`),
		},
		{
			name:     "list of scalars of every kind",
			document: youtrack.NewMap(youtrack.Pair{Key: "items", Value: youtrack.NewList(youtrack.NewNull(), youtrack.NewNumber("1"), youtrack.NewBool(true))}),
			want:     lines(`items:`, `  - null`, `  - 1`, `  - true`),
		},
		{
			name:     "list of records",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(first, second)}),
			want:     lines(`records:`, `  - {id: "1", name: "First"}`, `  - {id: "2", name: "Second"}`),
		},
		{
			name: "record holding mappings and lists",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "owner", Value: first},
				youtrack.Pair{Key: "tags", Value: youtrack.NewList(youtrack.NewString("First"), youtrack.NewString("Second"))},
				youtrack.Pair{Key: "none", Value: youtrack.NewList()},
				youtrack.Pair{Key: "empty", Value: youtrack.NewMap()},
				youtrack.Pair{Key: "gone", Value: youtrack.NewNull()},
			))}),
			want: lines(`records:`, `  - {owner: {id: "1", name: "First"}, tags: ["First", "Second"], none: [], empty: {}, gone: null}`),
		},
		{
			name:     "record with a key from data",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(youtrack.NewMap(youtrack.DataPair("First field", youtrack.NewString("First"))))}),
			want:     lines(`records:`, `  - {"First field": "First"}`),
		},
		{
			name:     "list of lists",
			document: youtrack.NewMap(youtrack.Pair{Key: "items", Value: youtrack.NewList(youtrack.NewList(youtrack.NewString("First")), youtrack.NewList())}),
			want:     lines(`items:`, `  - ["First"]`, `  - []`),
		},
		{
			name:     "list under a nested mapping",
			document: youtrack.NewMap(youtrack.Pair{Key: "outer", Value: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(first)})}),
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
	printable := youtrack.Pair{Key: "first", Value: youtrack.NewString("First")}
	tests := []struct {
		name     string
		document *youtrack.Node
	}{
		{name: "no document", document: nil},
		{name: "list as the document", document: youtrack.NewList(youtrack.NewMap(printable))},
		{name: "string as the document", document: youtrack.NewString("First")},
		{name: "text as the document", document: youtrack.NewText("First")},
		{name: "null as the document", document: youtrack.NewNull()},
		{name: "nil value", document: youtrack.NewMap(printable, youtrack.Pair{Key: "second", Value: nil})},
		{name: "nil value in a record", document: youtrack.NewMap(printable, youtrack.Pair{Key: "records", Value: youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "second", Value: nil}))})},
		{name: "nil list item", document: youtrack.NewMap(printable, youtrack.Pair{Key: "items", Value: youtrack.NewList(youtrack.NewString("Second"), nil)})},
		{name: "value of no kind", document: youtrack.NewMap(printable, youtrack.Pair{Key: "second", Value: &youtrack.Node{}})},
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
	document := youtrack.NewMap(youtrack.Pair{Key: "first", Value: youtrack.NewString("First")})
	assert.ErrorIs(t, render.YAML{}.Render(failingWriter{}, document), errWriteFailed)
}
