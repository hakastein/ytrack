package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// What a record of the list carries unasked, which is what its help names and what goes out where the caller
// writes no expression of their own.
const articleListFields = "idReadable,summary"

// Records of the list under its default expression, with $type and the keys in an order other than the one
// asked for: the server keeps an order of its own.
const (
	listedParent = `{"summary":"Родительская статья","$type":"Article","idReadable":"DEV-A-1"}`
	listedChild  = `{"idReadable":"DEV-A-2","$type":"Article","summary":"Дочерняя статья"}`
)

const (
	printedParentRow = `  - {idReadable: "DEV-A-1", summary: "Родительская статья"}` + "\n"
	printedChildRow  = `  - {idReadable: "DEV-A-2", summary: "Дочерняя статья"}` + "\n"
	printedDemoRow   = `  - {idReadable: "DEMO-A-1", summary: "Начало работы с базой знаний YouTrack"}` + "\n"
)

// A refusal names the request article list sends: the fields= expression reads as written, while the search is
// the text the server received, escaped.
func articleListRequest(address, fields, top, search string) string {
	return "GET " + address + "/api/articles?fields=" + fields + "&$top=" + top + "&query=" + search
}

// searchingArticles is what article list sends for a page of limit articles that it goes on to count: the same
// search goes out both times, so the count is of the articles found rather than of the knowledge base.
func searchingArticles(search, limit string) []url.Values {
	return []url.Values{
		{"fields": {articleListFields}, "$top": {limit}, "query": {search}},
		{"fields": {"id"}, "$top": {"-1"}, "query": {search}},
	}
}

