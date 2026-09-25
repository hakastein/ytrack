package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	listedFirst  = `{"fullName":"First","$type":"User","banned":false,"login":"first"}`
	listedSecond = `{"banned":false,"login":"second","$type":"User","fullName":"Second"}`
	listedUsers  = `[` + listedFirst + `,` + listedSecond + `]`
)

const printedFirstRow = `  - {login: "first", fullName: "First", banned: false}` + "\n"

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
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
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
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), slices.Concat([]string{"user", "list", "--query", ""}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestUserListAddsFieldsToTheDefaultOfTheList(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"id":"1-1","fullName":"First","$type":"User","banned":false,"login":"first"}]`))

	got := runWith(t, server.Env(), "user", "list", "--query", "fir", "--fields", "+id")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + `  - {login: "first", fullName: "First", banned: false, id: "1-1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,banned,id"}, "$top": {"50"}, "query": {"fir"}}}, server.Queries())
}

func TestUserListRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "user", "list", "--query", "fir", "--fields", "+")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestUserListCountsTheUsersWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, countedBy(`[`+listedFirst+`]`,
		fake.JSON(http.StatusOK, `[{"id":"1-1","$type":"User"},{"id":"1-2","$type":"User"},{"id":"1-3","$type":"User"}]`)))

	got := runWith(t, server.Env(), "user", "list", "--query", "a", "--limit", "1")

	want := "total: 3\nreturned: 1\ntruncated: true\nusers:\n" + printedFirstRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, searchingQueries("a", "1"), server.Queries())
}

func TestUserListRefusesWhenTheCountFails(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	server := fake.Serve(t, countedBy(`[`+listedFirst+`]`, fake.JSON(http.StatusInternalServerError, said)))

	got := runWith(t, server.Env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code: "upstream_failed",
		details: []detail{
			{"request", userListRequest(server.URL, "id", "-1", "a")},
			{"upstream_status", 500},
			{"upstream_error", "server_error"},
			{"upstream_message", "java.lang.NullPointerException"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 2)
}

func TestUserListRefusesMoreUsersThanTheLimit(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, listedUsers))

	got := runWith(t, server.Env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 1)
}

func TestUserListRefusesACountBelowTheUsersReceived(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, countedBy(`[`+listedFirst+`]`, fake.JSON(http.StatusOK, `[]`)))

	got := runWith(t, server.Env(), "user", "list", "--query", "a", "--limit", "1")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 0}, {"returned", 1}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, searchingQueries("a", "1"), server.Queries())
}
