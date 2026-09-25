package youtrack_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	searchMarkupFields = "query,styleRanges(start,length,style)"
	searchIssuesPath   = "/api/issues"
)

func searchListing(t *testing.T, server *fake.Server, query, expression string, limit int) (*render.Node, []*diag.Warning, *diag.Fault) {
	t.Helper()
	var warned []*diag.Warning
	call, fault := youtrack.ListIssues(query, &expression, youtrack.Page{Limit: limit}, func(w *diag.Warning) {
		warned = append(warned, w)
	})
	require.Nil(t, fault)
	node, fault := call(t.Context(), client(t, server))
	return node, warned, fault
}

func searchWarnings(t *testing.T, warned []*diag.Warning) []diag.Warning {
	t.Helper()
	var kept []diag.Warning
	for _, w := range warned {
		assert.NotEmpty(t, w.Message)
		prose := *w
		prose.Message = ""
		kept = append(kept, prose)
	}
	return kept
}

func searchMarkedUp(t *testing.T, marked string, rest http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fake.AssistPath {
			fake.JSON(http.StatusOK, marked)(w, r)
			return
		}
		rest(w, r)
	})
}

func searchBody(t *testing.T, query string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)
	return string(body)
}

func searchFreeText(query string, parts ...string) diag.Warning {
	written := make([]*render.Node, 0, len(parts))
	for _, part := range parts {
		written = append(written, render.NewString(part))
	}
	return diag.Warning{Code: diag.UnknownName, Details: []render.Pair{
		{Key: "query", Value: render.NewString(query)},
		{Key: "free_text", Value: render.NewList(written...)},
	}}
}

func TestListIssuesSendsTheSearchAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
	}{
		{name: "spaces around the search", query: "  field: value  "},
		{name: "an empty search", query: ""},
		{name: "brackets and a quote that never close", query: `(((( "open`},
		{name: "characters a query escapes", query: "a&b=c?d#e%20+f"},
		{name: "a tab and a line feed inside", query: "field:\tvalue\nnext"},
		{name: "a line separator inside", query: "a b"},
		{name: "a character outside the basic plane", query: "\U0001F600"},
		{name: "four kilobytes of it", query: strings.Repeat("word ", 820)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[]`)))

			_, _, fault := searchListing(t, server, tc.query, "idReadable", 50)

			require.Nil(t, fault)
			assert.Equal(t, []string{fake.AssistPath, searchIssuesPath}, server.Paths())
			marking := server.Request(t, 0)
			assert.Equal(t, http.MethodPost, marking.Method)
			assert.Equal(t, "application/json", marking.Header.Get("Content-Type"))
			assert.Equal(t, searchMarkupFields, marking.URL.Query().Get("fields"))
			assert.Equal(t, searchBody(t, tc.query), marking.Body)
			assert.Equal(t, []string{tc.query}, server.Request(t, 1).URL.Query()["query"])
		})
	}
}

func TestListIssuesWarnsOfTheFreeTextOfTheSearch(t *testing.T) {
	t.Parallel()
	const pair = "a\U0001F600b"
	const tail = "field: value \U0001F600 word"
	tests := []struct {
		name     string
		query    string
		ranges   []string
		warnings []diag.Warning
	}{
		{
			name:     "ranges that touch, as one part",
			query:    "Unknown: value",
			ranges:   []string{fake.StyleRange(0, 7, "text"), fake.StyleRange(7, 1, "text"), fake.StyleRange(9, 5, "text")},
			warnings: []diag.Warning{searchFreeText("Unknown: value", "Unknown:", "value")},
		},
		{
			name:     "words apart",
			query:    "one two three",
			ranges:   []string{fake.StyleRange(0, 3, "text"), fake.StyleRange(4, 3, "text"), fake.StyleRange(8, 5, "text")},
			warnings: []diag.Warning{searchFreeText("one two three", "one", "two", "three")},
		},
		{
			name:     "a range over both halves of a pair",
			query:    pair,
			ranges:   []string{fake.StyleRange(1, 2, "text")},
			warnings: []diag.Warning{searchFreeText(pair, "\U0001F600")},
		},
		{
			name:     "a range that begins at the second half of a pair",
			query:    pair,
			ranges:   []string{fake.StyleRange(2, 2, "text")},
			warnings: []diag.Warning{searchFreeText(pair, "b")},
		},
		{
			name:     "a range that ends at the first half of a pair",
			query:    pair,
			ranges:   []string{fake.StyleRange(0, 2, "text")},
			warnings: []diag.Warning{searchFreeText(pair, "a")},
		},
		{
			name:     "a range that ends where the search ends",
			query:    tail,
			ranges:   []string{fake.StyleRange(13, 2, "text"), fake.StyleRange(16, 4, "text")},
			warnings: []diag.Warning{searchFreeText(tail, "\U0001F600", "word")},
		},
		{
			name:     "one range over a token holding a line separator",
			query:    "a b",
			ranges:   []string{fake.StyleRange(0, 3, "text")},
			warnings: []diag.Warning{searchFreeText("a b", "a b")},
		},
		{
			name:  "every style but text",
			query: "field: value word",
			ranges: []string{
				fake.StyleRange(0, 5, "field-name"), fake.StyleRange(5, 1, "operator"), fake.StyleRange(7, 5, "field-value"),
				fake.StyleRange(7, 5, "error"), fake.StyleRange(13, 4, "keyword"),
			},
		},
		{name: "no range at all", query: "field: value"},
		{name: "a range of no length", query: "field: value", ranges: []string{fake.StyleRange(5, 0, "text")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searchMarkedUp(t, fake.Markup(t, tc.query, tc.ranges...), fake.JSON(http.StatusOK, `[]`))

			_, warned, fault := searchListing(t, server, tc.query, "idReadable", 50)

			require.Nil(t, fault)
			assert.Equal(t, tc.warnings, searchWarnings(t, warned))
		})
	}
}

func TestListIssuesWarnsOfTheFreeTextOfASearchTheServerThenRefuses(t *testing.T) {
	t.Parallel()
	const query = "field: word"
	const said = `{"error":"invalid_query","error_description":"refused"}`
	server := searchMarkedUp(t, fake.Markup(t, query, fake.StyleRange(7, 4, "text")), fake.JSON(http.StatusBadRequest, said))

	_, warned, fault := searchListing(t, server, query, "idReadable", 50)

	assert.Equal(t, []diag.Warning{searchFreeText(query, "word")}, searchWarnings(t, warned))
	assert.Equal(t, diag.Fault{Code: diag.Rejected, Details: []render.Pair{
		requestTo(http.MethodGet, server, searchIssuesPath+"?query=field%3A+word&fields=idReadable&$top=50"),
		{Key: "upstream_status", Value: render.NewNumber("400")},
		{Key: "upstream_error", Value: render.NewString("invalid_query")},
		{Key: "upstream_message", Value: render.NewString("refused")},
	}}, faultOf(t, fault))
}

func TestListIssuesSearchesNothingWhereTheSearchCannotBeMarkedUp(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusInternalServerError, `{"error":"server_error","error_description":"failed"}`))

	_, _, fault := searchListing(t, server, "field: value", "idReadable", 50)

	assert.Equal(t, diag.Fault{Code: diag.UpstreamFailed, Details: []render.Pair{
		requestTo(http.MethodPost, server, fake.AssistPath+"?fields="+searchMarkupFields),
		{Key: "upstream_status", Value: render.NewNumber("500")},
		{Key: "upstream_error", Value: render.NewString("server_error")},
		{Key: "upstream_message", Value: render.NewString("failed")},
	}}, faultOf(t, fault))
	assert.Equal(t, []string{fake.AssistPath}, server.Paths())
}

func TestListIssuesRefusesAMarkupThatDoesNotFitTheSearch(t *testing.T) {
	t.Parallel()
	const query = "field: value"
	shaped := func(marked string) []render.Pair {
		return []render.Pair{
			{Key: "upstream_status", Value: render.NewNumber("200")},
			{Key: "upstream_body", Value: render.NewString(marked)},
		}
	}
	missing := func(field, schema string) []render.Pair {
		return []render.Pair{
			{Key: "fields", Value: render.NewString(searchMarkupFields)},
			{Key: "missing", Value: render.NewList(render.NewMap(
				render.Pair{Key: "field", Value: render.NewString(field)},
				render.Pair{Key: "type", Value: render.NewString(schema)},
			))},
		}
	}
	echoedWithASpace := `{"$type":"SearchSuggestions","query":"field: value ","styleRanges":[]}`
	oneRange := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` +
		`{"$type":"SearchStyleRange","start":0,"length":5,"style":"field-name"}}`
	nullRange := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":[null]}`
	startAsText := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` +
		`[{"$type":"SearchStyleRange","start":"0","length":5,"style":"field-name"}]}`
	lengthAsFraction := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` +
		`[{"$type":"SearchStyleRange","start":0,"length":1.5,"style":"field-name"}]}`
	styleAsNull := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` +
		`[{"$type":"SearchStyleRange","start":0,"length":5,"style":null}]}`
	pastTheEnd := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` +
		`[{"$type":"SearchStyleRange","start":8,"length":5,"style":"text"}]}`
	beforeTheStart := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` +
		`[{"$type":"SearchStyleRange","start":-1,"length":5,"style":"text"}]}`
	noRanges := `{"$type":"SearchSuggestions","query":"field: value"}`
	noStyle := `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` +
		`[{"$type":"SearchStyleRange","start":0,"length":5}]}`
	tests := []struct {
		name         string
		marked       string
		afterRequest []render.Pair
	}{
		{name: "a search that came back with a space of its own", marked: echoedWithASpace, afterRequest: shaped(echoedWithASpace)},
		{name: "the ranges as one range", marked: oneRange, afterRequest: shaped(oneRange)},
		{name: "a range that is no object", marked: nullRange, afterRequest: shaped(nullRange)},
		{name: "where a range begins written as text", marked: startAsText, afterRequest: shaped(startAsText)},
		{name: "how far a range runs written as a fraction", marked: lengthAsFraction, afterRequest: shaped(lengthAsFraction)},
		{name: "a range of no style", marked: styleAsNull, afterRequest: shaped(styleAsNull)},
		{name: "a range that runs past the end", marked: pastTheEnd, afterRequest: shaped(pastTheEnd)},
		{name: "a range that begins before the start", marked: beforeTheStart, afterRequest: shaped(beforeTheStart)},
		{name: "no ranges at all", marked: noRanges, afterRequest: missing("styleRanges", "SearchSuggestions")},
		{name: "a range with no style", marked: noStyle, afterRequest: missing("styleRanges(style)", "SearchStyleRange")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searchMarkedUp(t, tc.marked, fake.JSON(http.StatusOK, `[]`))

			_, _, fault := searchListing(t, server, query, "idReadable", 50)

			request := requestTo(http.MethodPost, server, fake.AssistPath+"?fields="+searchMarkupFields)
			want := diag.Fault{Code: diag.UpstreamInvalid, Details: append([]render.Pair{request}, tc.afterRequest...)}
			assert.Equal(t, want, faultOf(t, fault))
			assert.Equal(t, []string{fake.AssistPath}, server.Paths())
		})
	}
}
