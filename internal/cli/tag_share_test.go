package cli_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The expression the catalogue of groups is read with: the name a caller writes a group by and the id the body
// addresses it by.
const shownGroupFields = "id,name"

// The request that reads the groups, as a refusal names it.
func groupsRequest(address string) string {
	return "GET " + address + "/api/groups?fields=" + shownGroupFields + "&$top=-1"
}

// The group of the polygon every user of it stands in, which is the one a scenario shares a tag with when it
// wants another token to be shown it.
const everyoneRegistered = "Зарегистрированные пользователи"

// One record of the catalogue of groups, as the server sends it. The class differs from group to group — a team
// of a project, a group of Hub and the two built-in ones are four schemas — and every one of them is a
// UserGroup, which is what the catalogue is read as.
func shownGroup(id, name, kind string) string {
	return `{"$type":` + strconv.Quote(kind) + `,"id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) + `}`
}

// Every name the fake catalogue carries, in code point order, which is what a caller near none of them is shown.
func everyGroupName() []any {
	return []any{"DEVELOPMENT Team", "Все пользователи", `ООО "РОМАШКА", Москва`}
}

// The groups a token is shown: a team of a project, the built-in group of everyone, and one whose name carries
// a comma — an instance may keep such, and a flag reading commas would resolve none of them.
func groupsOfTheInstance() string {
	return "[" + strings.Join([]string{
		shownGroup("6-1", "DEVELOPMENT Team", "ProjectTeam"),
		shownGroup("6-0", "Все пользователи", "AllUsersGroup"),
		shownGroup("101-0", `ООО "РОМАШКА", Москва`, "NestedGroup"),
	}, ",") + "]"
}

// A group as it stands in a set of sharing the server answers with: the id the body addressed it by and the
// name a record prints it under.
type sharedGroup struct {
	id   string
	name string
}

// One set of sharing as YouTrack keeps it, under the class the server names it by. A nil set is one the tag is
// shared with nobody through.
func sharedSetOf(kind string, groups []sharedGroup) string {
	items := make([]string, 0, len(groups))
	for _, group := range groups {
		items = append(items, shownGroup(group.id, group.name, "NestedGroup"))
	}
	return `{"$type":` + strconv.Quote(kind) + `,"permittedGroups":[` + strings.Join(items, ",") +
		`],"permittedUsers":[]}`
}

// The set of those shown a tag and the set of those who may change it, which the server keeps under one class.
func sharingOf(groups []sharedGroup) string {
	return sharedSetOf("WatchFolderSharingSettings", groups)
}

// The set of those who may hang the tag, which is a class of its own on the server.
func taggingOf(groups []sharedGroup) string {
	return sharedSetOf("TagSharingSettings", groups)
}

// sharedTag is the answer of the server to a creation that shared the tag: the third set stands beside the two
// the call named, empty, since a creation asks for all three whatever it wrote.
func sharedTag(name string, read, update []sharedGroup) string {
	return `{"$type":"Tag","name":` + asJSON(name) + `,"owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":` + sharingOf(read) + `,"updateSharingSettings":` + sharingOf(update) +
		`,"tagSharingSettings":` + taggingOf(nil) + `}`
}

// sharingATag is the server of a creation that names a group: the catalogue answers the read of the groups, and
// creation the POST that follows it.
func sharingATag(t *testing.T, catalogue string, creation http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creation(w, r)
			return
		}
		answer(http.StatusOK, catalogue)(w, r)
	})
}

// sentBody is the body of the request the server was sent last, read as JSON reads it, which is what a scenario
// of more than one request holds the write to.
func sentBody(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body))
	return body
}

// sentMethodsFrom is the methods of the requests a scenario sent after the ones its fixtures cost, which is how
// a contract test holds one command to what it asked without counting what stood before it.
func sentMethodsFrom(u *upstream, at int) []string {
	return sentMethods(u)[at:]
}

// permittedNames is the names a printed set of sharing lists its groups under.
func permittedNames(t *testing.T, record map[string]any, set string) []string {
	t.Helper()
	sharing, isObject := record[set].(map[string]any)
	require.True(t, isObject, "%s of %v is no object", set, record)
	groups, isList := sharing["permittedGroups"].([]any)
	require.True(t, isList, "the permitted groups of %v are no list", sharing)
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		named, isObject := group.(map[string]any)
		require.True(t, isObject, "%v is no group", group)
		name, isText := named["name"].(string)
		require.True(t, isText, "the name of %v is no string", named)
		names = append(names, name)
	}
	return names
}

// createdRecord is the tag a creation printed, read back as a mapping of its keys.
func createdRecord(t *testing.T, got outcome) map[string]any {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	var record map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(got.stdout), &record), "stdout: %s", got.stdout)
	return record
}

// A group is addressed by its name, and an empty one names no group there could be: the refusal comes
// before anything is sent, so no catalogue is read for a name that answers to nothing whatever it holds.
func TestTagCreateRefusesAGroupOfNoName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nobody is shown it", argv: []string{"--visible-for", ""}},
		{name: "nobody may change it", argv: []string{"--updateable-by", ""}},
		{name: "one group and an empty one after it", argv: []string{"--visible-for", "DEVELOPMENT Team", "--visible-for", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"tag", "create", "--name", "карта"}, tc.argv...)...)

			want := refusal{code: "bad_usage"}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// Being shown a tag, changing it and hanging it on an issue are three separate rights in YouTrack; each flag
// writes one of them.
func TestTagCreateHelpNamesTheSharingFlags(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"tag", "create", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	for _, flag := range []string{"--visible-for", "--updateable-by", "--taggable-by"} {
		assert.Contains(t, got.stdout, flag)
	}
}

// The names of the groups become the two sets of the body and nothing else: a comma inside a name is part
// of the name rather than the end of a value, a group named twice stands in the set once, and visibleFor and
// updateableBy — the one-group members the flags are named after — are written nowhere at all.
func TestTagCreateWritesTheNamedGroupsAsTheTwoSets(t *testing.T) {
	t.Parallel()
	const name = "карта"
	server := sharingATag(t, groupsOfTheInstance(), answer(http.StatusOK, sharedTag(name,
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}, {id: "101-0", name: `ООО "РОМАШКА", Москва`}},
		[]sharedGroup{{id: "6-0", name: "Все пользователи"}})))

	got := runWith(t, server.env(), "tag", "create", "--name", name,
		"--visible-for", "development team",
		"--visible-for", `ООО "РОМАШКА", Москва`,
		"--visible-for", "DEVELOPMENT TEAM",
		"--updateable-by", "все пользователи")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/groups", "/api/tags"}, server.sentPaths())
	assert.Equal(t, map[string]any{
		"name": name,
		"readSharingSettings": map[string]any{"permittedGroups": []any{
			map[string]any{"id": "6-1"},
			map[string]any{"id": "101-0"},
		}},
		"updateSharingSettings": map[string]any{"permittedGroups": []any{
			map[string]any{"id": "6-0"},
		}},
	}, sentBody(t, server))
}

// The ids the body carries are asked back for although no record prints one: the groups went out as ids,
// so that is what the answer is held to, while the caller's own tree is the whole of what is printed.
func TestTagCreateAsksForTheIDsItChecksAndPrintsTheNames(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), answer(http.StatusOK, sharedTag("карта",
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}}, nil)))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "DEVELOPMENT Team")

	want := `name: "карта"` + "\n" +
		"owner:\n  login: \"admin\"\n" +
		"readSharingSettings:\n  permittedGroups:\n    - {name: \"DEVELOPMENT Team\"}\n  permittedUsers: []\n" +
		"updateSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n" +
		"tagSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{shownGroupFields,
		"name,owner(login),readSharingSettings(permittedGroups(name,id),permittedUsers(login))," +
			"updateSharingSettings(permittedGroups(name),permittedUsers(login))," +
			"tagSharingSettings(permittedGroups(name),permittedUsers(login))"}, server.sentFields())
}

// Every name of both flags is read before any of it is refused, so a call naming two groups that are not
// there is answered once rather than twice over; the refusal carries the names nearest each of them, and
// nothing was written by the time it came.
func TestTagCreateRefusesEveryGroupItCannotResolveAtOnce(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Нет", "--updateable-by", "Тоже")

	want := refusal{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.url)},
			{"unknown", []any{
				[]detail{{"group", "Нет"}, {"nearest", everyGroupName()}},
				[]detail{{"group", "Тоже"}, {"nearest", everyGroupName()}},
			}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// A name more than one group answers to settles nothing, and the internal id is no name a caller may
// write, so the refusal names the candidates and asks for one of them exactly as it stands.
func TestTagCreateRefusesAGroupNameMoreThanOneGroupAnswersTo(t *testing.T) {
	t.Parallel()
	const catalogue = `[{"$type":"NestedGroup","id":"6-1","name":"Команда"},` +
		`{"$type":"ProjectTeam","id":"6-2","name":"команда"}]`
	server := sharingATag(t, catalogue, noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "КОМАНДА")

	want := refusal{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.url)},
			{"ambiguous", []any{[]detail{
				{"group", "КОМАНДА"},
				{"candidates", []any{"Команда", "команда"}},
			}}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// The catalogue a token is shown where two groups are named alike: Команда and команда differ by letter case
// alone, and the resolver folds case, so neither answers a name the other does not. The two teams beside them
// are what a name near nothing at all is answered with.
func groupsNamedAlike() string {
	return "[" + strings.Join([]string{
		shownGroup("6-1", "DEVELOPMENT Team", "ProjectTeam"),
		shownGroup("6-0", "Все пользователи", "AllUsersGroup"),
		shownGroup("7-1", "Команда", "NestedGroup"),
		shownGroup("7-2", "команда", "ProjectTeam"),
	}, ",") + "]"
}

// Where more than one group answers to a name, the one written exactly as it stands wins, and it is the
// id of that one that reaches the body: the two differ by letter case alone, so the call would be ambiguous
// were the exact writing not read — and each of the two is reached by writing it out.
func TestTagCreateResolvesAGroupNameTwoGroupsAnswerToByWritingItExactly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		id      string
	}{
		{name: "the one in title case", written: "Команда", id: "7-1"},
		{name: "the one in lower case", written: "команда", id: "7-2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := sharingATag(t, groupsNamedAlike(), answer(http.StatusOK, sharedTag("карта",
				[]sharedGroup{{id: tc.id, name: tc.written}}, nil)))

			got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", tc.written)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, map[string]any{
				"name": "карта",
				"readSharingSettings": map[string]any{"permittedGroups": []any{
					map[string]any{"id": tc.id},
				}},
			}, sentBody(t, server))
		})
	}
}

// Some names resolve to no group and some to more than one, and the call names every one of them in one
// refusal: the two keys stand side by side under the one request the reading cost, and nothing is written.
func TestTagCreateRefusesTheUnknownGroupsAndTheAmbiguousOnesTogether(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsNamedAlike(), noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Нет", "--updateable-by", "КОМАНДА")

	want := refusal{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.url)},
			{"unknown", []any{
				[]detail{{"group", "Нет"}, {"nearest", []any{"DEVELOPMENT Team", "Все пользователи", "Команда", "команда"}}},
			}},
			{"ambiguous", []any{
				[]detail{{"group", "КОМАНДА"}, {"candidates", []any{"Команда", "команда"}}},
			}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// The id of a group comes from the server and goes into the body, so a form YouTrack gives no entity is
// the catalogue's word being wrong rather than the caller's name: the refusal names the group and the id it
// arrived under, and nothing is written — a body carrying such an id would be answered 400 Invalid structure of
// entity id, with nothing in it about where the id came from.
func TestTagCreateRefusesAGroupIDItCannotShareTheTagBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "digits without a dash", id: "6"},
		{name: "two dots", id: ".."},
		{name: "digits, a dash and a letter", id: "6-x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			catalogue := "[" + shownGroup(tc.id, "Команда", "NestedGroup") + "]"
			server := sharingATag(t, catalogue, noCreation(t))

			got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "Команда")

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
			assert.Empty(t, got.stdout)
		})
	}
}

// A group is resolved by its name and written by its id, so a catalogue where either of the two is not
// text is one no name of a flag can be read against: the refusal says which member is wrong, and the group is
// named where the name itself is the member that arrived.
func TestTagCreateRefusesACatalogueOfGroupsItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		group string
	}{
		{name: "a name that is a number", group: `{"$type":"NestedGroup","id":"6-1","name":5}`},
		{name: "an id that is a number", group: `{"$type":"NestedGroup","id":7,"name":"Команда"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := sharingATag(t, "["+tc.group+"]", noCreation(t))

			got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "Команда")

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
			assert.Empty(t, got.stdout)
		})
	}
}

