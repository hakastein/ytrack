package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const (
	listedParent = `{"summary":"Parent","$type":"Article","idReadable":"DEV-A-1"}`
	listedChild  = `{"idReadable":"DEV-A-2","$type":"Article","summary":"Child"}`
)

const printedParentAndChild = "total: 2\nreturned: 2\ntruncated: false\narticles:\n" +
	`  - {idReadable: "DEV-A-1", summary: "Parent"}` + "\n" +
	`  - {idReadable: "DEV-A-2", summary: "Child"}` + "\n"

func TestArticleListPrintsTheArticlesOfTheSearchOrTheParent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		argv   []string
		route  string
		search string
	}{
		{
			name:   "a search",
			argv:   []string{"article", "list", "--query", "  project: DEV  "},
			route:  "GET /api/articles",
			search: "  project: DEV  ",
		},
		{
			name:  "the children of a parent",
			argv:  []string{"article", "list", "--parent", "DEV-A-9"},
			route: "GET /api/articles/DEV-A-9/childArticles",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedParent+","+listedChild+"]"))

			got := runWith(t, envOf(server), tc.argv...)

			assert.Equal(t, outcome{stdout: printedParentAndChild}, got)
			assert.Contains(t, server.Routes(), tc.route)
			assert.Equal(t, tc.search, server.Last(t).URL.Query().Get("query"))
		})
	}
}
