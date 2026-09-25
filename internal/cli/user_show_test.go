package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The limited user under the default expression, with $type, an id and the name the server sends although the
// schema declares none, and the keys in an order other than the one asked for.
const userLimited = `{"banned":false,"$type":"User","email":"dev.limited@ytrack.local","id":"1-2",` +
	`"name":"Ограниченный","fullName":"Ограниченный","login":"dev.limited"}`

const (
	printedLimited = `login: "dev.limited"
fullName: "Ограниченный"
email: "dev.limited@ytrack.local"
banned: false
`
	printedAdmin = `login: "admin"
fullName: "admin"
email: null
banned: false
`
	printedGuest = `login: "guest"
fullName: "гость"
email: null
banned: true
`
	printedMember = `login: "dev.member"
fullName: "Участник"
email: "dev.member@ytrack.local"
banned: false
`
)

// A refusal names the request user show sends: the login stands in the path escaped, the way it went out.
func userRequest(address, login, fields string) string {
	return "GET " + address + "/api/users/" + url.PathEscape(login) + "?fields=" + fields
}

// The whole refusal a 404 for a login becomes, with what the server said about it word for word.
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

// The + of an expression adds to the default of this command: what user show prints without a flag stays in the
// document, and the name asked for follows it.
func TestUserShowAddsFieldsToTheDefaultOfTheCommand(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, userLimited))

	got := runWith(t, server.env(), "user", "show", "dev.limited", "--fields", "+id")

	assert.Equal(t, outcome{stdout: printedLimited + `id: "1-2"` + "\n"}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,email,banned,id"}}}, server.sentQueries())
}

// An expression that does not parse is refused before the network here too: sent on, a "+" of its own would ask
// the server for the empty selection of fields, which it answers with a record of its choosing.
func TestUserShowRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "user", "show", "admin", "--fields", "+")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// A login the server would read as a path of its own reaches it escaped, as the one segment it is.
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

func TestUserShowTakesExactlyOneLogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no login", argv: []string{"user", "show"}},
		{name: "two logins", argv: []string{"user", "show", "admin", "guest"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
		})
	}
}

func TestUserRefusesACallThatNamesNoCommandOfIts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the group alone", argv: []string{"user"}},
		{name: "a command it does not have", argv: []string{"user", "bogus"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// Every string the server reads as something other than a login is refused before any request: sent, it would
// answer about another user, and the answer would look like the one that was asked for.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The forms refused before a request are no wider than what the server was measured to read as something else:
// each of these it reads as a login of its own, so each goes out and comes back a plain 404.
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

			assert.Equal(t, noSuchUser(server.url, tc.login), requireRefusal(t, got))
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, "/api/users/"+tc.login, requests[0].URL.Path)
		})
	}
}

// pflag reads a leading dash as flags wherever the word stands, so the separator is the only way in and the
// help of the command says so.
func TestUserShowRefusesALoginOfALeadingDashWithoutTheSeparator(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "user", "show", "-x")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

func TestUserShowSendsALoginOfALeadingDashAfterTheSeparator(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id -x not found"}`))

	got := runWith(t, server.env(), "user", "show", "--", "-x")

	assert.Equal(t, noSuchUser(server.url, "-x"), requireRefusal(t, got))
	requests := server.requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "/api/users/-x", requests[0].URL.Path)
}

func TestUserShowRefusesWithoutAToken(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, []string{"YTRACK_URL=" + server.url}, "user", "show", "admin")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

func TestUserShowHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"user", "show", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "login,fullName,email,banned")
}

func TestUserShowPrintsTheLimitedUserOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "dev.limited")

	assert.Equal(t, outcome{stdout: printedLimited}, got)
	assert.Len(t, dev.requests(), 1)
}

// Logins are unique whatever the case, so the server resolves one in any case and the printed login is the
// one it keeps rather than the one the caller wrote.
func TestUserShowPrintsTheLoginTheDevInstanceKeepsForALoginInAnotherCase(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "ADMIN")

	assert.Equal(t, outcome{stdout: printedAdmin}, got)
	assert.Len(t, dev.requests(), 1)
}

// A user with no email and a ban on the account: null says the email is not set, and banned is in the default
// because a banned user is still found and still allowed by a field.
func TestUserShowPrintsTheGuestOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "guest")

	assert.Equal(t, outcome{stdout: printedGuest}, got)
	assert.Len(t, dev.requests(), 1)
}

// The catalogue of users is not filtered by the rights a token holds on projects: the limited token, which the
// dev instance answers 404 for the project DEV, is sent the same user as the admin, byte for byte.
func TestUserShowPrintsTheAdminOfTheDevInstanceToTheLimitedTokenAsWell(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	asAdmin := runWith(t, dev.env(), "user", "show", "admin")
	asLimited := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "user", "show", "admin")

	assert.Equal(t, outcome{stdout: printedAdmin}, asAdmin)
	assert.Equal(t, asAdmin, asLimited)
	assert.Len(t, dev.requests(), 2)
}

// Rights hide no email of another user: the member's own email arrives as a string for the admin, for a user
// with no role at all and for the member, so email in the default costs no reader a key.
func TestUserShowPrintsTheEmailOfTheMemberOfTheDevInstanceToEveryToken(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	tokens := devTokens(t)

	asAdmin := runWith(t, dev.env(), "user", "show", "dev.member")
	asLimited := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + tokens.limited}, "user", "show", "dev.member")
	asMember := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + tokens.member}, "user", "show", "dev.member")

	assert.Equal(t, outcome{stdout: printedMember}, asAdmin)
	assert.Equal(t, asAdmin, asLimited)
	assert.Equal(t, asAdmin, asMember)
	assert.Len(t, dev.requests(), 3)
}

// A full name of one word holds no space, so the form lets it through and the server settles it: the refusal
// names the login the caller should have written and the command that finds it.
func TestUserShowRefusesTheFullNameOfTheLimitedUserOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "Ограниченный")

	assert.Equal(t, noSuchUser(dev.url, "Ограниченный"), requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

// An email address is no alias of a login, although a login may look like one.
func TestUserShowRefusesTheEmailOfTheLimitedUserOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "dev.limited@ytrack.local")

	assert.Equal(t, noSuchUser(dev.url, "dev.limited@ytrack.local"), requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

// The path resolves a login whole, while the search of user list takes the start of one: dev finds dev.limited
// and dev.member there and addresses nobody here.
func TestUserShowRefusesTheStartOfALoginOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "dev")

	assert.Equal(t, noSuchUser(dev.url, "dev"), requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

// An escaped slash stays one segment for the server too: it reads the login back whole instead of routing the
// request to a path of its own.
func TestUserShowRefusesALoginWithASlashOnTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "a/b")

	assert.Equal(t, noSuchUser(dev.url, "a/b"), requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

func TestUserShowRefusesANameTheSchemasOfTheDevInstanceDoNotDeclare(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "admin", "--fields", "login,fullNme")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", userRequest(dev.url, "admin", "login,fullNme")},
			{"fields", "login,fullNme"},
			{"unknown", []any{unknownEntry("fullNme", "fullName")}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}
