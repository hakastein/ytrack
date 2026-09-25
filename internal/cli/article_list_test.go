package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

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

func TestArticleListRefusesACallWithNoSearch(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "article", "list")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestArticleListSendsTheSearchAsWritten(t *testing.T) {
	t.Parallel()
	const search = "  project: DEV  "
	server := selecting(t, fake.JSON(http.StatusOK, "["+listedParent+"]"))

	got := runWith(t, server.Env(), "article", "list", "--query", search)

	want := "total: 1\nreturned: 1\ntruncated: false\narticles:\n" + printedParentRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
	assert.Equal(t, []string{"/api/articles"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {articleListFields}, "$top": {"50"}, "query": {search}}}, server.Queries())
}
