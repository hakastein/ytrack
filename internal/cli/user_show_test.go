package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const userLimited = `{"banned":false,"$type":"User","email":"dev.limited@ytrack.local","id":"1-2",` +
	`"name":"Ограниченный","fullName":"Ограниченный","login":"dev.limited"}`

const printedLimited = `login: "dev.limited"
fullName: "Ограниченный"
email: "dev.limited@ytrack.local"
banned: false
`

func userRequest(address, login, fields string) string {
	return "GET " + address + "/api/users/" + url.PathEscape(login) + "?fields=" + fields
}

func noSuchUser(address, login string) faultDocument {
	return faultDocument{
		code: "not_found",
		details: []detail{
			{"request", userRequest(address, login, "login,fullName,email,banned")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id " + login + " not found"},
		},
	}
}

func TestUserShowPrintsTheFieldsAskedInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, userLimited))

	got := runWith(t, server.env(), "user", "show", "dev.limited")

	assert.Equal(t, outcome{stdout: printedLimited}, got)
	requests := server.requests()
	require.Len(t, requests, 1)
	request := requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "/api/users/dev.limited", request.URL.Path)
	assert.Equal(t, url.Values{"fields": {"login,fullName,email,banned"}}, request.URL.Query())
	assert.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
	assert.Equal(t, "application/json", request.Header.Get("Accept"))
}

func TestUserShowAddsFieldsToTheDefaultOfTheCommand(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, userLimited))

	got := runWith(t, server.env(), "user", "show", "dev.limited", "--fields", "+id")

	assert.Equal(t, outcome{stdout: printedLimited + `id: "1-2"` + "\n"}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,email,banned,id"}}}, server.sentQueries())
}

func TestUserShowRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "user", "show", "admin", "--fields", "+")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestUserShowSendsALoginThatLooksLikeAPathAsOneSegment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		login   string
		escaped string
	}{
		{name: "a slash", login: "a/b", escaped: "a%2Fb"},
		{name: "a question mark", login: "a?b", escaped: "a%3Fb"},
		{name: "a hash", login: "a#b", escaped: "a%23b"},
		{name: "a percent", login: "100%", escaped: "100%25"},
		{name: "letters outside ASCII", login: "Иван.Иванов", escaped: "%D0%98%D0%B2%D0%B0%D0%BD.%D0%98%D0%B2%D0%B0%D0%BD%D0%BE%D0%B2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, userLimited))

			got := runWith(t, server.env(), "user", "show", tc.login)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, "/api/users/"+tc.escaped, requests[0].URL.EscapedPath())
			assert.Equal(t, "/api/users/"+tc.login, requests[0].URL.Path)
		})
	}
}

func TestUserShowRefusesALoginThatWouldReachAnotherEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login string
	}{
		{name: "empty", login: ""},
		{name: "a dot", login: "."},
		{name: "two dots", login: ".."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "user", "show", tc.login)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestUserShowRefusesEveryFormTheServerReadsAsSomethingOtherThanALogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login string
	}{
		{name: "a full name", login: "Иван Иванов"},
		{name: "a login behind a space", login: " admin"},
		{name: "a login before a tab", login: "admin\t"},
		{name: "two words on two lines", login: "a\nb"},
		{name: "an internal id", login: "2-1"},
		{name: "an internal id with a leading zero", login: "02-1"},
		{name: "an internal id whose second number has a zero", login: "2-01"},
		{name: "a Hub id", login: "7fae4e41-01f8-42c0-9cc4-960c478d8a72"},
		{name: "a Hub id in upper case", login: "7FAE4E41-01F8-42C0-9CC4-960C478D8A72"},
		{name: "me", login: "me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "user", "show", tc.login)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestUserShowSendsAFormThatOnlyLooksLikeOneOfTheRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login string
	}{
		{name: "me in upper case", login: "ME"},
		{name: "me capitalised", login: "Me"},
		{name: "me with a digit after it", login: "me2"},
		{name: "an internal id with a letter after it", login: "2-1x"},
		{name: "a Hub id without the dashes", login: "7fae4e4101f842c09cc4960c478d8a72"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := "Entity with id " + tc.login + " not found"
			server := serve(t, respondWith(http.StatusNotFound, `{"error":"Not Found","error_description":"`+said+`"}`))

			got := runWith(t, server.env(), "user", "show", tc.login)

			assert.Equal(t, noSuchUser(server.url, tc.login), requireFault(t, got))
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, "/api/users/"+tc.login, requests[0].URL.Path)
		})
	}
}

func TestUserShowRefusesWithoutAToken(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, []string{"YTRACK_URL=" + server.url}, "user", "show", "admin")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}
