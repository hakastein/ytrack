package render_test

import (
	"github.com/hakastein/go-youtrack"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestYAMLWritesTextAsALiteralBlockThatReadsBackByteForByte(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "one line", text: "First", want: lines(`text: |-`, `  First`)},
		{name: "lines", text: "First\nSecond", want: lines(`text: |-`, `  First`, `  Second`)},
		{name: "final line break", text: "First\n", want: lines(`text: |`, `  First`)},
		{name: "final line breaks", text: "First\n\n", want: lines(`text: |+`, `  First`, ``)},
		{name: "empty", text: "", want: lines(`text: |-`)},
		{name: "only a line break", text: "\n", want: lines(`text: |+`, ``)},
		{name: "only line breaks", text: "\n\n", want: lines(`text: |+`, ``, ``)},
		{name: "empty lines first and inside", text: "\n\nFirst\n\nSecond", want: lines(`text: |-`, ``, ``, `  First`, ``, `  Second`)},
		{name: "first line indented", text: "  First\nSecond", want: lines(`text: |2-`, `    First`, `  Second`)},
		{name: "first line after a tab", text: "\tFirst", want: lines(`text: |2-`, "  \tFirst")},
		{name: "first non-empty line indented", text: "\n  First", want: lines(`text: |2-`, ``, `    First`)},
		{name: "line of spaces first", text: "   \nFirst", want: lines(`text: |2-`, `     `, `  First`)},
		{name: "indented line after the first", text: "First\n  Second", want: lines(`text: |-`, `  First`, `    Second`)},
		{name: "line of spaces last", text: "First\n  ", want: lines(`text: |-`, `  First`, `    `)},
		{name: "spaces at the ends", text: " First ", want: lines(`text: |2-`, `   First `)},
		{name: "tab inside", text: "First\tSecond", want: lines(`text: |-`, "  First\tSecond")},
		{name: "document markers", text: "---\nFirst\n...", want: lines(`text: |-`, `  ---`, `  First`, `  ...`)},
		{name: "characters YAML reads outside a block", text: "# a: b\n- \"c\" 'd' \\ {} [] & * ! | > % @ `", want: lines(`text: |-`, `  # a: b`, "  - \"c\" 'd' \\ {} [] & * ! | > % @ `")},
		{name: "printable text beyond ASCII", text: "Статус\U000000A0\U0000E000\U0000FFFD😀", want: lines(`text: |-`, "  Статус\U000000A0\U0000E000\U0000FFFD😀")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			document := rendered(t, youtrack.NewMap(youtrack.Pair{Key: "text", Value: youtrack.NewText(tc.text)}))
			assert.Equal(t, tc.want, document)
			assert.Equal(t, map[string]string{"text": tc.text}, readBack(t, document))
		})
	}
}

func TestYAMLQuotesTextALiteralBlockCannotHold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "carriage return", text: "First\r\nSecond\r\n", want: lines(`text: "First\r\nSecond\r\n"`)},
		{name: "byte order mark", text: "\U0000FEFFFirst", want: lines("text: \"\\uFEFFFirst\"")},
		{name: "next line", text: "First\U00000085Second", want: lines(`text: "First\NSecond"`)},
		{name: "line separator", text: "First\U00002028Second", want: lines(`text: "First\LSecond"`)},
		{name: "paragraph separator", text: "First\U00002029Second", want: lines(`text: "First\PSecond"`)},
		{name: "C0 control", text: "First\x01", want: lines(`text: "First\x01"`)},
		{name: "delete", text: "First\x7f", want: lines(`text: "First\x7F"`)},
		{name: "C1 control", text: "First\U0000009B", want: lines(`text: "First\x9B"`)},
		{name: "noncharacter U+FFFE", text: "First\U0000FFFE", want: lines("text: \"First\\uFFFE\"")},
		{name: "noncharacter U+FFFF", text: "First\U0000FFFF", want: lines("text: \"First\\uFFFF\"")},
		{name: "one of them among lines", text: "First\nSecond\x01\n", want: lines(`text: "First\nSecond\x01\n"`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			document := rendered(t, youtrack.NewMap(youtrack.Pair{Key: "text", Value: youtrack.NewText(tc.text)}))
			assert.Equal(t, tc.want, document)
			assert.Equal(t, map[string]string{"text": tc.text}, readBack(t, document))
		})
	}
}

