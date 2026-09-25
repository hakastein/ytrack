package cli_test

import (
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// pflag names an unknown flag in its error as is, while an unknown command is quoted in
// backticks: only a flag carries raw bytes into the document.
const bait = "--q\" b\\ n\n t\t r\r soh\x01 esc\x1b del\x7f nel\xc2\x85 csi\xc2\x9b ls\xe2\x80\xa8 ps\xe2\x80\xa9 bom\xef\xbb\xbf fffe\xef\xbf\xbe ffff\xef\xbf\xbf Статус 😀"

type outcome struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, argv []string) outcome {
	t.Helper()
	return runWith(t, nil, argv...)
}

func TestRunRefusesAnyCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no command", argv: []string{}},
		{name: "unknown command", argv: []string{"bogus", "show", "DEV-1"}},
		{name: "unknown flag", argv: []string{"project", "list", "--bogus"}},
		{name: "help command", argv: []string{"help"}},
		{name: "help command with a topic", argv: []string{"help", "project"}},
		// cobra insists on a help command, so it is given one that refuses; it is called no-help because a
		// command called help is listed in the help even when it is hidden.
		{name: "the stand-in cobra is given for a help command", argv: []string{"no-help"}},
		{name: "group with no command", argv: []string{"project"}},
		{name: "unknown command of a group", argv: []string{"project", "bogus"}},
		// Run answers the protocol where a shell writes it, first in argv; behind a flag it reaches cobra, and no
		// shell writes such a call. What the protocol answers stands in completion_test.go.
		{name: "completion protocol behind a flag", argv: []string{"--limit=5", "__complete", "issue"}},
		{name: "flag with characters YAML must escape", argv: []string{bait}},
		{name: "flag with invalid UTF-8", argv: []string{"--\xff"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, run(t, tc.argv)))
		})
	}
}

// A decoder takes a BOM as it stands and hides which form a character was printed in, so
// the forms are checked on the bytes of stderr.
func TestRunEscapesTheUnprintableAndLeavesTextRaw(t *testing.T) {
	t.Parallel()
	stderr := run(t, []string{bait}).stderr
	for _, r := range []rune{0xFEFF, 0xFFFE, 0xFFFF} {
		assert.NotContains(t, stderr, string(r), "U+%04X stands raw", r)
	}
	for _, form := range []string{"\\uFEFF", "\\uFFFE", "\\uFFFF", "\\x01", "Статус", "😀"} {
		assert.Contains(t, stderr, form)
	}
}

// Given nil, cobra reads the process's arguments, so the test puts a word there; that is
// also why it does not run in parallel.
func TestRunTakesNilArgvAsEmpty(t *testing.T) {
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	os.Args = []string{args[0], "issue"}
	assert.Equal(t, run(t, []string{}), run(t, nil))
}

func TestRunHelpIsNotACommand(t *testing.T) {
	t.Parallel()
	got := run(t, []string{"--help"})
	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.NotEmpty(t, got.stdout)
}

type faultDocument struct {
	code string
	// The keys after message, in the order printed.
	details []detail
}

// A value quoted in the document reads back as a string, a bare integer as an int, null as nil, a list
// as []any and a mapping as its details in order.
type detail struct {
	key   string
	value any
}

// requireRefusal is a refusal that leaves the instance as it was, which every refusal but a write's is: the
// exit code is 1, and the caller may send the call again once they have fixed what it says.
func requireRefusal(t *testing.T, got outcome) faultDocument {
	t.Helper()
	assert.Equal(t, 1, got.code)
	return requireRefusalDocument(t, got)
}

// requireUncertainty is a refusal the caller cannot answer by sending the call again: the write may have
// happened, or the answer that came back says it did, and the exit code is where that stands.
func requireUncertainty(t *testing.T, got outcome) faultDocument {
	t.Helper()
	assert.Equal(t, 2, got.code)
	return requireRefusalDocument(t, got)
}

// requireRefusalDocument is the document a refusal printed, whatever exit code it came with.
func requireRefusalDocument(t *testing.T, got outcome) faultDocument {
	t.Helper()
	assert.Empty(t, got.stdout)

	mapping := requireMapping(t, "stderr", got.stderr)
	pairs := slices.Collect(slices.Chunk(mapping.Content, 2))
	require.GreaterOrEqual(t, len(pairs), 2, "stderr: %q", got.stderr)
	require.Equal(t, []string{"code", "message"}, []string{pairs[0][0].Value, pairs[1][0].Value})

	code, message := pairs[0][1], pairs[1][1]
	assert.Equal(t, yaml.DoubleQuotedStyle, code.Style)
	assert.Equal(t, yaml.DoubleQuotedStyle, message.Style)
	assert.NotEmpty(t, message.Value)
	found := faultDocument{code: code.Value}
	for _, pair := range pairs[2:] {
		found.details = append(found.details, detail{key: pair[0].Value, value: requireValue(t, pair[1])})
	}
	return found
}

// detailNamed is what the refusal printed under that key, for a scenario that holds one key to something
// while the rest of the document is the server's word and not worth writing out.
func detailNamed(t *testing.T, found faultDocument, key string) any {
	t.Helper()
	for _, printed := range found.details {
		if printed.key == key {
			return printed.value
		}
	}
	require.Fail(t, "the refusal printed no "+key, "%v", found.details)
	return nil
}

func requireValue(t *testing.T, node *yaml.Node) any {
	t.Helper()
	switch {
	case node.Kind == yaml.SequenceNode:
		items := []any{}
		for _, item := range node.Content {
			items = append(items, requireValue(t, item))
		}
		return items
	case node.Kind == yaml.MappingNode:
		pairs := []detail{}
		for pair := range slices.Chunk(node.Content, 2) {
			pairs = append(pairs, detail{key: pair[0].Value, value: requireValue(t, pair[1])})
		}
		return pairs
	case node.Style == yaml.DoubleQuotedStyle, node.Style == yaml.LiteralStyle:
		return node.Value
	case node.ShortTag() == "!!null":
		return nil
	case node.ShortTag() == "!!bool":
		flag, err := strconv.ParseBool(node.Value)
		require.NoError(t, err)
		return flag
	}
	require.Equal(t, "!!int", node.ShortTag(), "%q is neither quoted, a literal block, null, a bool nor an integer", node.Value)
	number, err := strconv.Atoi(node.Value)
	require.NoError(t, err)
	return number
}

// requireMapping is the one document text holds, as the mapping at its root; what names it stands in the
// message of a scenario that printed something else.
func requireMapping(t *testing.T, what, text string) *yaml.Node {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(text))
	var document yaml.Node
	require.NoError(t, decoder.Decode(&document), "%s: %q", what, text)
	require.ErrorIs(t, decoder.Decode(&yaml.Node{}), io.EOF, "%s holds more than one document", what)

	mapping := document.Content[0]
	require.Equal(t, yaml.MappingNode, mapping.Kind, "%s: %q", what, text)
	return mapping
}
