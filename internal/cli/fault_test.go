package cli_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/cli"
	"github.com/hakastein/ytrack/internal/fake"
)

func showRequest(address, code string) string {
	return "GET " + address + "/api/admin/projects/" + code + "?fields=" + defaultProjectFields
}

func TestAFaultNamesTheRequestThatWasSent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		argv   []string
		target string
	}{
		{
			name:   "a login holding a slash",
			argv:   []string{"user", "show", "a/b"},
			target: "/api/users/a%2Fb?fields=login,fullName,email,banned",
		},
		{
			name:   "a login holding a hash",
			argv:   []string{"user", "show", "a#b"},
			target: "/api/users/a%23b?fields=login,fullName,email,banned",
		},
		{
			name:   "a login holding a per cent",
			argv:   []string{"user", "show", "100%"},
			target: "/api/users/100%25?fields=login,fullName,email,banned",
		},
		{
			name: "a search holding a space, a plus, an ampersand and a hash",
			argv: []string{"user", "list", "--query", "Иван & Co = +100%#"},
			target: "/api/users?fields=login,fullName,banned&$top=50&query=" +
				"%D0%98%D0%B2%D0%B0%D0%BD+%26+Co+%3D+%2B100%25%23",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusNotFound, `{"error":"Not Found","error_description":"none"}`))

			got := runWith(t, server.Env(), tc.argv...)

			found := requireFault(t, got)
			require.Equal(t, detail{"request", "GET " + server.URL + tc.target}, found.details[0])
			printed, err := url.Parse(tc.target)
			require.NoError(t, err)
			assert.Equal(t, []string{http.MethodGet}, server.Methods())
			assert.Equal(t, printed.EscapedPath(), server.Request(t, 0).URL.EscapedPath())
			assert.Equal(t, printed.Query(), server.Request(t, 0).URL.Query())
		})
	}
}

func TestProjectShowRefusesByTheStatusOfTheAnswer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		status              int
		header              http.Header
		body                string
		code                string
		detailsAfterRequest []detail
	}{
		{
			name:   "203 with the project",
			status: http.StatusNonAuthoritativeInfo,
			body:   projectDEV,
			code:   "upstream_failed",
			detailsAfterRequest: []detail{
				{"upstream_status", 203},
				{"upstream_body", projectDEV},
			},
		},
		{
			name:   "302 with a JSON body",
			status: http.StatusFound,
			header: http.Header{"Location": {"https://sso.example/login"}},
			body:   `{"location":"https://sso.example/login"}`,
			code:   "upstream_failed",
			detailsAfterRequest: []detail{
				{"upstream_status", 302},
				{"upstream_body", `{"location":"https://sso.example/login"}`},
			},
		},
		{
			name:   "400",
			status: http.StatusBadRequest,
			body:   `{"error":"bad_request","error_description":"Query string has invalid syntax"}`,
			code:   "rejected",
			detailsAfterRequest: []detail{
				{"upstream_status", 400},
				{"upstream_error", "bad_request"},
				{"upstream_message", "Query string has invalid syntax"},
			},
		},
		{
			name:   "403",
			status: http.StatusForbidden,
			body:   `{"error":"Forbidden","error_description":"Access to the project is denied"}`,
			code:   "denied",
			detailsAfterRequest: []detail{
				{"upstream_status", 403},
				{"upstream_error", "Forbidden"},
				{"upstream_message", "Access to the project is denied"},
				authFromEnv(),
			},
		},
		{
			name:   "404 with a member besides error",
			status: http.StatusNotFound,
			body:   `{"error":"Not Found","reason":"project archived"}`,
			code:   "not_found",
			detailsAfterRequest: []detail{
				{"upstream_status", 404},
				{"upstream_error", "Not Found"},
				{"upstream_body", `{"error":"Not Found","reason":"project archived"}`},
			},
		},
		{
			name:   "409 with Retry-After",
			status: http.StatusConflict,
			header: http.Header{"Retry-After": {"1"}},
			body:   `{"error":"Conflict","error_description":"The project was changed"}`,
			code:   "upstream_failed",
			detailsAfterRequest: []detail{
				{"upstream_status", 409},
				{"upstream_error", "Conflict"},
				{"upstream_message", "The project was changed"},
				{"upstream_body", `{"error":"Conflict","error_description":"The project was changed"}`},
			},
		},
		{
			name:   "429 with Retry-After",
			status: http.StatusTooManyRequests,
			header: http.Header{"Retry-After": {"1"}},
			body:   `{"error":"Too Many Requests","error_description":"Slow down"}`,
			code:   "upstream_failed",
			detailsAfterRequest: []detail{
				{"upstream_status", 429},
				{"upstream_error", "Too Many Requests"},
				{"upstream_message", "Slow down"},
				{"upstream_body", `{"error":"Too Many Requests","error_description":"Slow down"}`},
			},
		},
		{
			name:   "500 with a member besides error and error_description",
			status: http.StatusInternalServerError,
			body:   `{"error":"server_error","error_description":"java.lang.NullPointerException","error_developer_message":"at jetbrains.gap"}`,
			code:   "upstream_failed",
			detailsAfterRequest: []detail{
				{"upstream_status", 500},
				{"upstream_error", "server_error"},
				{"upstream_message", "java.lang.NullPointerException"},
				{"upstream_body", `{"error":"server_error","error_description":"java.lang.NullPointerException","error_developer_message":"at jetbrains.gap"}`},
			},
		},
		{
			name:   "503 with no body",
			status: http.StatusServiceUnavailable,
			code:   "upstream_failed",
			detailsAfterRequest: []detail{
				{"upstream_status", 503},
				{"upstream_body", ""},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
				maps.Copy(w.Header(), tc.header)
				fake.JSON(tc.status, tc.body)(w, r)
			})

			got := runWith(t, server.Env(), "project", "show", "DEV")

			want := faultDocument{
				code:    tc.code,
				details: slices.Concat([]detail{{"request", showRequest(server.URL, "DEV")}}, tc.detailsAfterRequest),
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{"/api/admin/projects/DEV"}, server.Paths())
		})
	}
}

