package cli_test

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
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
		{name: "an issue id", argv: []string{"article", "delete", "DEV-1"}},
		{name: "an internal id", argv: []string{"article", "delete", "3-19"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleDeleteReadsTheIDAndDeletesByIt(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, articleNamed("DEV-A-7")), deletionDone())

	got := runWith(t, server.Env(), "article", "delete", "dev-A-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-A-7\"\n"}, got)
	requests := server.Requests()
	require.Len(t, requests, 2)
	assert.Equal(t, http.MethodGet, requests[0].Method)
	assert.Equal(t, "/api/articles/dev-A-7", requests[0].URL.Path)
	assert.Equal(t, url.Values{"fields": {deletedFields}}, requests[0].URL.Query())
	assert.Equal(t, http.MethodDelete, requests[1].Method)
	assert.Equal(t, "/api/articles/DEV-A-7", requests[1].URL.Path)
	assert.Empty(t, requests[1].URL.RawQuery, "a deletion asks for no fields")
	assert.Equal(t, "Bearer "+fake.Token, requests[1].Header.Get("Authorization"))
	assert.Equal(t, []string{"", ""}, server.Bodies(), "neither request carries a body")
}

func TestArticleDeleteRefusesAnArticleTheReadDoesNotFind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		said string
	}{
		{name: "an article nobody wrote", said: "Can't find article with id dev-A-7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"error":"Not Found","error_description":` + strconv.Quote(tc.said) + `}`
			server := deleting(t, fake.JSON(http.StatusNotFound, body), noDeletion(t))

			got := runWith(t, server.Env(), "article", "delete", "dev-A-7")

			assert.Equal(t, noArticleToDelete(server.URL, "dev-A-7", tc.said), requireFault(t, got))
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
			server := deleting(t, fake.JSON(http.StatusOK, articleNamed("DEV-A-7")), fake.JSON(tc.status, body))

			got := runWith(t, server.Env(), "article", "delete", "DEV-A-7")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", articleDeletionRequest(server.URL, "DEV-A-7")},
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
			server := deleting(t, fake.JSON(http.StatusOK, articleNamed("DEV-A-7")), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			})

			got := runWith(t, server.Env(), "article", "delete", "DEV-A-7")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleDeletionRequest(server.URL, "DEV-A-7")},
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
			server := deleting(t, fake.JSON(http.StatusOK, body), noDeletion(t))

			got := runWith(t, server.Env(), "article", "delete", "dev-A-7")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleReadRequest(server.URL, "dev-A-7")},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}
