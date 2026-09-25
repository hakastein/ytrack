package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

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

func notAsked(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request went out before the search was marked up", "%s %s", r.Method, r.URL)
	}
}

func TestIssueListRefusesAMarkupShortOfWhatItAskedFor(t *testing.T) {
	t.Parallel()
	server := marking(t, fake.JSON(http.StatusOK, `{"$type":"SearchSuggestions","query":"field: value"}`), notAsked(t))

	got := runWith(t, server.Env(), "issue", "list", "--query", "field: value")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "POST " + server.URL + fake.AssistPath + "?fields=" + markupFields},
			{"fields", markupFields},
			{"missing", []any{missingEntry("styleRanges", "SearchSuggestions")}},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{fake.AssistPath}, server.Paths())
}
