package cli_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A token no instance ever issued: a scenario hands it out to be refused, and no document and no cassette may
// carry it back.
const bogusToken = "perm-bogus"

// meRequest is the request auth status sends, as a refusal names it.
func meRequest(address string) string {
	return "GET " + address + "/api/users/me?fields=login,fullName"
}

// The server that refused the token knows nothing of the places ytrack looked, and a caller with a record per
// directory has a token for each of them, so every command with a network says which one went out.
func TestNoCommandNamesWhereTheTokenTheServerRefusedCameFrom(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name            string
		status          int
		upstreamError   string
		upstreamMessage string
		where           func(t *testing.T, address string) (env []string, from detail)
	}{
		{
			name:            "a token of the environment the server does not know",
			status:          http.StatusUnauthorized,
			upstreamError:   "Unauthorized",
			upstreamMessage: "Invalid token",
			where: func(_ *testing.T, address string) ([]string, detail) {
				return []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + bogusToken}, authFromEnv()
			},
		},
		{
			name:            "a token of the environment the server does not let in",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Access to the project is denied",
			where: func(_ *testing.T, address string) ([]string, detail) {
				return []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + bogusToken}, authFromEnv()
			},
		},
		{
			name:            "a token of the global record",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Access to the project is denied",
			where: func(t *testing.T, address string) ([]string, detail) {
				home, _ := homeWith(t, globalRecord(address, bogusToken))
				return []string{"HOME=" + home}, authFromSettings()
			},
		},
		{
			name:            "a token of the record for the directory of the call",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Access to the project is denied",
			where: func(t *testing.T, address string) ([]string, detail) {
				home, _ := homeWith(t, recordFile(scopedRecord(scope, address, bogusToken)))
				return []string{"HOME=" + home, "PWD=" + stated}, authFromSettings()
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"error":%q,"error_description":%q}`, tc.upstreamError, tc.upstreamMessage)
			server := serve(t, respondWith(tc.status, body))
			env, from := tc.where(t, server.url)

			got := runWith(t, env, "project", "show", "DEV")

			want := faultDocument{
				code: "denied",
				details: []detail{
					{"request", showRequest(server.url, "DEV")},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
					from,
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assertNoToken(t, got, bogusToken)
			assert.Len(t, server.requests(), 1)
		})
	}
}

// Every other code says something about what was asked for, and naming the token there would send the caller to
// look at the wrong thing. The documents themselves are held to in refusal_test.go.
func TestNoCommandLeavesTheOriginOutOfARefusalThatIsNotAboutTheToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		code        string
	}{
		{
			name:        "a project the server does not have",
			status:      http.StatusNotFound,
			contentType: "application/json",
			body:        `{"error":"Not Found","error_description":"Entity with id DEV not found"}`,
			code:        "not_found",
		},
		{
			name:        "a call the server would not take",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"error":"bad_request","error_description":"Query string has invalid syntax"}`,
			code:        "rejected",
		},
		{
			name:        "a server that broke",
			status:      http.StatusInternalServerError,
			contentType: "application/json",
			body:        `{"error":"server_error","error_description":"java.lang.NullPointerException"}`,
			code:        "upstream_failed",
		},
		{
			name:        "an answer of another shape",
			status:      http.StatusOK,
			contentType: "text/html",
			body:        "<!doctype html>\n<html><body>Log in</body></html>",
			code:        "upstream_invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})

			got := runWith(t, server.env(), "project", "show", "DEV")

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			var keys []string
			for _, held := range found.details {
				keys = append(keys, held.key)
			}
			assert.NotContains(t, keys, "auth_from")
			assertNoToken(t, got, token)
		})
	}
}

func TestAuthStatusRefusesATokenOfTheEnvironmentTheDevInstanceDoesNotKnow(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + bogusToken}, "auth", "status")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", meRequest(dev.url)},
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

func TestProjectShowRefusesATokenOfTheGlobalRecordTheDevInstanceDoesNotKnow(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	home, _ := homeWith(t, globalRecord(dev.url, bogusToken))

	got := runWith(t, []string{"HOME=" + home}, "project", "show", "DEV")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", showRequest(dev.url, "DEV")},
			{"upstream_status", 401},
			{"upstream_error", "Unauthorized"},
			{"upstream_message", "Invalid token"},
			authFromSettings(),
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assertNoToken(t, got, bogusToken)
	assert.Len(t, dev.requests(), 1)
}