// A catalogue that answers with an id no body may carry is worth saying before anything the caller wrote
// is: a name that resolves to nothing is theirs to fix and this is not, so the broken form is what the one
// refusal carries although the very same call also named a group nobody has and one two groups answer to.
func TestTagCreateNamesTheBrokenIDBeforeTheNamesItCouldNotResolve(t *testing.T) {
	t.Parallel()
	catalogue := "[" + strings.Join([]string{
		shownGroup("6", "Своя", "NestedGroup"),
		shownGroup("7-1", "Команда", "NestedGroup"),
		shownGroup("7-2", "команда", "ProjectTeam"),
	}, ",") + "]"
	server := sharingATag(t, catalogue, noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Своя", "--updateable-by", "Нет", "--taggable-by", "КОМАНДА")

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// A 200 says the server took the body, not that the set it kept is the set that went out. The order is the
// server's own, so the two are held together as sets — the very same ids the other way round are the write
// having come out as it went — and a group missing or added is a refusal over a tag that by then exists.
func TestTagCreateHoldsTheSetsItWroteAgainstTheOnesThatCameBack(t *testing.T) {
	t.Parallel()
	const name = "карта"
	tests := []struct {
		name     string
		kept     []sharedGroup
		mismatch []any
	}{
		{
			name: "the same two the other way round",
			kept: []sharedGroup{{id: "101-0", name: `ООО "РОМАШКА", Москва`}, {id: "6-1", name: "DEVELOPMENT Team"}},
		},
		{
			name: "one of the two dropped",
			kept: []sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
			mismatch: []any{[]detail{
				{"field", "readSharingSettings.permittedGroups"},
				{"written", []any{"6-1", "101-0"}},
				{"arrived", []any{"6-1"}},
			}},
		},
		{
			name: "a group nobody wrote standing in the set",
			kept: []sharedGroup{
				{id: "6-1", name: "DEVELOPMENT Team"},
				{id: "101-0", name: `ООО "РОМАШКА", Москва`},
				{id: "6-0", name: "Все пользователи"},
			},
			mismatch: []any{[]detail{
				{"field", "readSharingSettings.permittedGroups"},
				{"written", []any{"6-1", "101-0"}},
				{"arrived", []any{"6-1", "101-0", "6-0"}},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := sharingATag(t, groupsOfTheInstance(),
				answer(http.StatusOK, sharedTag(name, tc.kept, nil)))

			got := runWith(t, server.env(), "tag", "create", "--name", name,
				"--visible-for", "DEVELOPMENT Team", "--visible-for", `ООО "РОМАШКА", Москва`)

			if tc.mismatch == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				return
			}
			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
			assert.Equal(t, name, detailNamed(t, found, "tag"))
			assert.Empty(t, got.stdout)
		})
	}
}

// A set the call never named is never held against anything: the tag YouTrack shares as it pleases is not
// a set that was written, so a creation naming only the readers is answered by a tag anyone may change without
// a word of complaint.
func TestTagCreateHoldsOnlyTheSetsItWrote(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), answer(http.StatusOK, sharedTag("карта",
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
		[]sharedGroup{{id: "6-0", name: "Все пользователи"}})))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "DEVELOPMENT Team")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"name", "readSharingSettings"}, slices.Sorted(maps.Keys(sentBody(t, server))))
}

