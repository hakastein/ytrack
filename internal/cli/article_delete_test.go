package cli_test

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An article as the read before a deletion sees it, with the $type the server names it by.
func articleNamed(readable string) string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(readable) + `}`
}

// The request that read goes out as: the whole article is none of its business, so it asks for the one name the
// deletion is addressed by and printed as.
func articleReadRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + deletedFields
}

// The request the deletion itself goes out as, which is the one a refusal about it names.
func articleDeletionRequest(address, readable string) string {
	return "DELETE " + address + "/api/articles/" + readable
}

// The refusal an article the read does not find becomes: said is the server's own sentence about it, which is
// one sentence for an article nobody wrote and another for one the token may not see.
func noArticleToDelete(address, id, said string) faultDocument {
	return faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleReadRequest(address, id)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", said},
		},
	}
}

// Everything settled before the network: how many ids the command takes, what an id may look like, and that
// no flag stands between the caller and the deletion.
func TestArticleDeleteRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no id", argv: []string{"article", "delete"}},
		{name: "two ids", argv: []string{"article", "delete", "DEV-A-1", "DEV-A-2"}},
		{name: "an issue id", argv: []string{"article", "delete", "DEV-1"}},
		{name: "an internal id", argv: []string{"article", "delete", "3-19"}},
		{name: "--yes", argv: []string{"article", "delete", "DEV-A-1", "--yes"}},
		{name: "--force", argv: []string{"article", "delete", "DEV-A-1", "--force"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// No --yes or --force flag exists: one would become a habit typed before every deletion, defeating the
// confirmation it skips.
func TestArticleDeleteHelpOffersNoConfirmation(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "delete", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	for _, flag := range []string{"--yes", "--force", "--confirm"} {
		assert.NotContains(t, got.stdout, flag)
	}
}

// The whole of the command: the argument is read as the server resolves it, the deletion is addressed by
// the id that came back, and that id is the document.
func TestArticleDeleteReadsTheIDAndDeletesByIt(t *testing.T) {
	t.Parallel()
	server := deleting(t, respondWith(http.StatusOK, articleNamed("DEV-A-7")), deletionDone())

	got := runWith(t, server.env(), "article", "delete", "dev-A-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-A-7\"\n"}, got)
	requests := server.requests()
	require.Len(t, requests, 2)
	assert.Equal(t, http.MethodGet, requests[0].Method)
	assert.Equal(t, "/api/articles/dev-A-7", requests[0].URL.Path)
	assert.Equal(t, url.Values{"fields": {deletedFields}}, requests[0].URL.Query())
	assert.Equal(t, http.MethodDelete, requests[1].Method)
	assert.Equal(t, "/api/articles/DEV-A-7", requests[1].URL.Path)
	assert.Empty(t, requests[1].URL.RawQuery, "a deletion asks for no fields")
	assert.Equal(t, "Bearer "+token, requests[1].Header.Get("Authorization"))
	assert.Equal(t, []string{"", ""}, server.asks(), "neither request carries a body")
}

// An article the read does not find is a refusal and nothing else, and what the server says about it passes
// on word for word: one sentence for an article nobody wrote, another for one hidden from the token, and ytrack
// tells neither from the other.
func TestArticleDeleteRefusesAnArticleTheReadDoesNotFind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		said string
	}{
		{name: "an article nobody wrote", said: "Can't find article with id dev-A-7"},
		{name: "an article the token may not see", said: "Entity with id dev-A-7 not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"error":"Not Found","error_description":` + strconv.Quote(tc.said) + `}`
			server := deleting(t, respondWith(http.StatusNotFound, body), noDeletion(t))

			got := runWith(t, server.env(), "article", "delete", "dev-A-7")

			assert.Equal(t, noArticleToDelete(server.url, "dev-A-7", tc.said), requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// What the server says about the deletion itself passes on with the request it answered, which is the one
// that carried the id out.
func TestArticleDeleteRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		status          int
		upstreamError   string
		upstreamMessage string
		code            string
		details         []detail
	}{
		{
			name:            "the article is gone between the read and the deletion",
			status:          http.StatusNotFound,
			upstreamError:   "Not Found",
			upstreamMessage: "Entity with id DEV-A-7 not found",
			code:            "not_found",
		},
		{
			name:            "the token may read the article and not delete it",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Insufficient rights",
			code:            "denied",
			details:         []detail{authFromEnv()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"error":` + strconv.Quote(tc.upstreamError) + `,"error_description":` + strconv.Quote(tc.upstreamMessage) + `}`
			server := deleting(t, respondWith(http.StatusOK, articleNamed("DEV-A-7")), respondWith(tc.status, body))

			got := runWith(t, server.env(), "article", "delete", "DEV-A-7")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", articleDeletionRequest(server.url, "DEV-A-7")},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

// A deletion is answered with nothing, so a 200 carrying anything at all is something other than the
// endpoint that was asked: a login page, a proxy, or an answer about another call entirely. Whatever answered,
// it answered 2xx to a deletion that went out, so the tree may be gone and the exit code is 2.
func TestArticleDeleteRefusesA200ThatCarriesABody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "a JSON object", contentType: "application/json", body: `{"x":1}`},
		{name: "a web page", contentType: "text/html", body: "<!doctype html>\n<html><body>Log in</body></html>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := deleting(t, respondWith(http.StatusOK, articleNamed("DEV-A-7")), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			})

			got := runWith(t, server.env(), "article", "delete", "DEV-A-7")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleDeletionRequest(server.url, "DEV-A-7")},
					{"upstream_status", 200},
					{"upstream_body", tc.body},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

// The id that came back is sent straight out as a path segment, so it is held to the form ytrack sends
// before it goes: the generated client would resolve ".." against the endpoint and reach /api/, and the id of an
// issue would carry the deletion to an entity of another kind entirely.
func TestArticleDeleteRefusesAReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
	}{
		{name: "two dots", received: `".."`},
		{name: "a path after the id", received: `"DEV-A-7/.."`},
		{name: "the id of an issue", received: `"DEV-1"`},
		{name: "a number", received: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Article","idReadable":` + tc.received + `}`
			server := deleting(t, respondWith(http.StatusOK, body), noDeletion(t))

			got := runWith(t, server.env(), "article", "delete", "dev-A-7")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleReadRequest(server.url, "dev-A-7")},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// The title every article a contract test files goes by: the name of the scenario, so an article left behind
// names the test that left it.
func contractArticleTitle(t *testing.T) string {
	t.Helper()
	return "ytrack contract " + t.Name()
}

func TestArticleDeleteDeletesAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	filed := fileArticle(t, dev, contractArticleTitle(t), "--content", "Статья, заведённая под удаление.")

	got := runWith(t, dev.env(), "article", "delete", filed)

	assert.Equal(t, outcome{stdout: "idReadable: " + strconv.Quote(filed) + "\n"}, got)
	gone := runWith(t, dev.env(), "article", "show", filed, "--comments=0")
	assert.Equal(t, "not_found", requireRefusalDocument(t, gone).code)
	assert.Equal(t, []string{http.MethodPost, http.MethodGet, http.MethodDelete, http.MethodGet}, sentMethods(dev))
}

func TestArticleDeleteRefusesAnArticleTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "delete", "DEV-A-99999")

	assert.Equal(t, noArticleToDelete(dev.url, "DEV-A-99999", "Can't find article with id DEV-A-99999"),
		requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}

// An article hidden from a token is no article at all to it: the read answers the 404 of any missing entity,
// and DEV-A-1 is left where it stands.
func TestArticleDeleteSendsNoDeletionForAnArticleTheLimitedUserCannotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"article", "delete", "DEV-A-1")

	assert.Equal(t, noArticleToDelete(dev.url, "DEV-A-1", "Entity with id DEV-A-1 not found"), requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}
