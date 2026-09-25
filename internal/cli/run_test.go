package cli_test

import (
	"bytes"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/cli"
)

type outcome struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, argv []string) outcome {
	t.Helper()
	return runWith(t, nil, argv...)
}

func runWith(t *testing.T, env []string, argv ...string) outcome {
	t.Helper()
	return runOn(t, nil, env, argv...)
}

func runOn(t *testing.T, stdin *os.File, env []string, argv ...string) outcome {
	t.Helper()
	return runBuiltFrom(t, nil, stdin, env, argv...)
}

func runBuiltFrom(t *testing.T, build *debug.BuildInfo, stdin *os.File, env []string, argv ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), argv, env, build, stdin, &stdout, &stderr)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestRunRefusesAnyCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no command", argv: []string{}},
		{name: "unknown command", argv: []string{"bogus", "show", "DEV-1"}},
		{name: "help command", argv: []string{"help", "project"}},
		{name: "command with no subcommand", argv: []string{"project"}},
		{name: "unknown subcommand of a command", argv: []string{"project", "bogus"}},
		{name: "completion protocol behind a flag", argv: []string{"--limit=5", "__complete", "issue"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, run(t, tc.argv)))
		})
	}
}

func TestRunTakesNilArgvAsEmpty(t *testing.T) {
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	os.Args = []string{args[0], "--version"}

	assert.Equal(t, run(t, []string{}), run(t, nil))
}

type faultDocument struct {
	code    string
	details []detail
}

type detail struct {
	key   string
	value any
}

func requireFault(t *testing.T, got outcome) faultDocument {
	t.Helper()
	assert.Equal(t, 1, got.code)
	return requireFaultDocument(t, got)
}

func requireUncertainty(t *testing.T, got outcome) faultDocument {
	t.Helper()
	assert.Equal(t, 2, got.code)
	return requireFaultDocument(t, got)
}

func requireFaultDocument(t *testing.T, got outcome) faultDocument {
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