// A token that may not read the groups of the instance cannot resolve a name to one, so the refusal comes
// where the reading failed and no tag is made: /api/groups needs a right of Hub that a contributor has none of,
// and there is no second way to ask — a list built out of the sharing of visible tags would be a guess.
func TestTagCreateSendsNoWriteWhereTheGroupsWereRefused(t *testing.T) {
	t.Parallel()
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodGet, r.Method, "a creation reached the server") {
			return
		}
		answer(http.StatusForbidden, `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`)(w, r)
	})

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", everyoneRegistered)

	found := requireRefusal(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, detail{"request", groupsRequest(server.url)}, found.details[0])
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	assert.Empty(t, got.stdout)
}

// The polygon takes the set although the specification marks it read-only: the body in the journal
// is what says so, and the tag comes back shared with the group that was named. A token that stands in that
// group is shown the tag afterwards, owned by whoever made it.
func TestTagCreateSharesATagOfThePolygonWithAGroup(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)

	got := runWith(t, dev.env(), "tag", "create", "--name", name, "--visible-for", strings.ToLower(everyoneRegistered))

	record := createdRecord(t, got)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	assert.Equal(t, name, record["name"])
	assert.Equal(t, []string{everyoneRegistered}, permittedNames(t, record, "readSharingSettings"))
	assert.Empty(t, permittedNames(t, record, "updateSharingSettings"))
	assert.Contains(t, lastAsk(dev), `"readSharingSettings"`, "the body carried no set of sharing")

	listed := requireTagListing(t, runWith(t, limited, "tag", "list"))
	at := slices.IndexFunc(listed.Tags, func(record map[string]any) bool { return record["name"] == name })
	require.GreaterOrEqual(t, at, 0, "the shared tag stands in no list of the token it was shared with")
	assert.Equal(t, "admin", ownerOf(t, listed.Tags[at]))
}

