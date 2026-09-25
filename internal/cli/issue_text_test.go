package cli_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
)

type textCase struct {
	name   string
	text   string
	quoted bool
}

func (c textCase) style() yaml.Style {
	if c.quoted {
		return yaml.DoubleQuotedStyle
	}
	return yaml.LiteralStyle
}

func textCases() []textCase {
	return []textCase{
		{name: "trailing spaces in a line", text: "первая   \nвторая"},
		{name: "a line of spaces alone", text: "первая\n   \nвторая"},
		{name: "a line of three dashes", text: "первая\n---\nвторая"},
		{name: "a line of three dots", text: "первая\n...\nвторая"},
		{name: "a line of three tildes", text: "первая\n~~~\nвторая"},
		{name: "a character outside the basic plane", text: "смайл \xf0\x9f\x98\x80 в тексте"},
		{name: "a first line beginning with a space", text: " первая\nвторая"},
		{name: "no line ending at the end", text: "первая\nвторая"},
		{name: "an empty first line", text: "\nвторая"},
		{name: "a text of spaces alone", text: "   "},
		{name: "a CRLF", text: "первая\r\nвторая", quoted: true},

		{name: "trailing spaces on the last line", text: "первая\nвторая   "},
		{name: "the text is one line ending", text: "\n"},
		{name: "one line ending at the end", text: "первая\n"},
		{name: "two line endings at the end", text: "первая\n\n"},
		{name: "two empty first lines", text: "\n\nтретья"},
		{name: "an empty line before a line beginning with a space", text: "\n первая"},
		{name: "a tab at the start of a line", text: "первая\n\tвторая"},
		{name: "a tab in the middle of a line", text: "первая\tвторая"},
		{name: "a tab at the end of a line", text: "первая\t\nвторая"},
		{name: "a lone carriage return", text: "первая\rвторая", quoted: true},
		{name: "a NEL", text: "первая\xc2\x85вторая", quoted: true},
		{name: "a line separator", text: "первая\xe2\x80\xa8вторая", quoted: true},
		{name: "a paragraph separator", text: "первая\xe2\x80\xa9вторая", quoted: true},
		{name: "a byte order mark", text: "первая\xef\xbb\xbfвторая", quoted: true},
		{name: "a noncharacter below the byte order mark", text: "первая\xef\xbf\xbeвторая", quoted: true},
		{name: "the last noncharacter of the plane", text: "первая\xef\xbf\xbfвторая", quoted: true},
		{name: "a NUL", text: "первая\x00вторая", quoted: true},
		{name: "a DEL", text: "первая\x7fвторая", quoted: true},
		{name: "a no-break space beside a zero width space", text: "первая\xc2\xa0\xe2\x80\x8bвторая"},

		{name: "a first line of one tab", text: "\t\nвторая"},
		{name: "a first line of spaces before the text", text: "   \nвторая"},
		{name: "a line beginning with a hash", text: "# заголовок\nвторая"},
		{name: "a line beginning with a dash", text: "- пункт\nвторая"},
		{name: "a line beginning with a colon", text: ": значение\nвторая"},
		{name: "three line endings", text: "\n\n\n"},
		{name: "the empty text", text: ""},
	}
}

func TestIssueShowPrintsTextAsALiteralBlockWhereverItCan(t *testing.T) {
	t.Parallel()
	for _, tc := range textCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, issueWith(t, map[string]any{"description": tc.text})))

			got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "idReadable,description")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			printed := nodeAt(t, requireMapping(t, "stdout", got.stdout), "description")
			assert.Equal(t, tc.text, printed.Value, "stdout: %q", got.stdout)
			assert.Equal(t, tc.style(), printed.Style, "stdout: %q", got.stdout)
		})
	}
}

func TestIssueShowPrintsTextUnderANestedKeyAndInsideARecord(t *testing.T) {
	t.Parallel()
	positions := []struct {
		name   string
		fields string
		wrap   func(text string) map[string]any
		path   []string
		record bool
	}{
		{
			name:   "a nested mapping",
			fields: "project(description)",
			wrap: func(text string) map[string]any {
				return map[string]any{"project": map[string]any{"$type": "Project", "description": text}}
			},
			path: []string{"project", "description"},
		},
		{
			name:   "a record of a list",
			fields: "pinnedComments(id,text)",
			wrap: func(text string) map[string]any {
				comment := map[string]any{"$type": "IssueComment", "id": "3-19", "text": text}
				return map[string]any{"pinnedComments": []any{comment}}
			},
			path:   []string{"pinnedComments", "text"},
			record: true,
		},
	}
	for _, position := range positions {
		t.Run(position.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range textCases() {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					server := fake.Serve(t, fake.JSON(http.StatusOK, issueWith(t, position.wrap(tc.text))))

					got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", position.fields)

					require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
					root := requireMapping(t, "stdout", got.stdout)
					printed := nodeAt(t, root, position.path...)
					assert.Equal(t, tc.text, printed.Value, "stdout: %q", got.stdout)
					assert.Equal(t, tc.style(), printed.Style, "stdout: %q", got.stdout)
					if position.record {
						items := nodeAt(t, root, position.path[0])
						require.Equal(t, yaml.SequenceNode, items.Kind)
						assert.Equal(t, yaml.Style(0), items.Content[0].Style, "stdout: %q", got.stdout)
					}
				})
			}
		})
	}
}

func TestIssueShowPrintsADescriptionThatIsNotThereAsNull(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","description":null}`))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "idReadable,description")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\ndescription: null\n"}, got)
}

func issueWith(t *testing.T, keys map[string]any) string {
	t.Helper()
	object := map[string]any{"$type": "Issue", "idReadable": "DEV-1"}
	for key, value := range keys {
		object[key] = value
	}
	encoded, err := json.Marshal(object)
	require.NoError(t, err)
	return string(encoded)
}

func nodeAt(t *testing.T, mapping *yaml.Node, path ...string) *yaml.Node {
	t.Helper()
	node := mapping
	for _, key := range path {
		if node.Kind == yaml.SequenceNode {
			require.Len(t, node.Content, 1)
			node = node.Content[0]
		}
		require.Equal(t, yaml.MappingNode, node.Kind, "no mapping stands where %q was looked for", key)
		found := false
		for pair := range slices.Chunk(node.Content, 2) {
			if pair[0].Value == key {
				node, found = pair[1], true
				break
			}
		}
		require.True(t, found, "no key %q", key)
	}
	return node
}
