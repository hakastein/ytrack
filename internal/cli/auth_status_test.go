package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusFromEnv is the document auth status prints when env holds both the address and the token.
func statusFromEnv(address, login, fullName string) string {
	return status(address, "environment", login, fullName)
}

func assertNoToken(t *testing.T, got outcome, secret string) {
	t.Helper()
	assert.NotContains(t, got.stdout, secret)
	assert.NotContains(t, got.stderr, secret)
}

func TestAuthRefusesACallThatDoesNotAssemble(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the group with no command", argv: []string{"auth"}},
		{name: "a command the group does not have", argv: []string{"auth", "bogus"}},
		{name: "an argument to status", argv: []string{"auth", "status", "extra"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assertNoToken(t, got, token)
		})
	}
}

func TestNoCommandTakesTheAddressOrTheTokenAsAFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the token on the root", argv: []string{"--token", token}},
		{name: "the address on the root", argv: []string{"--base-url", "http://h"}},
		{name: "the token on project show", argv: []string{"project", "show", "DEV", "--token", token}},
		{name: "the address on project show", argv: []string{"project", "show", "DEV", "--base-url", "http://h"}},
		{name: "the token on auth status", argv: []string{"auth", "status", "--token", token}},
		{name: "the token on auth status after =", argv: []string{"auth", "status", "--token=" + token}},
		{name: "the address on auth status", argv: []string{"auth", "status", "--base-url", "http://h"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assertNoToken(t, got, token)
		})
	}
}

// A command listed without a word about it reads as one left half-made, so the caller goes to the source for what
// the list was there to say.
func TestAuthHelpSaysWhatEveryCommandOfTheGroupDoes(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "auth", "--help")

	require.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	for _, command := range []string{"login", "logout", "status"} {
		assert.Regexp(t, `(?m)^[^\S\n]+`+command+`[^\S\n]+\S`, got.stdout, "%s is listed without a description", command)
	}
}

func TestAuthStatusHelpPrintsNoToken(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "auth", "status", flag)

			assert.Equal(t, 0, got.code)
			assert.Empty(t, got.stderr)
			assert.Contains(t, got.stdout, "ytrack auth status")
			assertNoToken(t, got, token)
		})
	}
}

func TestAuthStatusPrintsTheAddressTheOriginsAndTheUserFromEnv(t *testing.T) {
	t.Parallel()
	// The keys in an order other than asked: the server keeps an order of its own.
	server := serve(t, answer(http.StatusOK, `{"fullName":"Administrator","$type":"Me","login":"admin"}`))

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
		name string
		// The address and what is printed of it are formats of the port the server listens on.
		address string
		printed string
		path    string
	}{
		{name: "a scheme in capitals and a slash at the end", address: "HTTP://127.0.0.1:%s/", printed: "http://127.0.0.1:%s", path: "/api/users/me"},
		// An IPv4-mapped address reaches the server's IPv4 listener and has letters to write in capitals.
		{name: "a host in capitals", address: "http://[::FFFF:127.0.0.1]:%s", printed: "http://[::ffff:127.0.0.1]:%s", path: "/api/users/me"},
		{name: "slashes at the end of a path", address: "http://127.0.0.1:%s/ctx//", printed: "http://127.0.0.1:%s/ctx", path: "/ctx/api/users/me"},
		{name: "escaped slashes in a path that ends in slashes", address: "http://127.0.0.1:%s/a%%2Fb%%2F//", printed: "http://127.0.0.1:%s/a%%2Fb%%2F", path: "/a%2Fb%2F/api/users/me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(http.StatusOK, `{"login":"admin","fullName":"Administrator","$type":"Me"}`))
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
	server := serve(t, answer(http.StatusOK, `{"login":"admin","fullName":"Administrator","$type":"Me"}`))
	address, err := url.Parse(server.url)
	require.NoError(t, err)
	address.User = url.UserPassword("svc", "secret")

	got := runWith(t, []string{"YTRACK_URL=" + address.String(), "YTRACK_TOKEN=" + token}, "auth", "status")

	assert.Equal(t, outcome{stdout: statusFromEnv("http://svc:xxxxx@"+address.Host, "admin", "Administrator")}, got)
	assertNoToken(t, got, token)
}

func TestAuthStatusRefusesAUserWithoutTheFullName(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, `{"login":"admin","$type":"Me"}`))

	got := runWith(t, server.env(), "auth", "status")

	want := refusal{
		code: "upstream_lied",
		details: []detail{
			{"request", "GET " + server.url + "/api/users/me?fields=login,fullName"},
			{"fields", "login,fullName"},
			{"missing", []any{missingEntry("fullName", "Me")}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assertNoToken(t, got, token)
	assert.Len(t, server.requests(), 1)
}

func TestAuthStatusPrintsTheAdminOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "auth", "status")

	assert.Equal(t, outcome{stdout: statusFromEnv(dev.url, "admin", "admin")}, got)
	assertNoToken(t, got, dev.token)
	assert.Len(t, dev.requests(), 1)
}

func TestAuthStatusPrintsTheLimitedUserOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := devTokens(t).limited

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + limited}, "auth", "status")

	assert.Equal(t, outcome{stdout: statusFromEnv(dev.url, "dev.limited", "Ограниченный")}, got)
	assertNoToken(t, got, limited)
	assert.Len(t, dev.requests(), 1)
}

func TestAuthStatusPrintsTheMemberOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	member := devTokens(t).member

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + member}, "auth", "status")

	assert.Equal(t, outcome{stdout: statusFromEnv(dev.url, "dev.member", "Участник")}, got)
	assertNoToken(t, got, member)
	assert.Len(t, dev.requests(), 1)
}
