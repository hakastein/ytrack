package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	listedAdmin   = `{"fullName":"admin","$type":"User","banned":false,"login":"admin"}`
	listedLimited = `{"banned":false,"login":"dev.limited","$type":"User","fullName":"Ограниченный"}`
	listedUsers   = `[` + listedAdmin + `,` + listedLimited + `]`
)

const (
	printedAdminRow   = `  - {login: "admin", fullName: "admin", banned: false}` + "\n"
	printedLimitedRow = `  - {login: "dev.limited", fullName: "Ограниченный", banned: false}` + "\n"
)

const printedListedUsers = "total: 2\nreturned: 2\ntruncated: false\nusers:\n" + printedAdminRow + printedLimitedRow

func userListRequest(address, fields, top, escapedSearch string) string {
	return "GET " + address + "/api/users?fields=" + fields + "&$top=" + top + "&query=" + escapedSearch
}

func searchingQueries(search, limit string) []url.Values {
	return []url.Values{
		{"fields": {"login,fullName,banned"}, "$top": {limit}, "query": {search}},
		{"fields": {"id"}, "$top": {"-1"}, "query": {search}},
	}
}

func TestUserListTakesItsSearchFromTheQueryFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no search at all", argv: []string{"user", "list"}},
		{name: "the flag twice", argv: []string{"user", "list", "--query", "a", "--query", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
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
		{name: "the flag twice", flags: []string{"--limit", "1", "--limit", "2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"user", "list", "--query", ""}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestUserListAddsFieldsToTheDefaultOfTheList(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `[{"id":"1-1","fullName":"admin","$type":"User","banned":false,"login":"admin"}]`))

	got := runWith(t, server.env(), "user", "list", "--query", "adm", "--fields", "+id")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + `  - {login: "admin", fullName: "admin", banned: false, id: "1-1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,banned,id"}, "$top": {"50"}, "query": {"adm"}}}, server.sentQueries())
}

func TestUserListRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "user", "list", "--query", "adm", "--fields", "+")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}

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

func TestUserListCountsTheUsersWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, countedBy(`[`+listedAdmin+`]`,
		respondWith(http.StatusOK, `[{"id":"1-1","$type":"User"},{"id":"1-2","$type":"User"},{"id":"1-3","$type":"User"}]`)))

	got := runWith(t, server.env(), "user", "list", "--query", "a", "--limit", "1")

	want := "total: 3\nreturned: 1\ntruncated: true\nusers:\n" + printedAdminRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, searchingQueries("a", "1"), server.sentQueries())
}

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
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 2)
}

func TestUserListRefusesMoreUsersThanTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, listedUsers))

	got := runWith(t, server.env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 1)
}

func TestUserListRefusesACountBelowTheUsersReceived(t *testing.T) {
	t.Parallel()
	server := serve(t, countedBy(`[`+listedAdmin+`]`, respondWith(http.StatusOK, `[]`)))

	got := runWith(t, server.env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 0}, {"returned", 1}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, searchingQueries("a", "1"), server.sentQueries())
}
