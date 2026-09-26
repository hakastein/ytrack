package cli_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const separator = "\n---\n"

type warned struct {
	Code     string   `yaml:"code"`
	Query    string   `yaml:"query"`
	FreeText []string `yaml:"free_text"`
}

func documentsOf(t *testing.T, text string) []*yaml.Node {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(text))
	var documents []*yaml.Node
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return documents
		}
		require.NoError(t, err, "stderr: %q", text)
		require.Len(t, document.Content, 1, "stderr: %q", text)
		documents = append(documents, document.Content[0])
	}
}

func requireWarning(t *testing.T, document *yaml.Node) warned {
	t.Helper()
	require.Equal(t, yaml.MappingNode, document.Kind)
	require.Equal(t, []string{"code", "message", "query", "free_text"}, keysOf(document))
	var found warned
	require.NoError(t, document.Decode(&found))
	assert.NotEmpty(t, document.Content[3].Value, "the warning says nothing")
	return found
}

func requireWarned(t *testing.T, got outcome) warned {
	t.Helper()
	documents := documentsOf(t, got.stderr)
	require.Len(t, documents, 1, "stderr: %q", got.stderr)
	return requireWarning(t, documents[0])
}

func warningOf(query string, parts ...string) warned {
	return warned{Code: "unknown_name", Query: query, FreeText: parts}
}

func marking(t *testing.T, assist, rest http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fake.AssistPath {
			assist(w, r)
			return
		}
		rest(w, r)
	})
}

func TestIssueListPrintsTheIssuesItWarnedAbout(t *testing.T) {
	t.Parallel()
	const query = "one two"
	server := marking(t, fake.JSON(http.StatusOK, fake.Markup(t, query, fake.StyleRange(0, 3, "text"))),
		fake.JSON(http.StatusOK, foundDEV1AndDEV2))

	got := runWith(t, envOf(server), "issue", "list", "--query", query, "--fields", "idReadable")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, printedDEV1AndDEV2, got.stdout)
	assert.Equal(t, warningOf(query, "one"), requireWarned(t, got))
}

func TestIssueListWarnsBeforeItRefusesTheSearchTheServerWouldNotRun(t *testing.T) {
	t.Parallel()
	const query = "field: value word"
	const said = `{"error":"invalid_query","error_description":"refused"}`
	server := marking(t, fake.JSON(http.StatusOK, fake.Markup(t, query, fake.StyleRange(7, 5, "error"), fake.StyleRange(13, 4, "text"))),
		fake.JSON(http.StatusBadRequest, said))

	got := runWith(t, envOf(server), "issue", "list", "--query", query)

	assert.Equal(t, 1, got.code)
	assert.Empty(t, got.stdout)
	documents := documentsOf(t, got.stderr)
	require.Len(t, documents, 2, "stderr: %q", got.stderr)
	assert.Equal(t, warningOf(query, "word"), requireWarning(t, documents[0]))
	assert.Equal(t, "rejected", nodeAt(t, documents[1], "code").Value)
	assert.Equal(t, 1, strings.Count(got.stderr, separator), "stderr: %q", got.stderr)
	assert.False(t, strings.HasPrefix(got.stderr, "---"), "stderr: %q", got.stderr)
	assert.True(t, strings.HasSuffix(got.stderr, "\n"), "stderr: %q", got.stderr)
}