func TestYAMLEscapesTheUnprintableInAStringAndLeavesThePrintableRaw(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "double quote", value: `"`, want: lines(`value: "\""`)},
		{name: "backslash", value: `\`, want: lines(`value: "\\"`)},
		{name: "line feed", value: "\n", want: lines(`value: "\n"`)},
		{name: "carriage return", value: "\r", want: lines(`value: "\r"`)},
		{name: "tab", value: "\t", want: lines(`value: "\t"`)},
		{name: "NUL", value: "\x00", want: lines(`value: "\x00"`)},
		{name: "C0 control", value: "\x01", want: lines(`value: "\x01"`)},
		{name: "escape", value: "\x1b", want: lines(`value: "\x1B"`)},
		{name: "last C0 control", value: "\x1f", want: lines(`value: "\x1F"`)},
		{name: "delete", value: "\x7f", want: lines(`value: "\x7F"`)},
		{name: "first C1 control", value: "\U00000080", want: lines(`value: "\x80"`)},
		{name: "next line", value: "\U00000085", want: lines(`value: "\N"`)},
		{name: "C1 control", value: "\U0000009B", want: lines(`value: "\x9B"`)},
		{name: "last C1 control", value: "\U0000009F", want: lines(`value: "\x9F"`)},
		{name: "line separator", value: "\U00002028", want: lines(`value: "\L"`)},
		{name: "paragraph separator", value: "\U00002029", want: lines(`value: "\P"`)},
		{name: "byte order mark", value: "\U0000FEFF", want: lines("value: \"\\uFEFF\"")},
		{name: "noncharacter U+FFFE", value: "\U0000FFFE", want: lines("value: \"\\uFFFE\"")},
		{name: "noncharacter U+FFFF", value: "\U0000FFFF", want: lines("value: \"\\uFFFF\"")},
		{name: "ASCII YAML reads outside quotes", value: "# a: b - 'c' {} [] & * ! | > % @ `", want: lines("value: \"# a: b - 'c' {} [] & * ! | > % @ `\"")},
		{name: "no-break space", value: "\U000000A0", want: lines("value: \"\U000000A0\"")},
		{name: "Cyrillic", value: "Статус", want: lines(`value: "Статус"`)},
		{name: "private use", value: "\U0000E000", want: lines("value: \"\U0000E000\"")},
		{name: "replacement character", value: "\U0000FFFD", want: lines("value: \"\U0000FFFD\"")},
		{name: "beyond the BMP", value: "😀", want: lines(`value: "😀"`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			document := rendered(t, youtrack.NewMap(youtrack.Pair{Key: "value", Value: youtrack.NewString(tc.value)}))
			assert.Equal(t, tc.want, document)
			assert.Equal(t, map[string]string{"value": tc.value}, readBack(t, document))
		})
	}
}

func TestYAMLPrintsInvalidUTF8AsTheReplacementCharacter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value *youtrack.Node
		want  string
	}{
		{name: "string", value: youtrack.NewString("First\xffSecond"), want: lines("value: \"First\U0000FFFDSecond\"")},
		{name: "text", value: youtrack.NewText("First\n\xff"), want: lines("value: \"First\\n\U0000FFFD\"")},
		{name: "cut sequence", value: youtrack.NewString("First\xe2\x80"), want: lines("value: \"First\U0000FFFD\U0000FFFD\"")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rendered(t, youtrack.NewMap(youtrack.Pair{Key: "value", Value: tc.value})))
		})
	}
}

func TestYAMLWritesARecordHoldingTextAsABlock(t *testing.T) {
	t.Parallel()
	first := youtrack.Pair{Key: "id", Value: youtrack.NewString("1")}
	tests := []struct {
		name     string
		document *youtrack.Node
		want     string
	}{
		{
			name:     "text in a record",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(youtrack.NewMap(first, youtrack.Pair{Key: "text", Value: youtrack.NewText("First\nSecond")}))}),
			want:     lines(`records:`, `  - id: "1"`, `    text: |-`, `      First`, `      Second`),
		},
		{
			name: "records with and without text",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(
				youtrack.NewMap(first),
				youtrack.NewMap(youtrack.Pair{Key: "text", Value: youtrack.NewText("First")}, youtrack.Pair{Key: "tags", Value: youtrack.NewList(youtrack.NewString("First"))}),
			)}),
			want: lines(`records:`, `  - {id: "1"}`, `  - text: |-`, `      First`, `    tags:`, `      - "First"`),
		},
		{
			name:     "text in a mapping in a record",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(youtrack.NewMap(first, youtrack.Pair{Key: "body", Value: youtrack.NewMap(youtrack.Pair{Key: "text", Value: youtrack.NewText("First")})}))}),
			want:     lines(`records:`, `  - id: "1"`, `    body:`, `      text: |-`, `        First`),
		},
		{
			name: "text in a record in a record",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(youtrack.NewMap(first, youtrack.Pair{Key: "replies", Value: youtrack.NewList(
				youtrack.NewMap(youtrack.Pair{Key: "text", Value: youtrack.NewText("First")}),
			)}))}),
			want: lines(`records:`, `  - id: "1"`, `    replies:`, `      - text: |-`, `          First`),
		},
		{
			name:     "text a record cannot hold in a block",
			document: youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "text", Value: youtrack.NewText("First\r\n")}))}),
			want:     lines(`records:`, `  - text: "First\r\n"`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rendered(t, tc.document))
		})
	}
}

func TestYAMLIndentsTextInARecordRelativeToTheRecord(t *testing.T) {
	t.Parallel()
	document := rendered(t, youtrack.NewMap(youtrack.Pair{Key: "records", Value: youtrack.NewList(
		youtrack.NewMap(youtrack.Pair{Key: "text", Value: youtrack.NewText("  First\nSecond\n\n")}),
	)}))
	assert.Equal(t, lines(`records:`, `  - text: |2+`, `        First`, `      Second`, ``), document)
	var read map[string][]map[string]string
	require.NoError(t, yaml.Unmarshal([]byte(document), &read))
	assert.Equal(t, map[string][]map[string]string{"records": {{"text": "  First\nSecond\n\n"}}}, read)
}

func TestYAMLRefusesTextOutsideARecord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		document *youtrack.Node
	}{
		{name: "text as a list item", document: youtrack.NewMap(youtrack.Pair{Key: "items", Value: youtrack.NewList(youtrack.NewString("First"), youtrack.NewText("Second"))})},
		{name: "text in a list in a list", document: youtrack.NewMap(youtrack.Pair{Key: "items", Value: youtrack.NewList(youtrack.NewList(youtrack.NewText("First")))})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refused(t, tc.document)
		})
	}
}
