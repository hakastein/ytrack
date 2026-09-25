package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func assertNoToken(t *testing.T, got outcome, secret string) {
	t.Helper()
	assert.NotContains(t, got.stdout, secret)
	assert.NotContains(t, got.stderr, secret)
}

func urlPrinted(t *testing.T, got outcome) any {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %q", got.stderr)
	for pair := range slices.Chunk(requireMapping(t, "stdout", got.stdout).Content, 2) {
		if pair[0].Value == "url" {
			return requireValue(t, pair[1])
		}
	}
	require.Fail(t, "no url was printed", "stdout: %q", got.stdout)
	return nil
}

func TestAuthStatusPrintsTheAddressTheSourceOfTheLoginAndTheUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login func(t *testing.T, server *fake.Server) []string
		from  string
	}{
		{name: "a login from the environment", login: func(_ *testing.T, server *fake.Server) []string { return server.Env() }, from: "environment"},
		{name: "a saved login", login: func(t *testing.T, server *fake.Server) []string {
			home, _ := homeWith(t, globalRecord(server.URL, fake.Token))
			return []string{"HOME=" + home}
		}, from: "settings"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"fullName":"Full Name","$type":"Me","login":"login"}`))

			got := runWith(t, tc.login(t, server), "auth", "status")

			assert.Equal(t, outcome{stdout: status(server.URL, tc.from, "login", "Full Name")}, got)
			sent := server.Request(t, 0)
			assert.Equal(t, []string{"/api/users/me"}, server.Paths())
			assert.Equal(t, http.MethodGet, sent.Method)
			assert.Equal(t, url.Values{"fields": {"login,fullName"}}, sent.URL.Query())
			assert.Equal(t, bearing(fake.Token), sent.Header.Get("Authorization"))
		})
	}
}

func TestAuthStatusPrintsTheAddressInOneSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		printed string
		path    string
	}{
		{name: "a scheme in capitals and a slash at the end", address: "HTTP://127.0.0.1:%s%s/", printed: "http://127.0.0.1:%s%s", path: "/api/users/me"},
		{name: "a host in capitals", address: "http://[::FFFF:127.0.0.1]:%s%s", printed: "http://[::ffff:127.0.0.1]:%s%s", path: "/api/users/me"},
		{name: "slashes at the end of a path", address: "http://127.0.0.1:%s%s/ctx//", printed: "http://127.0.0.1:%s%s/ctx", path: "/ctx/api/users/me"},
		{name: "escaped slashes in a path that ends in slashes", address: "http://127.0.0.1:%s%s/a%%2Fb%%2F//", printed: "http://127.0.0.1:%s%s/a%%2Fb%%2F", path: "/a%2Fb%2F/api/users/me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"login":"login","fullName":"Full Name","$type":"Me"}`))
			listening := server.Address(t)
			port, prefix := listening.Port(), listening.Path

			got := runWith(t, []string{"YTRACK_URL=" + fmt.Sprintf(tc.address, port, prefix), "YTRACK_TOKEN=" + fake.Token}, "auth", "status")

			assert.Equal(t, fmt.Sprintf(tc.printed, port, prefix), urlPrinted(t, got))
			assert.Equal(t, tc.path, server.Request(t, 0).URL.EscapedPath())
		})
	}
}

func TestAuthStatusPrintsTheAddressWithoutItsPassword(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"login":"login","fullName":"Full Name","$type":"Me"}`))
	address := server.Address(t)
	address.User = url.UserPassword("svc", "secret")

	got := runWith(t, []string{"YTRACK_URL=" + address.String(), "YTRACK_TOKEN=" + fake.Token}, "auth", "status")

	assert.Equal(t, "http://svc:xxxxx@"+address.Host+address.Path, urlPrinted(t, got))
}

func TestAuthStatusRefusesAUserWithoutTheFullName(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"login":"login","$type":"Me"}`))

	got := runWith(t, server.Env(), "auth", "status")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "GET " + server.URL + "/api/users/me?fields=login,fullName"},
			{"fields", "login,fullName"},
			{"missing", []any{missingEntry("fullName", "Me")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{"/api/users/me"}, server.Paths())
}
