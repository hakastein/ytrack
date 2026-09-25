package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// Records of the list under its default expression, with $type and the keys in an order other than the one asked
// for: the server keeps an order of its own.
const (
	listedAdmin   = `{"fullName":"admin","$type":"User","banned":false,"login":"admin"}`
	listedLimited = `{"banned":false,"login":"dev.limited","$type":"User","fullName":"Ограниченный"}`
	listedUsers   = `[` + listedAdmin + `,` + listedLimited + `]`
)

const (
	printedAdminRow   = `  - {login: "admin", fullName: "admin", banned: false}` + "\n"
	printedLimitedRow = `  - {login: "dev.limited", fullName: "Ограниченный", banned: false}` + "\n"
	printedMemberRow  = `  - {login: "dev.member", fullName: "Участник", banned: false}` + "\n"
	printedGuestRow   = `  - {login: "guest", fullName: "гость", banned: true}` + "\n"
)

const printedListedUsers = "total: 2\nreturned: 2\ntruncated: false\nusers:\n" + printedAdminRow + printedLimitedRow

// A refusal names the request user list sends: the fields= expression reads as written, while search is the
// text the server received, escaped.
func userListRequest(address, fields, top, search string) string {
	return "GET " + address + "/api/users?fields=" + fields + "&$top=" + top + "&query=" + search
}

// searchingQueries is what user list sends for a selection of limit users that it goes on to count: the same
// search goes out both times, so the count is of the users found rather than of the catalogue.
func searchingQueries(search, limit string) []url.Values {
	return []url.Values{
		{"fields": {"login,fullName,banned"}, "$top": {limit}, "query": {search}},
		{"fields": {"id"}, "$top": {"-1"}, "query": {search}},
	}
}

// userListing is the document user list prints, read back.
type userListing struct {
	Total     int              `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated bool             `yaml:"truncated"`
	Users     []map[string]any `yaml:"users"`
}

func requireUserListing(t *testing.T, got outcome) userListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed userListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Users, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

func TestUserListTakesItsSearchFromTheQueryFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a word of its own", argv: []string{"user", "list", "dev"}},
		{name: "two words of their own", argv: []string{"user", "list", "a", "b"}},
		{name: "no search at all", argv: []string{"user", "list"}},
		{name: "the flag twice", argv: []string{"user", "list", "--query", "a", "--query", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestUserListRefusesALimitItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "zero", flags: []string{"--limit", "0"}},
		{name: "a negative number", flags: []string{"--limit", "-1"}},
		{name: "past the largest int32", flags: []string{"--limit", "2147483648"}},
		{name: "not a number", flags: []string{"--limit", "x"}},
		{name: "the flag twice", flags: []string{"--limit", "1", "--limit", "2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"user", "list", "--query", ""}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestUserListHelpNamesTheDefaultFieldsAndTheQueryFlag(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"user", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "login,fullName,banned")
	assert.Contains(t, got.stdout, "--query")
}

// The + of an expression adds to the default of this command, which is not the default of user show: banned
// stays in the row and email does not appear.
func TestUserListAddsFieldsToTheDefaultOfTheList(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `[{"id":"1-1","fullName":"admin","$type":"User","banned":false,"login":"admin"}]`))

	got := runWith(t, server.env(), "user", "list", "--query", "adm", "--fields", "+id")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + `  - {login: "admin", fullName: "admin", banned: false, id: "1-1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,banned,id"}, "$top": {"50"}, "query": {"adm"}}}, server.sentQueries())
}

// An expression that does not parse is refused before the network here too.
func TestUserListRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "user", "list", "--query", "adm", "--fields", "+")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// The search is the caller's text and reaches the server as it stands, whatever a query has to escape on the way.
func TestUserListSendsTheSearchTheServerMustReadBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "characters a query escapes", search: "Иван & Co = +100%#"},
		{name: "an empty text", search: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, listedUsers))

			got := runWith(t, server.env(), "user", "list", "--query", tc.search)

			assert.Equal(t, outcome{stdout: printedListedUsers}, got)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, "/api/users", requests[0].URL.Path)
			want := url.Values{"fields": {"login,fullName,banned"}, "$top": {"50"}, "query": {tc.search}}
			assert.Equal(t, want, requests[0].URL.Query())
		})
	}
}

// pflag reads the value of a flag as text wherever it starts, so a search needs no separator and a search that
// spells a flag of cobra's own is still a search.
func TestUserListSearchesForATextThatLooksLikeAFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		flag   []string
		search string
	}{
		{name: "a leading dash, apart", flag: []string{"--query", "-dev"}, search: "-dev"},
		{name: "a leading dash, joined", flag: []string{"--query=-dev"}, search: "-dev"},
		{name: "the word help, apart", flag: []string{"--query", "--help"}, search: "--help"},
		{name: "the word help, joined", flag: []string{"--query=--help"}, search: "--help"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, listedUsers))

			got := runWith(t, server.env(), slices.Concat([]string{"user", "list"}, tc.flag)...)

			// Printed rather than the help of the command, which cobra prints for --help anywhere else.
			assert.Equal(t, outcome{stdout: printedListedUsers}, got)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, []string{tc.search}, requests[0].URL.Query()["query"])
		})
	}
}

func TestUserListCountsTheUsersWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, countedBy(`[`+listedAdmin+`]`,
		respondWith(http.StatusOK, `[{"id":"1-1","$type":"User"},{"id":"1-2","$type":"User"},{"id":"1-3","$type":"User"}]`)))

	got := runWith(t, server.env(), "user", "list", "--query", "a", "--limit", "1")

	want := "total: 3\nreturned: 1\ntruncated: true\nusers:\n" + printedAdminRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, searchingQueries("a", "1"), server.sentQueries())
}

// A count that fails takes the command with it: half a document would say the rest were not cut off.
func TestUserListRefusesWhenTheCountFails(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	server := serve(t, countedBy(`[`+listedAdmin+`]`, respondWith(http.StatusInternalServerError, said)))

	got := runWith(t, server.env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code: "upstream_failed",
		details: []detail{
			{"request", userListRequest(server.url, "id", "-1", "a")},
			{"upstream_status", 500},
			{"upstream_error", "server_error"},
			{"upstream_message", "java.lang.NullPointerException"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 2)
}

// A page longer than the limit means $top went out wrong or the server ignored it, and the count that follows
// would be of something else, so the refusal is of the users it names.
func TestUserListRefusesMoreUsersThanTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, listedUsers))

	got := runWith(t, server.env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

// Fewer users counted than arrived is the selection changing between the two requests, and the document has no
// way to say so: total below returned would print truncated: false over a page that was cut.
func TestUserListRefusesACountBelowTheUsersReceived(t *testing.T) {
	t.Parallel()
	server := serve(t, countedBy(`[`+listedAdmin+`]`, respondWith(http.StatusOK, `[]`)))

	got := runWith(t, server.env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 0}, {"returned", 1}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, searchingQueries("a", "1"), server.sentQueries())
}

func TestUserListFindsTheLimitedUserOfTheDevInstanceByTheStartOfTheLogin(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "list", "--query", "dev.lim")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + printedLimitedRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, dev.requests(), 1)
}

// The search reads the full name as well as the login, so a user is found by the name a human knows them by.
func TestUserListFindsTheLimitedUserOfTheDevInstanceByTheStartOfTheFullName(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "list", "--query", "Огран")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + printedLimitedRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, dev.requests(), 1)
}

func TestUserListFindsNoUserOfTheDevInstanceByTheDomainOfAnEmail(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "list", "--query", "ytrack.local")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nusers: []\n"}, got)
	assert.Len(t, dev.requests(), 1)
}

// The server matches the start of a word and nothing inside one, so the tail of a full name finds nobody.
func TestUserListFindsNoUserOfTheDevInstanceByATextInsideAWord(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "list", "--query", "граниченный")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nusers: []\n"}, got)
	assert.Len(t, dev.requests(), 1)
}

// An empty search asks for the catalogue, which the limit alone cuts: one request, since fewer users arrived
// than were asked for.
func TestUserListFindsEveryUserOfTheDevInstanceForAnEmptySearch(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "list", "--query", "")

	printed := requireUserListing(t, got)
	rows := strings.SplitAfter(got.stdout, "\n")
	for _, row := range []string{printedAdminRow, printedLimitedRow, printedMemberRow, printedGuestRow} {
		assert.Contains(t, rows, row)
	}
	assert.False(t, printed.Truncated)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,banned"}, "$top": {"50"}, "query": {""}}}, dev.sentQueries())
}

func TestUserListCountsTheUsersOfTheDevInstanceBeyondTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "list", "--query", "", "--limit", "1")

	printed := requireUserListing(t, got)
	assert.Equal(t, 1, printed.Returned)
	assert.True(t, printed.Truncated)
	assert.GreaterOrEqual(t, printed.Total, 4)
	assert.Equal(t, searchingQueries("", "1"), dev.sentQueries())
}

// The catalogue of users is not filtered by the rights a token holds on projects: the limited token, which the
// dev instance answers 404 for the project DEV, is sent the same users as the admin, byte for byte.
func TestUserListPrintsTheAdminOfTheDevInstanceToTheLimitedTokenAsWell(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	asAdmin := runWith(t, dev.env(), "user", "list", "--query", "adm")
	asLimited := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "user", "list", "--query", "adm")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + printedAdminRow
	assert.Equal(t, outcome{stdout: want}, asAdmin)
	assert.Equal(t, asAdmin, asLimited)
	assert.Len(t, dev.requests(), 2)
}

func TestUserListRefusesANameTheSchemasOfTheDevInstanceDoNotDeclare(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "list", "--query", "adm", "--fields", "login,bogus")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", userListRequest(dev.url, "login,bogus", "50", "adm")},
			{"fields", "login,bogus"},
			{"unknown", []any{unknownEntry("bogus", userNames()...)}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}
