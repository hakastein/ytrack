package cli_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A search of 20 runes and 21 units of UTF-16: 😀 takes two of them, so привет begins at 15 and the text ends
// at 21 rather than at 20.
const markedSearch = "State: Opne \xf0\x9f\x98\x80 привет"

// marking is a server whose markup the scenario writes itself; everything the selection asks afterwards goes
// to rest.
func marking(t *testing.T, assist, rest http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == assistPath {
			assist(w, r)
			return
		}
		rest(w, r)
	})
}

// notAsked stands for the requests a selection whose markup failed never gets to send.
func notAsked(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request went out before the search was marked up", "%s %s", r.Method, r.URL)
	}
}

// A sign-in page under a 200, which is what a proxy in front of YouTrack answers with.
func signInPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = io.WriteString(w, "<html><body>Sign in</body></html>")
}

// A selection is never printed over a search nothing was said about, so a markup that fails takes the command
// with it and the search itself is never run.
func TestIssueListRefusesASearchThatCannotBeMarkedUp(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	tests := []struct {
		name   string
		assist http.HandlerFunc
		code   string
	}{
		{name: "a server that failed", assist: respondWith(http.StatusInternalServerError, said), code: "upstream_failed"},
		{
			name:   "a token the server will not take",
			assist: respondWith(http.StatusUnauthorized, `{"error":"Unauthorized","error_description":"Not authorized"}`),
			code:   "denied",
		},
		{name: "a page in place of an answer", assist: signInPage, code: "upstream_invalid"},
		{name: "an answer that breaks off", assist: breakOff, code: "upstream_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := marking(t, tc.assist, notAsked(t))

			got := runWith(t, server.env(), "issue", "list", "--query", "project: DEV")

			assert.Equal(t, tc.code, requireRefusal(t, got).code)
			requireMarkedUpFirst(t, server, "project: DEV")
			assert.Equal(t, 0, sentTo(server, issuesPath))
		})
	}
}

// styleRanges and the members of a range are names of ytrack's own, which the specification declares nowhere,
// so an answer that lacks one is the server falling short of the request and never a name for the caller to
// fix.
func TestIssueListRefusesAMarkupShortOfWhatItAskedFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		marked  string
		missing []detail
	}{
		{
			name:    "no styled ranges at all",
			marked:  `{"$type":"SearchSuggestions","query":"project: DEV"}`,
			missing: missingEntry("styleRanges", "SearchSuggestions"),
		},
		{
			name: "a range with no style",
			marked: `{"$type":"SearchSuggestions","query":"project: DEV","styleRanges":` +
				`[{"$type":"SearchStyleRange","start":0,"length":8}]}`,
			missing: missingEntry("styleRanges(style)", "SearchStyleRange"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := marking(t, respondWith(http.StatusOK, tc.marked), notAsked(t))

			got := runWith(t, server.env(), "issue", "list", "--query", "project: DEV")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", "POST " + server.url + assistPath + "?fields=" + markupFields},
					{"fields", markupFields},
					{"missing", []any{tc.missing}},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, 0, sentTo(server, issuesPath))
		})
	}
}

// The offsets of a markup are counted in units of UTF-16 and point into the text the server read back, so a
// range outside that text, or a text other than the one that was sent, leaves the markup saying nothing about
// the caller's search.
func TestIssueListRefusesAMarkupThatDoesNotFitTheSearch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		marked string
		fits   bool
	}{
		{name: "a range that ends where the text ends", marked: markup(t, markedSearch, styled(15, 6, "text")), fits: true},
		{name: "a range that runs past the end", marked: markup(t, markedSearch, styled(16, 6, "text"))},
		{name: "a range that begins before the start", marked: markup(t, markedSearch, styled(-1, 6, "text"))},
		{
			name:   "a search that came back with a space of its own",
			marked: markup(t, markedSearch+" ", styled(15, 6, "text")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := marking(t, respondWith(http.StatusOK, tc.marked), respondWith(http.StatusOK, `[`+listedDEV1()+`]`))

			got := runWith(t, server.env(), "issue", "list", "--query", markedSearch)

			requireMarkedUpFirst(t, server, markedSearch)
			if tc.fits {
				assert.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				assert.Equal(t, 1, sentTo(server, issuesPath))
				return
			}
			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, 0, sentTo(server, issuesPath))
		})
	}
}

func TestIssueListRefusesAMarkupOfAShapeItCannotRead(t *testing.T) {
	t.Parallel()
	const search = "State: Opne"
	tests := []struct {
		name   string
		marked string
	}{
		{
			name: "the styled ranges as one range rather than a list of them",
			marked: `{"$type":"SearchSuggestions","query":"State: Opne","styleRanges":` +
				`{"$type":"SearchStyleRange","start":0,"length":5,"style":"field-name"}}`,
		},
		{name: "a range that is no object at all", marked: `{"$type":"SearchSuggestions","query":"State: Opne","styleRanges":[null]}`},
		{
			name:   "where a range begins written as text",
			marked: markup(t, search, `{"$type":"SearchStyleRange","start":"0","length":5,"style":"field-name"}`),
		},
		{
			name:   "how far a range runs on written as a fraction",
			marked: markup(t, search, `{"$type":"SearchStyleRange","start":0,"length":1.5,"style":"field-name"}`),
		},
		{
			name:   "a range of no style",
			marked: markup(t, search, `{"$type":"SearchStyleRange","start":0,"length":5,"style":null}`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := marking(t, respondWith(http.StatusOK, tc.marked), notAsked(t))

			got := runWith(t, server.env(), "issue", "list", "--query", search)

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			requireMarkedUpFirst(t, server, search)
			assert.Equal(t, 0, sentTo(server, issuesPath))
		})
	}
}
