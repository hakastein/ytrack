package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
)

const articleListFields = "idReadable,summary"

const (
	listedParent = `{"summary":"Parent","$type":"Article","idReadable":"DEV-A-1"}`
	listedChild  = `{"idReadable":"DEV-A-2","$type":"Article","summary":"Child"}`
)

const (
	printedParentRow = `  - {idReadable: "DEV-A-1", summary: "Parent"}` + "\n"
	printedChildRow  = `  - {idReadable: "DEV-A-2", summary: "Child"}` + "\n"
)

func articleListRequest(address, fields, top, escapedQuery string) string {
	return "GET " + address + "/api/articles?fields=" + fields + "&$top=" + top + "&query=" + escapedQuery
}

func searchingArticles(search, limit string) []url.Values {
	return []url.Values{
		{"fields": {articleListFields}, "$top": {limit}, "query": {search}},
		{"fields": {"id"}, "$top": {"-1"}, "query": {search}},
	}
}

func selecting(t *testing.T, handler http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fake.AssistPath {
			assert.Fail(t, "an article list asked for a markup", "%s %s", r.Method, r.URL)
			http.Error(w, "the language of articles is not marked up", http.StatusInternalServerError)
			return
		}
		handler(w, r)
	})
}

func countedArticles(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			count(w, r)
			return
		}
		fake.JSON(http.StatusOK, records)(w, r)
	}
}

func TestArticleListTakesItsSearchFromTheQueryFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no search at all"},
		{name: "the flag twice", argv: []string{"--query", "a", "--query", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), slices.Concat([]string{"article", "list"}, tc.argv)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleListRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "a limit of zero", flags: []string{"--limit", "0"}},
		{name: "the limit twice", flags: []string{"--limit", "1", "--limit", "2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), slices.Concat([]string{"article", "list", "--query", ""}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleListSendsTheSearchAsWritten(t *testing.T) {
	t.Parallel()
	const search = "  project: DEV  "
	server := selecting(t, fake.JSON(http.StatusOK, "["+listedParent+"]"))

	got := runWith(t, server.Env(), "article", "list", "--query", search)

	want := "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedParentRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {articleListFields}, "$top": {"50"}, "query": {search}}}, server.Queries())
}

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
			server := selecting(t, fake.JSON(http.StatusOK, tc.records))

			got := runWith(t, server.Env(), "article", "list", "--query", "project: DEV")

			assert.Equal(t, outcome{stdout: tc.want}, got)
			for _, record := range articleRecords(t, got) {
				assert.Equal(t, []string{"idReadable", "summary"}, recordKeys(record))
				assert.Equal(t, yaml.FlowStyle, record.Style, "the record stands on more than one line")
			}
		})
	}
}

func articleRecords(t *testing.T, got outcome) []*yaml.Node {
	t.Helper()
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "articles").Content
}

func TestArticleListCountsTheArticlesWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	const found = `[{"id":"177-1","$type":"Article"},{"id":"177-2","$type":"Article"},{"id":"177-3","$type":"Article"}]`
	server := selecting(t, countedArticles("["+listedParent+","+listedChild+"]", fake.JSON(http.StatusOK, found)))

	got := runWith(t, server.Env(), "article", "list", "--query", "project: DEV", "--limit", "2")

	want := "total: 3\nreturned: 2\ntruncated: true\narticles:\n" + printedParentRow + printedChildRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, searchingArticles("project: DEV", "2"), server.Queries())
}

func TestArticleListCountsNothingWhenThePageIsShortOfTheLimit(t *testing.T) {
	t.Parallel()
	server := selecting(t, fake.JSON(http.StatusOK, "["+listedParent+"]"))

	got := runWith(t, server.Env(), "article", "list", "--query", "project: DEV", "--limit", "2")

	want := "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedParentRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, server.Requests(), 1)
}

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
			count: fake.JSON(http.StatusOK, "[]"),
			want: faultDocument{
				code:    "upstream_failed",
				details: []detail{{"total", 0}, {"returned", 1}},
			},
		},
		{
			name:  "a server that failed the count",
			count: fake.JSON(http.StatusInternalServerError, said),
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

			got := runWith(t, server.Env(), "article", "list", "--query", "project: DEV", "--limit", "1")

			want := tc.want
			for i, printed := range want.details {
				if printed.key == "request" {
					want.details[i].value = articleListRequest(server.URL, "id", "-1", "project%3A+DEV")
				}
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.Requests(), 2)
		})
	}
}

func TestArticleListRefusesMoreArticlesThanTheLimit(t *testing.T) {
	t.Parallel()
	server := selecting(t, fake.JSON(http.StatusOK, "["+listedParent+","+listedChild+"]"))

	got := runWith(t, server.Env(), "article", "list", "--query", "project: DEV", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 1)
}

func TestArticleListPassesOnTheServerRefusingASearch(t *testing.T) {
	t.Parallel()
	const said = `{"error":"invalid_query","error_description":"Can't parse search query, please check and update query syntax"}`
	server := selecting(t, fake.JSON(http.StatusBadRequest, said))

	got := runWith(t, server.Env(), "article", "list", "--query", "has: parent")

	want := faultDocument{
		code: "rejected",
		details: []detail{
			{"request", articleListRequest(server.URL, articleListFields, "50", "has%3A+parent")},
			{"upstream_status", 400},
			{"upstream_error", "invalid_query"},
			{"upstream_message", "Can't parse search query, please check and update query syntax"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, documentsOf(t, got.stderr), 1)
}
