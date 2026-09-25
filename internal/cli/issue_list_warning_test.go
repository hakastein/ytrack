package cli_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
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
	require.Equal(t, []string{"code", "message", "query", "free_text"}, recordKeys(document))
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

func markedUp(t *testing.T, marked string) *fake.Server {
	t.Helper()
	return marking(t, fake.JSON(http.StatusOK, marked), fake.JSON(http.StatusOK, `[`+listedDEV1()+`]`))
}

func TestIssueListWarnsOfTheTextOfASearch(t *testing.T) {
	t.Parallel()
	const tokenHoldingLineSeparator = "a\xe2\x80\xa8b"
	tests := []struct {
		name   string
		query  string
		ranges []string
		parts  []string
	}{
		{
			name:   "a name the instance has no field for, with the colon after it",
			query:  "нетТакогоПоля: Значение",
			ranges: []string{fake.StyleRange(0, 13, "text"), fake.StyleRange(13, 1, "text"), fake.StyleRange(15, 8, "text")},
			parts:  []string{"нетТакогоПоля:", "Значение"},
		},
		{
			name:   "words of a text search, apart",
			query:  "Задача в работе",
			ranges: []string{fake.StyleRange(0, 6, "text"), fake.StyleRange(7, 1, "text"), fake.StyleRange(9, 6, "text")},
			parts:  []string{"Задача", "в", "работе"},
		},
		{
			name:   "a character outside the basic plane and the word after it",
			query:  markedSearch,
			ranges: []string{fake.StyleRange(12, 2, "text"), fake.StyleRange(greetingUTF16Start, greetingUTF16Length, "text")},
			parts:  []string{"\xf0\x9f\x98\x80", "привет"},
		},
		{
			name:   "one range over a token holding a line separator",
			query:  tokenHoldingLineSeparator,
			ranges: []string{fake.StyleRange(0, 3, "text")},
			parts:  []string{tokenHoldingLineSeparator},
		},
		{
			name:  "every style but text",
			query: "State: Opne привет",
			ranges: []string{
				fake.StyleRange(0, 5, "field-name"), fake.StyleRange(7, 4, "field-value"), fake.StyleRange(5, 1, "operator"),
				fake.StyleRange(7, 4, "error"), fake.StyleRange(12, 6, "keyword"),
			},
		},
		{
			name:   "no range at all",
			query:  "project: DEV",
			ranges: nil,
		},
		{
			name:   "a range of no length",
			query:  "project: DEV",
			ranges: []string{fake.StyleRange(9, 0, "text")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := markedUp(t, fake.Markup(t, tc.query, tc.ranges...))

			got := runWith(t, server.Env(), "issue", "list", "--query", tc.query)

			assert.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			requireMarkedUpFirst(t, server, tc.query)
			assert.Equal(t, 1, sentTo(server, issuesPath))
			if tc.parts == nil {
				assert.Empty(t, got.stderr)
				return
			}
			assert.Equal(t, warningOf(tc.query, tc.parts...), requireWarned(t, got))
		})
	}
}

func TestIssueListPrintsTheIssuesItWarnedAbout(t *testing.T) {
	t.Parallel()
	const query = "Задача в работе"
	server := marking(t, fake.JSON(http.StatusOK, fake.Markup(t, query, fake.StyleRange(0, 6, "text"))),
		fake.JSON(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`]`))

	got := runWith(t, server.Env(), "issue", "list", "--query", query)

	assert.Equal(t, 0, got.code)
	assert.Equal(t, "total: 2\nreturned: 2\ntruncated: false\nissues:\n"+printedDEV1Row+printedDEV2Row, got.stdout)
	assert.Equal(t, warningOf(query, "Задача"), requireWarned(t, got))
	for _, said := range []string{"styleRanges", "field-name", "start", "length", "style"} {
		assert.NotContains(t, got.stderr, said)
	}
	requireMarkedUpFirst(t, server, query)
}

func TestIssueListWarnsBeforeItRefusesTheSearchTheServerWouldNotRun(t *testing.T) {
	t.Parallel()
	const query = "State: Opne привет"
	const said = `{"error":"invalid_query","error_description":"Invalid query"}`
	server := marking(t, fake.JSON(http.StatusOK, fake.Markup(t, query, fake.StyleRange(7, 4, "error"), fake.StyleRange(12, 6, "text"))),
		fake.JSON(http.StatusBadRequest, said))

	got := runWith(t, server.Env(), "issue", "list", "--query", query)

	assert.Equal(t, 1, got.code)
	assert.Empty(t, got.stdout)
	documents := documentsOf(t, got.stderr)
	require.Len(t, documents, 2, "stderr: %q", got.stderr)
	assert.Equal(t, warningOf(query, "привет"), requireWarning(t, documents[0]))
	assert.Equal(t, "rejected", nodeAt(t, documents[1], "code").Value)
	assert.Equal(t, 1, strings.Count(got.stderr, separator), "stderr: %q", got.stderr)
	assert.False(t, strings.HasPrefix(got.stderr, "---"), "stderr: %q", got.stderr)
	assert.True(t, strings.HasSuffix(got.stderr, "\n"), "stderr: %q", got.stderr)
	requireMarkedUpFirst(t, server, query)
}

func TestIssueListWarnsOfASearchThatReadsBackAsItWasWritten(t *testing.T) {
	t.Parallel()
	const query = "\"a\\b\nc\xe2\x80\xa8d"
	server := markedUp(t, fake.Markup(t, query, fake.StyleRange(0, 8, "text")))

	got := runWith(t, server.Env(), "issue", "list", "--query", query)

	assert.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, warningOf(query, query), requireWarned(t, got))
	requireMarkedUpFirst(t, server, query)
}
