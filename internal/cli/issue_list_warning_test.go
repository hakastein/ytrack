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
	assert.Equal(t, []string{"unknown_name"}, stderrCodes(t, got))
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
	assert.Equal(t, []string{"unknown_name", "rejected"}, stderrCodes(t, got))
}
