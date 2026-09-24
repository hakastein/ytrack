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
)

// A refusal names the request project show sends, with its fields= expression as it was written.
func showRequest(address, code string) string {
	return "GET " + address + "/api/admin/projects/" + code + "?fields=" + defaultProjectFields
}

// The request a refusal names is the one that was sent: an agent that sends the printed address again reaches
// the same endpoint and asks the same question. user show and user list are where the caller's own text reaches
// the path and the query, so they are where an address unescaped whole stops being the request it names.
func TestARefusalNamesTheRequestThatWasSent(t *testing.T) {
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
			server := serve(t, answer(http.StatusNotFound, `{"error":"Not Found","error_description":"none"}`))

			got := runWith(t, server.env(), tc.argv...)

			found := requireRefusal(t, got)
			require.Equal(t, detail{"request", "GET " + server.url + tc.target}, found.details[0])
			printed, err := url.Parse(server.url + tc.target)
			require.NoError(t, err)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, requests[0].URL.EscapedPath(), printed.EscapedPath())
			assert.Equal(t, requests[0].URL.Query(), printed.Query())
		})
	}
}

func TestProjectShowRefusesByTheStatusOfTheAnswer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		header http.Header
		body   string
		code   string
		// What follows the request.
		details []detail
	}{
		{
			name:   "203 with the project",
			status: http.StatusNonAuthoritativeInfo,
			body:   projectDEV,
			code:   "upstream_failed",
			details: []detail{
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
			details: []detail{
				{"upstream_status", 302},
				{"upstream_body", `{"location":"https://sso.example/login"}`},
			},
		},
		{
			name:   "400",
			status: http.StatusBadRequest,
			body:   `{"error":"bad_request","error_description":"Query string has invalid syntax"}`,
			code:   "rejected",
			details: []detail{
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
			details: []detail{
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
			details: []detail{
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
			// A status ADR-0005 does not name carries its body whole, whatever its shape.
			details: []detail{
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
			details: []detail{
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
			details: []detail{
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
			details: []detail{
				{"upstream_status", 503},
				{"upstream_body", ""},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, func(w http.ResponseWriter, r *http.Request) {
				maps.Copy(w.Header(), tc.header)
				answer(tc.status, tc.body)(w, r)
			})

			got := runWith(t, server.env(), "project", "show", "DEV")

			want := refusal{
				code:    tc.code,
				details: slices.Concat([]detail{{"request", showRequest(server.url, "DEV")}}, tc.details),
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestProjectShowRefusesACodeTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "NOPE")

	want := refusal{
		code: "not_found",
		details: []detail{
			{"request", showRequest(dev.url, "NOPE")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id NOPE not found"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

func TestProjectShowRefusesATokenTheDevInstanceDoesNotKnow(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + bogusToken}, "project", "show", "DEV")

	want := refusal{
		code: "denied",
		details: []detail{
			{"request", showRequest(dev.url, "DEV")},
			{"upstream_status", 401},
			{"upstream_error", "Unauthorized"},
			{"upstream_message", "Invalid token"},
			authFromEnv(),
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assertNoToken(t, got, bogusToken)
	assert.Len(t, dev.requests(), 1)
}

func TestProjectShowRefusesAProjectHiddenFromTheLimitedUser(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "project", "show", "DEV")

	want := refusal{
		code: "not_found",
		details: []detail{
			{"request", showRequest(dev.url, "DEV")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id DEV not found"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
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
			code:        "upstream_lied",
		},
		{
			name:        "200 with no body",
			status:      http.StatusOK,
			contentType: "application/json",
			code:        "upstream_lied",
		},
		{
			name:        "200 unfinished JSON",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"shortName":"DEV","name":"DEVELOP`,
			code:        "upstream_lied",
		},
		{
			name:        "200 JSON with more after it",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{} x`,
			code:        "upstream_lied",
		},
		{
			name:        "200 list",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `[]`,
			code:        "upstream_lied",
		},
		{
			name:        "404 HTML",
			status:      http.StatusNotFound,
			contentType: "text/html",
			body:        "<html><body><h1>404 Not Found</h1></body></html>",
			code:        "upstream_lied",
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
			server := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})

			got := runWith(t, server.env(), "project", "show", "DEV")

			want := refusal{
				code: tc.code,
				details: []detail{
					{"request", showRequest(server.url, "DEV")},
					{"upstream_status", tc.status},
					{"upstream_body", tc.body},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestProjectShowDoesNotFollowARedirect(t *testing.T) {
	t.Parallel()
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		// Followed, the redirect would print DEV as if nothing had happened.
		if r.URL.Path == "/moved" {
			answer(http.StatusOK, projectDEV)(w, r)
			return
		}
		w.Header().Set("Location", "/moved")
		w.WriteHeader(http.StatusFound)
	})

	got := runWith(t, server.env(), "project", "show", "DEV")

	want := refusal{
		code: "upstream_lied",
		details: []detail{
			{"request", showRequest(server.url, "DEV")},
			{"upstream_status", 302},
			{"upstream_body", ""},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

func TestProjectShowRefusesTheWebPageTheDevInstanceServesOutsideTheAPI(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	// Under a path it does not route, YouTrack answers 200 with the page of its web application.
	address := dev.url + "/youtrack"

	got := runWith(t, []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + dev.token}, "project", "show", "DEV")

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	require.Len(t, found.details, 3, "details: %v", found.details)
	assert.Equal(t, []detail{{"request", showRequest(address, "DEV")}, {"upstream_status", 200}}, found.details[:2])
	assert.Equal(t, "upstream_body", found.details[2].key)
	// The page belongs to the instance, so it is held to being HTML rather than to its bytes.
	assert.Contains(t, found.details[2].value, "<html")
	assert.Len(t, dev.requests(), 1)
}

func TestProjectShowRefusesAnAnswerCutShort(t *testing.T) {
	t.Parallel()
	server := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, projectDEV)
	})

	got := runWith(t, server.env(), "project", "show", "DEV")

	want := refusal{
		code: "upstream_failed",
		details: []detail{
			{"request", showRequest(server.url, "DEV")},
			{"upstream_status", 200},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
}

func TestProjectShowRefusesWhenNothingListens(t *testing.T) {
	t.Parallel()
	// Nothing can listen on port 0, while a port a closed server frees can go to a server of another test.
	const nowhere = "http://127.0.0.1:0"

	got := runWith(t, []string{"YTRACK_URL=" + nowhere, "YTRACK_TOKEN=" + token}, "project", "show", "DEV")

	// The message is the kernel's word for the failed connection, and that word differs between kernels.
	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", showRequest(nowhere, "DEV")}}, found.details)
	assert.NotContains(t, got.stderr, token)
}

func TestProjectShowDoesNotOfferHTTP2ToAServerThatSpeaksIt(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var offered [][]string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request reached the server", "%s %s", r.Proto, r.URL)
	}))
	server.EnableHTTP2 = true
	// ytrack trusts no certificate a test can make, so the protocols are read off the handshake, where they are
	// offered before the certificate is refused.
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		offered = append(offered, hello.SupportedProtos)
		return nil, nil
	}}
	// The server logs every handshake a client breaks off.
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)

	got := runWith(t, []string{"YTRACK_URL=" + server.URL, "YTRACK_TOKEN=" + token}, "project", "show", "DEV")

	// The words for a certificate refused differ between the verifiers of operating systems.
	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", showRequest(server.URL, "DEV")}}, found.details)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, offered, 1, "handshakes")
	assert.NotContains(t, offered[0], "h2")
}

func TestProjectShowRefusesWhenNoAnswerArrivesInTime(t *testing.T) {
	t.Parallel()
	server := serve(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	var stdout, stderr bytes.Buffer

	code := cli.Run(ctx, []string{"project", "show", "DEV"}, server.env(), nil, nil, &stdout, &stderr)

	got := outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
	want := refusal{
		code:    "upstream_failed",
		details: []detail{{"request", showRequest(server.url, "DEV")}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
}
