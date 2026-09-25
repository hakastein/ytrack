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
)

// The separator that heads every document of the stream but the first, on a line of its own in the zero column.
const separator = "\n---\n"

// The warning of a selection, read back: the code and the message every document of the stream opens with, the
// search it is about and the parts of that search the server looks for as text.
type warned struct {
	Code     string   `yaml:"code"`
	Query    string   `yaml:"query"`
	FreeText []string `yaml:"free_text"`
}

// The documents stderr holds, each as the mapping at its root; a scenario says for itself how many it expects.
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

// requireWarning holds a document to the keys of a warning and to the order they are printed in, and reads it
// back: neither a style, an offset nor styleRanges stands among them.
func requireWarning(t *testing.T, document *yaml.Node) warned {
	t.Helper()
	require.Equal(t, yaml.MappingNode, document.Kind)
	require.Equal(t, []string{"code", "message", "query", "free_text"}, recordKeys(document))
	var found warned
	require.NoError(t, document.Decode(&found))
	assert.NotEmpty(t, document.Content[3].Value, "the warning says nothing")
	return found
}

// requireWarned is the one warning a selection printed, and the whole of what it printed.
func requireWarned(t *testing.T, got outcome) warned {
	t.Helper()
	documents := documentsOf(t, got.stderr)
	require.Len(t, documents, 1, "stderr: %q", got.stderr)
	return requireWarning(t, documents[0])
}

// warningOf is the whole of the warning a search is answered with where the server looks for parts of it as text.
func warningOf(query string, parts ...string) warned {
	return warned{Code: "unknown_name", Query: query, FreeText: parts}
}

// A selection whose markup the scenario writes itself, answering the search with one issue.
func markedUp(t *testing.T, marked string) *upstream {
	t.Helper()
	return marking(t, respondWith(http.StatusOK, marked), respondWith(http.StatusOK, `[`+listedDEV1()+`]`))
}