func TestEveryCommandRefusesByTheStatusOfItsFirstAnswer(t *testing.T) {
	t.Parallel()
	attached := aFileToAttach(t)
	tests := []struct {
		name string
		argv []string
	}{
		{name: "issue show", argv: []string{"issue", "show", "DEV-1"}},
		{name: "issue list", argv: []string{"issue", "list", "--query", "a"}},
		{name: "issue create", argv: []string{"issue", "create", "DEV", "--summary", "x"}},
		{name: "issue update", argv: []string{"issue", "update", "DEV-1", "--summary", "x"}},
		{name: "issue delete", argv: []string{"issue", "delete", "DEV-1"}},
		{name: "article show", argv: []string{"article", "show", "DEV-A-1"}},
		{name: "article list", argv: []string{"article", "list", "--query", "a"}},
		{name: "article list --parent", argv: []string{"article", "list", "--parent", "DEV-A-1"}},
		{name: "article create", argv: []string{"article", "create", "DEV", "--summary", "x"}},
		{name: "article update", argv: []string{"article", "update", "DEV-A-1", "--summary", "x"}},
		{name: "article delete", argv: []string{"article", "delete", "DEV-A-1"}},
		{name: "comment list", argv: []string{"comment", "list", "DEV-1"}},
		{name: "comment create", argv: []string{"comment", "create", "DEV-1", "--text", "x"}},
		{name: "comment update", argv: []string{"comment", "update", "DEV-1", "7-1", "--text", "x"}},
		{name: "comment delete", argv: []string{"comment", "delete", "DEV-1", "7-1"}},
		{name: "attachment list", argv: []string{"attachment", "list", "DEV-1"}},
		{name: "attachment create", argv: []string{"attachment", "create", "DEV-1", attached}},
		{name: "attachment delete", argv: []string{"attachment", "delete", "DEV-1", "12-1"}},
		{name: "tag list", argv: []string{"tag", "list"}},
		{name: "tag create", argv: []string{"tag", "create", "--name", "x"}},
		{name: "tag create --visible-for", argv: []string{"tag", "create", "--name", "x", "--visible-for", "First"}},
		{name: "tag delete", argv: []string{"tag", "delete", "--name", "x"}},
		{name: "tag add", argv: []string{"tag", "add", "DEV-1", "--name", "x"}},
		{name: "tag remove", argv: []string{"tag", "remove", "DEV-1", "--name", "x"}},
		{name: "link list", argv: []string{"link", "list", "DEV-1"}},
		{name: "link add", argv: []string{"link", "add", "DEV-1", "needs", "DEV-2"}},
		{name: "link remove", argv: []string{"link", "remove", "DEV-1", "needs", "DEV-2"}},
		{name: "time list", argv: []string{"time", "list", "DEV-1"}},
		{name: "time create", argv: []string{"time", "create", "DEV-1", "PT1H"}},
		{name: "time update", argv: []string{"time", "update", "DEV-1", "199-6", "--text", "x"}},
		{name: "time delete", argv: []string{"time", "delete", "DEV-1", "199-6"}},
		{name: "activity list", argv: []string{"activity", "list", "DEV-1"}},
		{name: "field list", argv: []string{"field", "list", "DEV"}},
		{name: "field show", argv: []string{"field", "show", "DEV", "Type"}},
		{name: "project show", argv: []string{"project", "show", "DEV"}},
		{name: "project list", argv: []string{"project", "list"}},
		{name: "user show", argv: []string{"user", "show", "first"}},
		{name: "user list", argv: []string{"user", "list", "--query", "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusForbidden, `{"error":"Forbidden","error_description":"Denied"}`))

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, "denied", requireFault(t, got).code)
			assert.Len(t, server.Requests(), 1)
		})
	}
}

func TestProjectShowRefusesAnAnswerOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		code        string
	}{
		{
			name:        "200 HTML",
			status:      http.StatusOK,
			contentType: "text/html",
			body:        "<!doctype html>\n<html><body>Log in</body></html>",
			code:        "upstream_invalid",
		},
		{
			name:        "200 with no body",
			status:      http.StatusOK,
			contentType: "application/json",
			code:        "upstream_invalid",
		},
		{
			name:        "200 unfinished JSON",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"shortName":"DEV","name":"DEVELOP`,
			code:        "upstream_invalid",
		},
		{
			name:        "200 JSON with more after it",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{} x`,
			code:        "upstream_invalid",
		},
		{
			name:        "200 list",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `[]`,
			code:        "upstream_invalid",
		},
		{
			name:        "404 HTML",
			status:      http.StatusNotFound,
			contentType: "text/html",
			body:        "<html><body><h1>404 Not Found</h1></body></html>",
			code:        "upstream_invalid",
		},
		{
			name:        "500 HTML",
			status:      http.StatusInternalServerError,
			contentType: "text/html",
			body:        "<html><body><h1>500 Internal Server Error</h1></body></html>",
			code:        "upstream_failed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})

			got := runWith(t, server.Env(), "project", "show", "DEV")

			want := faultDocument{
				code: tc.code,
				details: []detail{
					{"request", showRequest(server.URL, "DEV")},
					{"upstream_status", tc.status},
					{"upstream_body", tc.body},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{"/api/admin/projects/DEV"}, server.Paths())
		})
	}
}

func TestProjectShowDoesNotFollowARedirect(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			fake.JSON(http.StatusOK, projectDEV)(w, r)
			return
		}
		w.Header().Set("Location", "/moved")
		w.WriteHeader(http.StatusFound)
	})

	got := runWith(t, server.Env(), "project", "show", "DEV")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", showRequest(server.URL, "DEV")},
			{"upstream_status", 302},
			{"upstream_body", ""},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, server.Paths())
}

func TestProjectShowRefusesAnAnswerCutShort(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, projectDEV)
	})

	got := runWith(t, server.Env(), "project", "show", "DEV")

	want := faultDocument{
		code: "upstream_failed",
		details: []detail{
			{"request", showRequest(server.URL, "DEV")},
			{"upstream_status", 200},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}

func TestProjectShowRefusesWhenNothingListens(t *testing.T) {
	t.Parallel()
	got := runWith(t, fake.Unreachable().Env(), "project", "show", "DEV")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", showRequest(fake.NobodyListens, "DEV")}}, found.details)
	assert.NotContains(t, got.stderr, fake.Token)
}

func TestProjectShowDoesNotOfferHTTP2ToAServerThatSpeaksIt(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var offered [][]string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request reached the server", "%s %s", r.Proto, r.URL)
	}))
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		offered = append(offered, hello.SupportedProtos)
		return nil, nil
	}}
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)

	got := runWith(t, []string{"YTRACK_URL=" + server.URL, "YTRACK_TOKEN=" + fake.Token}, "project", "show", "DEV")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", showRequest(server.URL, "DEV")}}, found.details)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, offered, 1, "handshakes")
	assert.NotContains(t, offered[0], "h2")
}

func TestProjectShowRefusesWhenNoResponseComesInTime(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	var stdout, stderr bytes.Buffer

	code := cli.Run(ctx, []string{"project", "show", "DEV"}, server.Env(), nil, nil, &stdout, &stderr)

	got := outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"request", showRequest(server.URL, "DEV")}},
	}
	assert.Equal(t, want, requireFault(t, got))
}
