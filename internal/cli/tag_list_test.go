package cli_test

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The expression the list sends where the caller writes none.
const tagFields = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))"

// The tag YouTrack builds for every user itself and shows to no one else.
const builtInStar = "Звезда"

// tagsRequest is the request the list sends, as a refusal names it.
func tagsRequest(address, fields, top string) string {
	return "GET " + address + "/api/tags?fields=" + fields + "&$top=" + top
}

// The queries of a list that fills its limit and is therefore counted: the page, then the pass over ids alone.
func countingTags(limit string) []url.Values {
	return []url.Values{
		{"fields": {tagFields}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
}

// tagListing is the document tag list prints, read back. A record is a map rather than a struct so that a
// scenario can hold it to the keys it carries and to nothing besides.
type tagListing struct {
	Total     int              `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated bool             `yaml:"truncated"`
	Tags      []map[string]any `yaml:"tags"`
}

func requireTagListing(t *testing.T, got outcome) tagListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed tagListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Tags, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

// ownerOf is the login a printed record names its owner by.
func ownerOf(t *testing.T, record map[string]any) string {
	t.Helper()
	owner, isObject := record["owner"].(map[string]any)
	require.True(t, isObject, "the owner of %v is no object", record)
	login, isText := owner["login"].(string)
	require.True(t, isText, "the login of %v is no string", owner)
	return login
}

// What the group offers and what it does not, settled before the network: the list is of one token's tags
// and takes no argument at all, since the owner of a tag is a user rather than anything a caller writes.
func TestTagRefusesACallThatNamesNoCommandOfIts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the group alone", argv: []string{"tag"}},
		{name: "a verb it has none of", argv: []string{"tag", "bogus"}},
		{name: "a list of one owner", argv: []string{"tag", "list", "x"}},
		{name: "a list of a project", argv: []string{"tag", "list", "DEV"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The limit reaches the server as $top, an int32, so a number it could not carry is refused before any
// request; both flags of the list are given once, and an expression that does not parse is refused where
// every other one is.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The help of the list says what a caller gets unasked, so the default fields expression is read off it
// rather than off this repository.
func TestTagListHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"tag", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, tagFields)
}

// A record holds what was asked of it, in the order it was asked, whatever order the server sent: $type is
// the server's own and stands nowhere, an empty set of readers prints empty rather than missing, and a name
// carrying brackets is a name like any other (3.14). The request carries the limit as $top and the default
// expression as fields, and neither query nor $skip goes with it.
func TestTagListPrintsTheRecordsAsTheyWereAskedFor(t *testing.T) {
	t.Parallel()
	const records = `[{"$type":"Tag","owner":{"$type":"User","login":"admin"},"name":"[bug] fix login",` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedUsers":[],"permittedGroups":[]}},` +
		`{"readSharingSettings":{"permittedGroups":[{"$type":"NestedGroup","name":"DEVELOPMENT Team"},` +
		`{"$type":"RegisteredUsersGroup","name":"Зарегистрированные пользователи"}],` +
		`"permittedUsers":[{"login":"dev.limited","$type":"User"}],"$type":"WatchFolderSharingSettings"},` +
		`"name":"карта","$type":"Tag","owner":{"login":"dev.member","$type":"User"}}]`
	server := serve(t, answer(http.StatusOK, records))

	got := runWith(t, server.env(), "tag", "list")

	want := "total: 2\nreturned: 2\ntruncated: false\ntags:\n" +
		`  - {name: "[bug] fix login", owner: {login: "admin"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n" +
		`  - {name: "карта", owner: {login: "dev.member"}, readSharingSettings: {permittedGroups: [{name: "DEVELOPMENT Team"}, {name: "Зарегистрированные пользователи"}], permittedUsers: [{login: "dev.limited"}]}}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/tags"}, server.sentPaths())
	assert.Equal(t, []url.Values{{"fields": {tagFields}, "$top": {"50"}}}, server.sentQueries())
}

// The limit goes out on every call, whatever it is: without $top the server stops at 42 tags and says
// nothing of the rest.
func TestTagListSendsTheLimitAsTop(t *testing.T) {
	t.Parallel()
	for _, limit := range []string{"1", "2147483647"} {
		t.Run(limit, func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(http.StatusOK, `[]`))

			got := runWith(t, server.env(), "tag", "list", "--limit", limit)

			assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\ntags: []\n"}, got)
			assert.Equal(t, []url.Values{{"fields": {tagFields}, "$top": {limit}}}, server.sentQueries())
		})
	}
}

// The document is the same shape whatever came back: a token shown no tag at all prints the key empty,
// and one tag prints a list of one rather than a record of its own (5.8).
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
			body: `[{"$type":"Tag","name":"` + builtInStar + `","owner":{"$type":"User","login":"admin"},` +
				`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedGroups":[],"permittedUsers":[]}}]`,
			want: "total: 1\nreturned: 1\ntruncated: false\ntags:\n" +
				`  - {name: "` + builtInStar + `", owner: {login: "admin"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, answer(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "tag", "list")

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

// A page that fills the limit proves nothing about the rest, so the whole is read off a second pass over
// ids alone; a page short of the limit is the whole of it and costs no second request.
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
			server := serve(t, countedBy(page, answer(http.StatusOK, tc.count)))

			got := runWith(t, server.env(), "tag", "list", "--limit", "2")

			assert.Equal(t, outcome{stdout: tc.head + printed}, got)
			assert.Equal(t, countingTags("2"), server.sentQueries())
			assert.Equal(t, []string{"/api/tags", "/api/tags"}, server.sentPaths())
		})
	}
}

// The count is a request like any other, and one the server refuses leaves nothing to print: a page whose
// whole is unknown is no answer to how many tags there are.
func TestTagListRefusesACountTheServerWouldNotAnswer(t *testing.T) {
	t.Parallel()
	const page = `[{"$type":"Tag","name":"a","owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedGroups":[],"permittedUsers":[]}}]`
	server := serve(t, countedBy(page, answer(http.StatusInternalServerError, `{"error":"Internal Server Error"}`)))

	got := runWith(t, server.env(), "tag", "list", "--limit", "1")

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, tagsRequest(server.url, "id", "-1"), detailNamed(t, found, "request"))
	assert.Equal(t, countingTags("1"), server.sentQueries())
	assert.Empty(t, got.stdout)
}

// What the server answers with is read the way it is read everywhere: a token it does not let through,
// and a page under a 200, which is what an instance behind a login form answers with (3.6).
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
			code: "upstream_lied",
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

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tagsRequest(server.url, tagFields, "50"), detailNamed(t, found, "request"))
			assert.Len(t, server.requests(), 1)
			assert.Empty(t, got.stdout)
		})
	}
}

// What the polygon shows its admin, read by the command that is about tags: every record carries the
// three keys of the default and no fourth, the built-in star of that token stands among them, and no tag of
// another user does — the polygon shares none.
func TestTagListReadsThePolygonTagsOfTheAdmin(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "tag", "list")

	printed := requireTagListing(t, got)
	require.NotEmpty(t, printed.Tags)
	stars := 0
	for _, record := range printed.Tags {
		assert.Equal(t, []string{"name", "owner", "readSharingSettings"}, slices.Sorted(maps.Keys(record)))
		assert.Equal(t, "admin", ownerOf(t, record))
		if record["name"] == builtInStar {
			stars++
		}
	}
	assert.Equal(t, 1, stars, "the admin is shown a star other than their own")
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/tags", dev.sentPaths()[0])
	assert.Equal(t, []string{tagFields}, dev.sentFields())
}

// The star is one tag per user and no one else's business: the limited token is shown its own and none of
// the four the admin owns, so the same instance answers the two of them with two catalogues that share not one
// record.
func TestTagListShowsTheLimitedTokenItsOwnStarAlone(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "tag", "list")

	printed := requireTagListing(t, got)
	stars := 0
	for _, record := range printed.Tags {
		assert.Equal(t, "dev.limited", ownerOf(t, record))
		if record["name"] == builtInStar {
			stars++
		}
	}
	assert.Equal(t, 1, stars)
	require.Len(t, dev.requests(), 1)
}