// articleListing is the document article list prints, read back.
type articleListing struct {
	Total     int              `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated bool             `yaml:"truncated"`
	Articles  []map[string]any `yaml:"articles"`
}

func requireArticleListing(t *testing.T, got outcome) articleListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed articleListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Articles, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

// selecting is the server of a selection of articles: it fails the scenario on a request that reaches for a
// markup, since a search of the knowledge base asks for none, and leaves every other request to handler.
func selecting(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == assistPath {
			assert.Fail(t, "a selection of articles asked for a markup", "%s %s", r.Method, r.URL)
			http.Error(w, "the language of articles is not marked up", http.StatusInternalServerError)
			return
		}
		handler(w, r)
	})
}

// countedArticles answers a request for articles with records and the request that counts them, $top=-1, with
// count.
func countedArticles(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			count(w, r)
			return
		}
		respondWith(http.StatusOK, records)(w, r)
	}
}

// The search is the value of one flag: a word of its own is no search, no flag at all is its own refusal,
// and bytes that are no text are refused before the network, since the server answers them with a 500 naming a
// 400 and says nothing of what it read.
func TestArticleListTakesItsSearchFromTheQueryFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a word of its own", argv: []string{"project: DEV"}},
		{name: "two words of their own", argv: []string{"a", "b"}},
		{name: "no search at all"},
		{name: "the flag twice", argv: []string{"--query", "a", "--query", "b"}},
		{name: "a byte that is no UTF-8", argv: []string{"--query", "\xff"}},
		{name: "a truncated sequence inside a search that parses", argv: []string{"--query", "title: \xc3\x28"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"article", "list"}, tc.argv)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// A limit the request cannot carry and a name the expression has no place for are both caught before any
// request: comments hang from one article at a time and no record of a line holds them.
func TestArticleListRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "a limit of zero", flags: []string{"--limit", "0"}},
		{name: "the limit twice", flags: []string{"--limit", "1", "--limit", "2"}},
		{name: "comments added to the default", flags: []string{"--fields", "+comments"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"article", "list", "--query", ""}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The help names what goes out unasked and the flag the search is written with.
func TestArticleListHelpNamesTheDefaultFieldsAndTheQueryFlag(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, articleListFields)
	assert.Contains(t, got.stdout, "--query")
}

// Nothing of the search is read by ytrack and nothing of it is marked up: whatever the caller wrote reaches
// the server as they wrote it, and a query the server cannot parse is its business and not the tool's.
func TestArticleListSendsTheSearchWordForWordAndAsksForNoMarkup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "brackets that open and never close", search: "(((("},
		{name: "a saved search of the language of issues", search: "#Unresolved"},
		{name: "characters a query escapes", search: "title: a&b=c?d#e%20+f"},
		{name: "a tab and a line feed inside", search: "title:\tпервая\nвторая"},
		{name: "a line separator inside", search: "title: первая\xe2\x80\xa8вторая"},
		{name: "an empty search", search: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := selecting(t, respondWith(http.StatusOK, "["+listedParent+"]"))

			got := runWith(t, server.env(), "article", "list", "--query", tc.search)

			want := "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedParentRow
			assert.Equal(t, outcome{stdout: want}, got)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, "/api/articles", requests[0].URL.Path)
			assert.Equal(t, url.Values{"fields": {articleListFields}, "$top": {"50"}, "query": {tc.search}},
				requests[0].URL.Query())
		})
	}
}

// The shape of the document does not depend on how many were found: an empty selection prints the same keys
// as one that found something, with an empty list under the plural.
func TestArticleListPrintsTheSameDocumentHoweverManyWereFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		records string
		want    string
	}{
		{name: "none at all", records: "[]", want: "total: 0\nreturned: 0\ntruncated: false\narticles: []\n"},
		{
			name:    "one of them",
			records: "[" + listedParent + "]",
			want:    "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedParentRow,
		},
		{
			name:    "two of them",
			records: "[" + listedParent + "," + listedChild + "]",
			want:    "total: 2\nreturned: 2\ntruncated: false\narticles:\n" + printedParentRow + printedChildRow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := selecting(t, respondWith(http.StatusOK, tc.records))

			got := runWith(t, server.env(), "article", "list", "--query", "project: DEV")

			assert.Equal(t, outcome{stdout: tc.want}, got)
			for _, record := range articleRecords(t, got) {
				assert.Equal(t, []string{"idReadable", "summary"}, recordKeys(record))
				assert.Equal(t, yaml.FlowStyle, record.Style, "the record stands on more than one line")
			}
		})
	}
}

// articleRecords is the records of a printed selection as they were printed, so a scenario can hold the keys to
// their order and a record to the one line it stands on.
func articleRecords(t *testing.T, got outcome) []*yaml.Node {
	t.Helper()
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "articles").Content
}

// A page that fills the limit proves nothing about the whole, so it is counted by a second pass over ids
// alone, and that pass asks the same search: a count of the knowledge base would answer about something else.
func TestArticleListCountsTheArticlesWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	const found = `[{"id":"177-1","$type":"Article"},{"id":"177-2","$type":"Article"},{"id":"177-3","$type":"Article"}]`
	server := selecting(t, countedArticles("["+listedParent+","+listedChild+"]", respondWith(http.StatusOK, found)))

	got := runWith(t, server.env(), "article", "list", "--query", "project: DEV", "--limit", "2")

	want := "total: 3\nreturned: 2\ntruncated: true\narticles:\n" + printedParentRow + printedChildRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, searchingArticles("project: DEV", "2"), server.sentQueries())
}

// A page shorter than the limit is the whole of what the search found, and nothing is asked twice.
func TestArticleListCountsNothingWhenThePageIsShortOfTheLimit(t *testing.T) {
	t.Parallel()
	server := selecting(t, respondWith(http.StatusOK, "["+listedParent+"]"))

	got := runWith(t, server.env(), "article", "list", "--query", "project: DEV", "--limit", "2")

	want := "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedParentRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, server.requests(), 1)
}

// A count that fails, or that comes back below what already arrived, takes the command with it: half a
// document would say the rest were not cut off.
func TestArticleListRefusesWhenTheCountDoesNotMatch(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	tests := []struct {
		name  string
		count http.HandlerFunc
		want  faultDocument
	}{
		{
			name:  "a count of none over a page of one",
			count: respondWith(http.StatusOK, "[]"),
			want: faultDocument{
				code:    "upstream_failed",
				details: []detail{{"total", 0}, {"returned", 1}},
			},
		},
		{
			name:  "a server that failed the count",
			count: respondWith(http.StatusInternalServerError, said),
			want: faultDocument{
				code: "upstream_failed",
				details: []detail{
					{"request", articleListRequest("", "id", "-1", "project%3A+DEV")},
					{"upstream_status", 500},
					{"upstream_error", "server_error"},
					{"upstream_message", "java.lang.NullPointerException"},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := selecting(t, countedArticles("["+listedParent+"]", tc.count))

			got := runWith(t, server.env(), "article", "list", "--query", "project: DEV", "--limit", "1")

			want := tc.want
			for i, printed := range want.details {
				if printed.key == "request" {
					want.details[i].value = articleListRequest(server.url, "id", "-1", "project%3A+DEV")
				}
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Len(t, server.requests(), 2)
		})
	}
}

// A page longer than the limit means $top went out wrong or the server ignored it, and the count that would
// follow it would be of something else, so the refusal comes before it.
func TestArticleListRefusesMoreArticlesThanTheLimit(t *testing.T) {
	t.Parallel()
	server := selecting(t, respondWith(http.StatusOK, "["+listedParent+","+listedChild+"]"))

	got := runWith(t, server.env(), "article", "list", "--query", "project: DEV", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

// A search the server parses and disagrees with is its refusal word for word, and it is one document: the
// selection warns about nothing of its own, so nothing stands before it on stderr.
func TestArticleListPassesOnTheServerRefusingASearch(t *testing.T) {
	t.Parallel()
	const said = `{"error":"invalid_query","error_description":"Can't parse search query, please check and update query syntax"}`
	server := selecting(t, respondWith(http.StatusBadRequest, said))

	got := runWith(t, server.env(), "article", "list", "--query", "has: parent")

	want := faultDocument{
		code: "rejected",
		details: []detail{
			{"request", articleListRequest(server.url, articleListFields, "50", "has%3A+parent")},
			{"upstream_status", 400},
			{"upstream_error", "invalid_query"},
			{"upstream_message", "Can't parse search query, please check and update query syntax"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, documentsOf(t, got.stderr), 1)
}

// The one article of the demo project, found by the project it stands in: one request, since the page is
// shorter than the limit, and no markup asked for on the way.
func TestArticleListFindsTheArticleOfTheDemoProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "list", "--query", "project: DEMO")

	want := "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedDemoRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/articles"}, dev.sentPaths())
}

func TestArticleListCountsTheArticlesOfTheDevInstanceBeyondTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "list", "--query", "project: DEV", "--limit", "1")

	printed := requireArticleListing(t, got)
	assert.Equal(t, 1, printed.Returned)
	assert.True(t, printed.Truncated)
	assert.GreaterOrEqual(t, printed.Total, 2)
	assert.Equal(t, searchingArticles("project: DEV", "1"), dev.sentQueries())
}

func TestArticleListFindsTheArticleOfTheDevInstanceByTheTitleAttribute(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "list", "--query", "title: родительская")

	want := "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedParentRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.NotContains(t, got.stdout, "DEV-A-2")
}

// The same word under the attribute an issue goes by finds nothing at all, and the server says nothing
// about it: an attribute of the other language is a silent empty list, and this is the fact that settles why
// the selection asks for no markup — the markup would have warned about title and kept quiet here.
func TestArticleListFindsNothingOfTheDevInstanceByAnAttributeOfIssues(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "list", "--query", "summary: родительская")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\narticles: []\n"}, got)
	assert.Equal(t, []string{"/api/articles"}, dev.sentPaths())
}

// A project code the instance has none of is a search it parses and disagrees with, and its word for it
// goes out as it came.
func TestArticleListPassesOnTheDevInstanceRefusingAProjectItDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "list", "--query", "project: NOPE")

	found := requireRefusal(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, "invalid_query", detailNamed(t, found, "upstream_error"))
	assert.Len(t, dev.requests(), 1)
}

func TestArticleListFindsNoArticleOfTheDevInstanceForTheLimitedToken(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"article", "list", "--query", "")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\narticles: []\n"}, got)
	assert.Len(t, dev.requests(), 1)
}
