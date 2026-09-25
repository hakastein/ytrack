package cli_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
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

func proxySignInPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = io.WriteString(w, "<html><body>Sign in</body></html>")
}

func TestIssueListRefusesASearchThatCannotBeMarkedUp(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	tests := []struct {
		name   string
		assist http.HandlerFunc
		code   string
	}{
		{name: "a server that failed", assist: fake.JSON(http.StatusInternalServerError, said), code: "upstream_failed"},
		{
			name:   "a token the server will not take",
			assist: fake.JSON(http.StatusUnauthorized, `{"error":"Unauthorized","error_description":"Not authorized"}`),
			code:   "denied",
		},
		{name: "a page in place of an answer", assist: proxySignInPage, code: "upstream_invalid"},
		{name: "an answer that breaks off", assist: breakOff, code: "upstream_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := marking(t, tc.assist, notAsked(t))

			got := runWith(t, server.Env(), "issue", "list", "--query", "project: DEV")

			assert.Equal(t, tc.code, requireFault(t, got).code)
			requireMarkedUpFirst(t, server, "project: DEV")
			assert.Equal(t, 0, sentTo(server, issuesPath))
		})
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