// Two groups read the tag and one of them changes it, and the older visibleFor is what cannot say
// so: asked for beside the sets, it carries one of the two groups the server chose and no word of the other.
func TestTagCreateSharesWithSeveralGroupsWhereVisibleForNamesOne(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t)

	got := runWith(t, dev.env(), "tag", "create", "--name", name,
		"--visible-for", "DEVELOPMENT Team", "--visible-for", "Все пользователи",
		"--updateable-by", "DEVELOPMENT Team", "--fields", "+visibleFor(name)")

	record := createdRecord(t, got)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	assert.ElementsMatch(t, []string{"DEVELOPMENT Team", "Все пользователи"},
		permittedNames(t, record, "readSharingSettings"))
	assert.Equal(t, []string{"DEVELOPMENT Team"}, permittedNames(t, record, "updateSharingSettings"))
	shown, isObject := record["visibleFor"].(map[string]any)
	require.True(t, isObject, "visibleFor of %v is no object", record)
	assert.Contains(t, []any{"DEVELOPMENT Team", "Все пользователи"}, shown["name"])
}

// The limited token of the polygon may not read the groups at all, so the flag is out of its reach: the
// refusal is the one the reading earned, one request went out and no tag was made.
func TestTagCreateRefusesTheLimitedTokenTheGroupsOfThePolygon(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}

	got := runWith(t, limited, "tag", "create", "--name", contractTagName(t), "--visible-for", everyoneRegistered)

	found := requireRefusal(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	assert.Equal(t, []string{"/api/groups"}, dev.sentPaths())
}

