package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusFromEnv(address, login, fullName string) string {
	return status(address, "environment", login, fullName)
}

func assertNoToken(t *testing.T, got outcome, secret string) {
	t.Helper()
	assert.NotContains(t, got.stdout, secret)
	assert.NotContains(t, got.stderr, secret)
}

func TestAuthStatusPrintsTheAddressTheLoginSourcesAndTheUserFromEnv(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `{"fullName":"Administrator","$type":"Me","login":"admin"}`))

	got := runWith(t, server.env(), "auth", "status")

	assert.Equal(t, outcome{stdout: statusFromEnv(server.url, "admin", "Administrator")}, got)
	assertNoToken(t, got, token)
	requests := server.requests()
	require.Len(t, requests, 1)
	request := requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "/api/users/me", request.URL.Path)
	assert.Equal(t, url.Values{"fields": {"login,fullName"}}, request.URL.Query())
	assert.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
}

func TestAuthStatusPrintsTheAddressInOneSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		printed string
		path    string
	}{
		{name: "a scheme in capitals and a slash at the end", address: "HTTP://127.0.0.1:%s/", printed: "http://127.0.0.1:%s", path: "/api/users/me"},
		{name: "a host in capitals", address: "http://[::FFFF:127.0.0.1]:%s", printed: "http://[::ffff:127.0.0.1]:%s", path: "/api/users/me"},
		{name: "slashes at the end of a path", address: "http://127.0.0.1:%s/ctx//", printed: "http://127.0.0.1:%s/ctx", path: "/ctx/api/users/me"},
		{name: "escaped slashes in a path that ends in slashes", address: "http://127.0.0.1:%s/a%%2Fb%%2F//", printed: "http://127.0.0.1:%s/a%%2Fb%%2F", path: "/a%2Fb%2F/api/users/me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, `{"login":"admin","fullName":"Administrator","$type":"Me"}`))
			listening, err := url.Parse(server.url)
			require.NoError(t, err)
			port := listening.Port()

			got := runWith(t, []string{"YTRACK_URL=" + fmt.Sprintf(tc.address, port), "YTRACK_TOKEN=" + token}, "auth", "status")

			assert.Equal(t, outcome{stdout: statusFromEnv(fmt.Sprintf(tc.printed, port), "admin", "Administrator")}, got)
			assertNoToken(t, got, token)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, tc.path, requests[0].URL.EscapedPath())
		})
	}
}

func TestAuthStatusPrintsTheAddressWithoutItsPassword(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `{"login":"admin","fullName":"Administrator","$type":"Me"}`))
	address, err := url.Parse(server.url)
	require.NoError(t, err)
	address.User = url.UserPassword("svc", "secret")

	got := runWith(t, []string{"YTRACK_URL=" + address.String(), "YTRACK_TOKEN=" + token}, "auth", "status")

	assert.Equal(t, outcome{stdout: statusFromEnv("http://svc:xxxxx@"+address.Host, "admin", "Administrator")}, got)
	assertNoToken(t, got, token)
}

func TestAuthStatusRefusesAUserWithoutTheFullName(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `{"login":"admin","$type":"Me"}`))

	got := runWith(t, server.env(), "auth", "status")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "GET " + server.url + "/api/users/me?fields=login,fullName"},
			{"fields", "login,fullName"},
			{"missing", []any{missingEntry("fullName", "Me")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assertNoToken(t, got, token)
	assert.Len(t, server.requests(), 1)
}
