package cli_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The children of an article are read off a path of their own, which takes no search, so a call that names both
// is refused rather than one of them dropped; and --parent names an article, as it does for article create.
func TestArticleListRefusesAParentItCannotList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a parent and a query", argv: []string{"--parent", "DEV-A-1", "--query", ""}},
		{name: "a parent that is an issue", argv: []string{"--parent", "DEV-1"}},
		{name: "a parent of no form", argv: []string{"--parent", ".."}},
		{name: "the parent twice", argv: []string{"--parent", "DEV-A-1", "--parent", "DEV-A-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"article", "list"}, tc.argv)...)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// A parent the instance has none of, or one the token may not see, is the 404 of the server.
func TestArticleListRefusesAParentTheServerHasNot(t *testing.T) {
	t.Parallel()
	const said = `{"error":"Not Found","error_description":"Entity with id DEV-A-9 not found"}`
	server := serve(t, respondWith(http.StatusNotFound, said))

	got := runWith(t, server.env(), "article", "list", "--parent", "DEV-A-9")

	assert.Equal(t, "not_found", requireRefusal(t, got).code)
	assert.Equal(t, []string{"/api/articles/DEV-A-9/childArticles"}, server.sentPaths())
}