// A name the polygon has no group under is answered by the reading of the groups and by nothing else: one
// request goes out and no write follows it.
func TestTagCreateRefusesAGroupThePolygonHasNone(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "tag", "create", "--name", contractTagName(t),
		"--visible-for", "ytrack contract no such group")

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	assert.Equal(t, []string{"/api/groups"}, dev.sentPaths())
	unknown, isList := detailNamed(t, found, "unknown").([]any)
	require.True(t, isList, "the refusal named no unknown group")
	assert.Len(t, unknown, 1)
}

// Sharing is what makes one name the name of two tags: the limited token owns one, the admin shares
// another of the very same name with the group it stands in, and from then on the name resolves to neither. The
// server reads the name without regard to letter case against everything the token is shown, so it will not
// make a third one either.
func TestTagCreateMakesANameAmbiguousForTheTokenItIsSharedWith(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)

	own := runWith(t, limited, "tag", "create", "--name", name)
	require.Equal(t, 0, own.code, "stderr: %s", own.stderr)
	t.Cleanup(func() { removeTag(t, limited, name, "dev.limited") })

	shared := runWith(t, dev.env(), "tag", "create", "--name", name, "--visible-for", everyoneRegistered)
	require.Equal(t, 0, shared.code, "stderr: %s", shared.stderr)
	// LIFO, so the admin's tag goes first and the limited token is left shown its own alone.
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	before := len(dev.requests())

	got := runWith(t, limited, "tag", "delete", "--name", name)

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, []any{[]detail{
		{"tag", name},
		{"candidates", []any{
			[]detail{{"name", name}, {"owner", "admin"}},
			[]detail{{"name", name}, {"owner", "dev.limited"}},
		}},
	}}, detailNamed(t, found, "ambiguous"))
	assert.Equal(t, []string{http.MethodGet}, sentMethodsFrom(dev, before))

	again := runWith(t, limited, "tag", "create", "--name", strings.ToUpper(name))
	assert.Equal(t, "rejected", requireRefusal(t, again).code)
}

// Being allowed to change a tag is not being allowed to destroy it: the admin shares one for reading
// and for changing, and the limited token names it, reaches the deletion and is refused there. The tag stands
// in the admin's list afterwards.
func TestTagDeleteRefusesTheTokenAGroupMayOnlyChangeTheTagFor(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)

	made := runWith(t, dev.env(), "tag", "create", "--name", name,
		"--visible-for", everyoneRegistered, "--updateable-by", everyoneRegistered)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	before := len(dev.requests())

	got := runWith(t, limited, "tag", "delete", "--name", name)

	found := requireRefusal(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, name, detailNamed(t, found, "tag"), "the refusal names no tag the caller wrote")
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethodsFrom(dev, before))

	listed := requireTagListing(t, runWith(t, dev.env(), "tag", "list"))
	assert.True(t, slices.ContainsFunc(listed.Tags, func(record map[string]any) bool { return record["name"] == name }),
		"the tag is gone from the list of its owner after a deletion the server refused")
}
