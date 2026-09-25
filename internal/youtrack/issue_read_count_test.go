package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
)

const (
	issueCountPath   = "/api/issuesGetter/count"
	issueCountTarget = issueCountPath + "?fields=count"
)

func issueCount(count string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, `{"$type":"IssueCountResponse","count":`+count+`}`)
}

func issueCounted(count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == issueCountPath {
			count(w, r)
			return
		}
		fake.JSON(http.StatusOK, `[{"$type":"Issue","idReadable":"DEV-1"}]`)(w, r)
	}
}

func TestListIssuesAsksTheCounterTheSearchOfThePage(t *testing.T) {
	t.Parallel()
	const query = `  field: "exact phrase" \ "  `
	server := fake.Serve(t, fake.Searching(t, issueCounted(issueCount("7"))))

	_, _, fault := searchListing(t, server, query, "idReadable", 1)

	require.Nil(t, fault)
	assert.Equal(t, []string{fake.AssistPath, searchIssuesPath, issueCountPath}, server.Paths())
	counting := server.Last(t)
	assert.Equal(t, http.MethodPost, counting.Method)
	assert.Equal(t, "application/json", counting.Header.Get("Content-Type"))
	assert.Equal(t, "count", counting.URL.Query().Get("fields"))
	assert.Equal(t, searchBody(t, query), counting.Body)
}

func TestListIssuesPrintsTheCountOfTheSearch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		counts    []http.HandlerFunc
		total     *render.Node
		truncated *render.Node
		paths     []string
	}{
		{
			name:      "a count ready at once",
			counts:    []http.HandlerFunc{issueCount("7")},
			total:     render.NewNumber("7"),
			truncated: render.NewBool(true),
			paths:     []string{fake.AssistPath, searchIssuesPath, issueCountPath},
		},
		{
			name:      "a count ready when asked again",
			counts:    []http.HandlerFunc{issueCount("-1"), issueCount("7")},
			total:     render.NewNumber("7"),
			truncated: render.NewBool(true),
			paths:     []string{fake.AssistPath, searchIssuesPath, issueCountPath, issueCountPath},
		},
		{
			name:      "a count not ready when asked again",
			counts:    []http.HandlerFunc{issueCount("-1"), issueCount("-1"), issueCount("7")},
			total:     render.NewNull(),
			truncated: render.NewNull(),
			paths:     []string{fake.AssistPath, searchIssuesPath, issueCountPath, issueCountPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, issueCounted(fake.InTurn(tc.counts...))))

			node, _, fault := searchListing(t, server, "field: value", "idReadable", 1)

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(
				render.Pair{Key: "total", Value: tc.total},
				render.Pair{Key: "returned", Value: render.NewNumber("1")},
				render.Pair{Key: "truncated", Value: tc.truncated},
				render.Pair{Key: "issues", Value: render.NewList(render.NewMap(
					render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
				))},
			), node)
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestListIssuesRefusesACountThatIsNoNumberOfIssues(t *testing.T) {
	t.Parallel()
	shaped := func(answer string) []render.Pair {
		return []render.Pair{
			{Key: "upstream_status", Value: render.NewNumber("200")},
			{Key: "upstream_body", Value: render.NewString(answer)},
		}
	}
	counted := func(count string) string { return `{"$type":"IssueCountResponse","count":` + count + `}` }
	const uncounted = `{"$type":"IssueCountResponse","id":"count"}`
	tests := []struct {
		name         string
		answer       string
		afterRequest []render.Pair
	}{
		{name: "a negative number other than -1", answer: counted("-2"), afterRequest: shaped(counted("-2"))},
		{name: "a fraction", answer: counted("1.5"), afterRequest: shaped(counted("1.5"))},
		{name: "a number in quotes", answer: counted(`"3"`), afterRequest: shaped(counted(`"3"`))},
		{name: "nothing at all", answer: counted("null"), afterRequest: shaped(counted("null"))},
		{
			name:   "no count in the answer",
			answer: uncounted,
			afterRequest: []render.Pair{
				{Key: "fields", Value: render.NewString("count")},
				{Key: "missing", Value: render.NewList(render.NewMap(
					render.Pair{Key: "field", Value: render.NewString("count")},
					render.Pair{Key: "type", Value: render.NewString("IssueCountResponse")},
				))},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, issueCounted(fake.JSON(http.StatusOK, tc.answer))))

			_, _, fault := searchListing(t, server, "field: value", "idReadable", 1)

			request := requestTo(http.MethodPost, server, issueCountTarget)
			want := diag.Fault{Code: diag.UpstreamInvalid, Details: append([]render.Pair{request}, tc.afterRequest...)}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestListIssuesRefusesWhereTheCounterFailsWhenAskedAgain(t *testing.T) {
	t.Parallel()
	failed := fake.JSON(http.StatusInternalServerError, `{"error":"server_error","error_description":"failed"}`)
	server := fake.Serve(t, fake.Searching(t, issueCounted(fake.InTurn(issueCount("-1"), failed))))

	_, _, fault := searchListing(t, server, "field: value", "idReadable", 1)

	assert.Equal(t, diag.Fault{Code: diag.UpstreamFailed, Details: []render.Pair{
		requestTo(http.MethodPost, server, issueCountTarget),
		{Key: "upstream_status", Value: render.NewNumber("500")},
		{Key: "upstream_error", Value: render.NewString("server_error")},
		{Key: "upstream_message", Value: render.NewString("failed")},
	}}, faultOf(t, fault))
	assert.Equal(t, []string{fake.AssistPath, searchIssuesPath, issueCountPath, issueCountPath}, server.Paths())
}
