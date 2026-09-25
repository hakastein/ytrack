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

func articleNamed(readable string) string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(readable) + `}`
}

func articleReadRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + deletedFields
}

func articleDeletionRequest(address, readable string) string {
	return "DELETE " + address + "/api/articles/" + readable
}

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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestArticleDeleteHelpOffersNoConfirmation(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "delete", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	for _, flag := range []string{"--yes", "--force", "--confirm"} {
		assert.NotContains(t, got.stdout, flag)
	}
}

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

			assert.Equal(t, noArticleToDelete(server.url, "dev-A-7", tc.said), requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

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
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

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
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

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
	assert.Equal(t, "not_found", requireFaultDocument(t, gone).code)
	assert.Equal(t, []string{http.MethodPost, http.MethodGet, http.MethodDelete, http.MethodGet}, sentMethods(dev))
}

func TestArticleDeleteRefusesAnArticleTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "delete", "DEV-A-99999")

	assert.Equal(t, noArticleToDelete(dev.url, "DEV-A-99999", "Can't find article with id DEV-A-99999"),
		requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}

func TestArticleDeleteSendsNoDeletionForAnArticleTheLimitedUserCannotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"article", "delete", "DEV-A-1")

	assert.Equal(t, noArticleToDelete(dev.url, "DEV-A-1", "Entity with id DEV-A-1 not found"), requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}