// A part of the search is a chain of the ranges the server styled as text, each beginning where the one before
// it ended: the server marks a name it does not know and the colon after it as two ranges, and what the caller
// wrote is one word. The offsets are counted in units of UTF-16, so the part cut by them is the caller's text
// byte for byte.
func TestIssueListWarnsOfTheTextOfASearch(t *testing.T) {
	t.Parallel()
	// A line separator inside a word: the server leaves it in the token, so one range carries it.
	const wholeToken = "a\xe2\x80\xa8b"
	tests := []struct {
		name   string
		query  string
		ranges []string
		parts  []string
	}{
		{
			name:   "a name the instance has no field for, with the colon after it",
			query:  "нетТакогоПоля: Значение",
			ranges: []string{styled(0, 13, "text"), styled(13, 1, "text"), styled(15, 8, "text")},
			parts:  []string{"нетТакогоПоля:", "Значение"},
		},
		{
			name:   "words of a text search, apart",
			query:  "Задача в работе",
			ranges: []string{styled(0, 6, "text"), styled(7, 1, "text"), styled(9, 6, "text")},
			parts:  []string{"Задача", "в", "работе"},
		},
		{
			name:   "a character outside the basic plane and the word after it",
			query:  markedSearch,
			ranges: []string{styled(12, 2, "text"), styled(15, 6, "text")},
			parts:  []string{"\xf0\x9f\x98\x80", "привет"},
		},
		{
			name:   "one range over a token holding a line separator",
			query:  wholeToken,
			ranges: []string{styled(0, 3, "text")},
			parts:  []string{wholeToken},
		},
		{
			name:  "every style but text",
			query: "State: Opne привет",
			ranges: []string{
				styled(0, 5, "field-name"), styled(7, 4, "field-value"), styled(5, 1, "operator"),
				styled(7, 4, "error"), styled(12, 6, "keyword"),
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
			ranges: []string{styled(9, 0, "text")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := markedUp(t, markup(t, tc.query, tc.ranges...))

			got := runWith(t, server.env(), "issue", "list", "--query", tc.query)

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

// A warning is what a caller is told about a call that went through: the issues found are printed, the exit code
// is the one of a selection that succeeded, and nothing of the markup itself reaches the document.
func TestIssueListPrintsTheIssuesItWarnedAbout(t *testing.T) {
	t.Parallel()
	const query = "Задача в работе"
	server := marking(t, respondWith(http.StatusOK, markup(t, query, styled(0, 6, "text"))),
		respondWith(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", query)

	assert.Equal(t, 0, got.code)
	assert.Equal(t, "total: 2\nreturned: 2\ntruncated: false\nissues:\n"+printedDEV1Row+printedDEV2Row, got.stdout)
	assert.Equal(t, warningOf(query, "Задача"), requireWarned(t, got))
	for _, said := range []string{"styleRanges", "field-name", "start", "length", "style"} {
		assert.NotContains(t, got.stderr, said)
	}
	requireMarkedUpFirst(t, server, query)
}

// The warning is about the search that was sent, so it is printed even where the server then refuses that
// search; two documents of one stream are told apart by a --- on a line of its own, which no single document of
// a refusal is headed by.
func TestIssueListWarnsBeforeItRefusesTheSearchTheServerWouldNotRun(t *testing.T) {
	t.Parallel()
	const query = "State: Opne привет"
	const said = `{"error":"invalid_query","error_description":"Invalid query"}`
	server := marking(t, respondWith(http.StatusOK, markup(t, query, styled(7, 4, "error"), styled(12, 6, "text"))),
		respondWith(http.StatusBadRequest, said))

	got := runWith(t, server.env(), "issue", "list", "--query", query)

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

// The search the warning carries is the one the caller wrote, whatever a reader of YAML would make of the
// characters in it.
func TestIssueListWarnsOfASearchThatReadsBackAsItWasWritten(t *testing.T) {
	t.Parallel()
	const query = "\"a\\b\nc\xe2\x80\xa8d"
	server := markedUp(t, markup(t, query, styled(0, 8, "text")))

	got := runWith(t, server.env(), "issue", "list", "--query", query)

	assert.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, warningOf(query, query), requireWarned(t, got))
	requireMarkedUpFirst(t, server, query)
}

func TestIssueListWarnsOfTheWordTheDevInstanceSearchesForAsText(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	const query = "issue id: DEV-1 работе"

	got := runWith(t, dev.env(), "issue", "list", "--query", query)

	assert.Equal(t, warningOf(query, "работе"), requireWarned(t, got))
	printed := requireListingPrinted(t, got)
	assert.Equal(t, 1, printed.Returned)
	found := records(t, got)
	require.Len(t, found, 1)
	assert.Equal(t, "DEV-1", nodeAt(t, found[0], "idReadable").Value)
	requireMarkedUpFirst(t, dev, query)
}

func TestIssueListWarnsOfANameTheDevInstanceHasNoFieldFor(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	const query = "issue id: DEV-1 нетТакогоПоля: Значение"

	got := runWith(t, dev.env(), "issue", "list", "--query", query)

	assert.Equal(t, warningOf(query, "нетТакогоПоля:", "Значение"), requireWarned(t, got))
	assert.Equal(t, "total: 0\nreturned: 0\ntruncated: false\nissues: []\n", got.stdout)
	assert.Equal(t, 0, got.code)
	requireMarkedUpFirst(t, dev, query)
}

func TestIssueListWarnsOfTheTextOfTheDevInstanceOutsideTheBasicPlane(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	const query = "issue id: DEV-1 \xf0\x9f\x98\x80 привет"

	got := runWith(t, dev.env(), "issue", "list", "--query", query)

	assert.Equal(t, warningOf(query, "\xf0\x9f\x98\x80", "привет"), requireWarned(t, got))
	assert.Equal(t, 0, got.code)
	requireMarkedUpFirst(t, dev, query)
}

func TestIssueListSaysNothingOfASearchTheDevInstanceNeitherMarksUpNorRefuses(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", "((((", "--limit", "1000")

	printed := requireIssueListing(t, got)
	assert.Equal(t, printed.Returned, *printed.Total)
	found := make([]any, 0, len(printed.Issues))
	for _, issue := range printed.Issues {
		found = append(found, issue["idReadable"])
	}
	assert.Subset(t, found, []any{"DEV-1", "DEV-2", "DEV-3", "DEV-4", "DEV-5", "DEV-6"})
	requireMarkedUpFirst(t, dev, "((((")
	assert.Equal(t, 1, sentTo(dev, issuesPath))
}
