package cli_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	markedSearch        = "State: Opne \xf0\x9f\x98\x80 привет"
	greetingUTF16Start  = 15
	greetingUTF16Length = 6
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
			server := marking(t, fake.JSON(http.StatusOK, tc.marked), notAsked(t))

			got := runWith(t, server.Env(), "issue", "list", "--query", "project: DEV")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", "POST " + server.URL + fake.AssistPath + "?fields=" + markupFields},
					{"fields", markupFields},
					{"missing", []any{tc.missing}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, 0, sentTo(server, issuesPath))
		})
	}
}

func TestIssueListRefusesAMarkupThatDoesNotFitTheSearch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		marked string
		fits   bool
	}{
		{
			name:   "a range that ends where the text ends",
			marked: fake.Markup(t, markedSearch, fake.StyleRange(greetingUTF16Start, greetingUTF16Length, "text")),
			fits:   true,
		},
		{
			name:   "a range that runs past the end",
			marked: fake.Markup(t, markedSearch, fake.StyleRange(greetingUTF16Start+1, greetingUTF16Length, "text")),
		},
		{
			name:   "a range that begins before the start",
			marked: fake.Markup(t, markedSearch, fake.StyleRange(-1, greetingUTF16Length, "text")),
		},
		{
			name:   "a search that came back with a space of its own",
			marked: fake.Markup(t, markedSearch+" ", fake.StyleRange(greetingUTF16Start, greetingUTF16Length, "text")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := marking(t, fake.JSON(http.StatusOK, tc.marked), fake.JSON(http.StatusOK, `[`+listedDEV1()+`]`))

			got := runWith(t, server.Env(), "issue", "list", "--query", markedSearch)

			requireMarkedUpFirst(t, server, markedSearch)
			if tc.fits {
				assert.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				assert.Equal(t, 1, sentTo(server, issuesPath))
				return
			}
			found := requireFault(t, got)
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
			marked: fake.Markup(t, search, `{"$type":"SearchStyleRange","start":"0","length":5,"style":"field-name"}`),
		},
		{
			name:   "how far a range runs on written as a fraction",
			marked: fake.Markup(t, search, `{"$type":"SearchStyleRange","start":0,"length":1.5,"style":"field-name"}`),
		},
		{
			name:   "a range of no style",
			marked: fake.Markup(t, search, `{"$type":"SearchStyleRange","start":0,"length":5,"style":null}`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := marking(t, fake.JSON(http.StatusOK, tc.marked), notAsked(t))

			got := runWith(t, server.Env(), "issue", "list", "--query", search)

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			requireMarkedUpFirst(t, server, search)
			assert.Equal(t, 0, sentTo(server, issuesPath))
		})
	}
}
