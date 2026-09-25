package cli_test

import (
	"math"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

const tagFields = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))"

const privateStarOfEveryUser = "Звезда"

func tagsRequest(address, fields, top string) string {
	return "GET " + address + "/api/tags?fields=" + fields + "&$top=" + top
}

func countingTags(limit string) []url.Values {
	return []url.Values{
		{"fields": {tagFields}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
}

func TestTagListRefusesFlagsItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a limit of zero", argv: []string{"--limit", "0"}},
		{name: "a limit given twice", argv: []string{"--limit", "1", "--limit", "2"}},
		{name: "fields given twice", argv: []string{"--fields", "name", "--fields", "owner(login)"}},
		{name: "an expression that does not parse", argv: []string{"--fields", "name,,owner"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"tag", "list"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTagListPrintsTheRecordsAsTheyWereAskedFor(t *testing.T) {
	t.Parallel()
	const records = `[{"$type":"Tag","owner":{"$type":"User","login":"admin"},"name":"[bug] fix login",` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedUsers":[],"permittedGroups":[]}},` +
		`{"readSharingSettings":{"permittedGroups":[{"$type":"NestedGroup","name":"DEVELOPMENT Team"},` +
		`{"$type":"RegisteredUsersGroup","name":"Зарегистрированные пользователи"}],` +
		`"permittedUsers":[{"login":"dev.limited","$type":"User"}],"$type":"WatchFolderSharingSettings"},` +
		`"name":"карта","$type":"Tag","owner":{"login":"dev.member","$type":"User"}}]`
	server := serve(t, respondWith(http.StatusOK, records))

	got := runWith(t, server.env(), "tag", "list")

	want := "total: 2\nreturned: 2\ntruncated: false\ntags:\n" +
		`  - {name: "[bug] fix login", owner: {login: "admin"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n" +
		`  - {name: "карта", owner: {login: "dev.member"}, readSharingSettings: {permittedGroups: [{name: "DEVELOPMENT Team"}, {name: "Зарегистрированные пользователи"}], permittedUsers: [{login: "dev.limited"}]}}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/tags"}, server.sentPaths())
	assert.Equal(t, []url.Values{{"fields": {tagFields}, "$top": {"50"}}}, server.sentQueries())
}

func TestTagListSendsTheLimitAsTop(t *testing.T) {
	t.Parallel()
	for _, limit := range []string{"1", strconv.Itoa(math.MaxInt32)} {
		t.Run(limit, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, `[]`))

			got := runWith(t, server.env(), "tag", "list", "--limit", limit)

			assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\ntags: []\n"}, got)
			assert.Equal(t, []url.Values{{"fields": {tagFields}, "$top": {limit}}}, server.sentQueries(),
				"without $top the server stops at 42 tags and does not say there are more")
		})
	}
}

func TestTagListPrintsTheSameShapeForAnyNumberOfRecords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "none at all",
			body: `[]`,
			want: "total: 0\nreturned: 0\ntruncated: false\ntags: []\n",
		},
		{
			name: "one",
			body: `[{"$type":"Tag","name":"` + privateStarOfEveryUser + `","owner":{"$type":"User","login":"admin"},` +
				`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedGroups":[],"permittedUsers":[]}}]`,
			want: "total: 1\nreturned: 1\ntruncated: false\ntags:\n" +
				`  - {name: "` + privateStarOfEveryUser + `", owner: {login: "admin"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "tag", "list")

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestTagListCountsTheTagsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	const page = `[{"$type":"Tag","name":"a","owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedGroups":[],"permittedUsers":[]}},` +
		`{"$type":"Tag","name":"b","owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedGroups":[],"permittedUsers":[]}}]`
	const printed = `  - {name: "a", owner: {login: "admin"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n" +
		`  - {name: "b", owner: {login: "admin"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n"
	tests := []struct {
		name  string
		count string
		head  string
	}{
		{
			name:  "more counted than arrived",
			count: `[{"$type":"Tag","id":"10-2"},{"$type":"Tag","id":"10-3"},{"$type":"Tag","id":"10-4"}]`,
			head:  "total: 3\nreturned: 2\ntruncated: true\ntags:\n",
		},
		{
			name:  "as many counted as arrived",
			count: `[{"$type":"Tag","id":"10-2"},{"$type":"Tag","id":"10-3"}]`,
			head:  "total: 2\nreturned: 2\ntruncated: false\ntags:\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, countedBy(page, respondWith(http.StatusOK, tc.count)))

			got := runWith(t, server.env(), "tag", "list", "--limit", "2")

			assert.Equal(t, outcome{stdout: tc.head + printed}, got)
			assert.Equal(t, countingTags("2"), server.sentQueries())
			assert.Equal(t, []string{"/api/tags", "/api/tags"}, server.sentPaths())
		})
	}
}

func TestTagListRefusesACountTheServerWouldNotAnswer(t *testing.T) {
	t.Parallel()
	const page = `[{"$type":"Tag","name":"a","owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedGroups":[],"permittedUsers":[]}}]`
	server := serve(t, countedBy(page, respondWith(http.StatusInternalServerError, `{"error":"Internal Server Error"}`)))

	got := runWith(t, server.env(), "tag", "list", "--limit", "1")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, tagsRequest(server.url, "id", "-1"), detailNamed(t, found, "request"))
	assert.Equal(t, countingTags("1"), server.sentQueries())
	assert.Empty(t, got.stdout)
}

func TestTagListRefusesWhatTheServerAnswered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		code        string
	}{
		{
			name: "a token the server does not let through", status: http.StatusForbidden,
			contentType: "application/json", body: `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`,
			code: "denied",
		},
		{
			name: "a page under a 200", status: http.StatusOK,
			contentType: "text/html", body: "<html><body>Sign in</body></html>",
			code: "upstream_invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			got := runWith(t, server.env(), "tag", "list")

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tagsRequest(server.url, tagFields, "50"), detailNamed(t, found, "request"))
			assert.Len(t, server.requests(), 1)
			assert.Empty(t, got.stdout)
		})
	}
}
