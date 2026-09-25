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

const shownGroupFields = "id,name"

func groupsRequest(address string) string {
	return "GET " + address + "/api/groups?fields=" + shownGroupFields + "&$top=-1"
}

const everyoneRegistered = "Зарегистрированные пользователи"

func catalogueGroup(id, name, kind string) string {
	return `{"$type":` + strconv.Quote(kind) + `,"id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) + `}`
}

const groupWithAComma = `ООО "РОМАШКА", Москва`

func everyGroupName() []any {
	return []any{"DEVELOPMENT Team", "Все пользователи", groupWithAComma}
}

func groupsOfTheInstance() string {
	return "[" + strings.Join([]string{
		catalogueGroup("6-1", "DEVELOPMENT Team", "ProjectTeam"),
		catalogueGroup("6-0", "Все пользователи", "AllUsersGroup"),
		catalogueGroup("101-0", groupWithAComma, "NestedGroup"),
	}, ",") + "]"
}

type sharedGroup struct {
	id   string
	name string
}

func sharedSetOf(kind string, groups []sharedGroup) string {
	items := make([]string, 0, len(groups))
	for _, group := range groups {
		items = append(items, catalogueGroup(group.id, group.name, "NestedGroup"))
	}
	return `{"$type":` + strconv.Quote(kind) + `,"permittedGroups":[` + strings.Join(items, ",") +
		`],"permittedUsers":[]}`
}

func sharingOf(groups []sharedGroup) string {
	return sharedSetOf("WatchFolderSharingSettings", groups)
}

func taggableBy(groups []sharedGroup) string {
	return sharedSetOf("TagSharingSettings", groups)
}

func sharedTag(name string, read, update []sharedGroup) string {
	return `{"$type":"Tag","name":` + asJSON(name) + `,"owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":` + sharingOf(read) + `,"updateSharingSettings":` + sharingOf(update) +
		`,"tagSharingSettings":` + taggableBy(nil) + `}`
}

func sharingATag(t *testing.T, catalogue string, creation http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creation(w, r)
			return
		}
		respondWith(http.StatusOK, catalogue)(w, r)
	})
}

func sentBody(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body))
	return body
}

func sentMethodsFrom(u *upstream, at int) []string {
	return sentMethods(u)[at:]
}

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

func createdRecord(t *testing.T, got outcome) map[string]any {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	var record map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(got.stdout), &record), "stdout: %s", got.stdout)
	return record
}

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

			want := faultDocument{code: "bad_usage"}
			assert.Equal(t, want, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTagCreateHelpNamesTheSharingFlags(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"tag", "create", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	for _, flag := range []string{"--visible-for", "--updateable-by", "--taggable-by"} {
		assert.Contains(t, got.stdout, flag)
	}
}

func TestTagCreateWritesTheNamedGroupsAsTheTwoSets(t *testing.T) {
	t.Parallel()
	const name = "карта"
	server := sharingATag(t, groupsOfTheInstance(), respondWith(http.StatusOK, sharedTag(name,
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}, {id: "101-0", name: groupWithAComma}},
		[]sharedGroup{{id: "6-0", name: "Все пользователи"}})))

	got := runWith(t, server.env(), "tag", "create", "--name", name,
		"--visible-for", "development team",
		"--visible-for", groupWithAComma,
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
	}, sentBody(t, server), "visibleFor and updateableBy hold a single group")
}

func TestTagCreateAsksForTheIDsItChecksAndPrintsTheNames(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), respondWith(http.StatusOK, sharedTag("карта",
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

func TestTagCreateRefusesEveryGroupItCannotResolveAtOnce(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Нет", "--updateable-by", "Тоже")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.url)},
			{"unknown", []any{
				[]detail{{"group", "Нет"}, {"nearest", everyGroupName()}},
				[]detail{{"group", "Тоже"}, {"nearest", everyGroupName()}},
			}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTagCreateRefusesAGroupNameMoreThanOneGroupAnswersTo(t *testing.T) {
	t.Parallel()
	const catalogue = `[{"$type":"NestedGroup","id":"6-1","name":"Команда"},` +
		`{"$type":"ProjectTeam","id":"6-2","name":"команда"}]`
	server := sharingATag(t, catalogue, noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "КОМАНДА")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.url)},
			{"ambiguous", []any{[]detail{
				{"group", "КОМАНДА"},
				{"candidates", []any{"Команда", "команда"}},
			}}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func groupsNamedAlike() string {
	return "[" + strings.Join([]string{
		catalogueGroup("6-1", "DEVELOPMENT Team", "ProjectTeam"),
		catalogueGroup("6-0", "Все пользователи", "AllUsersGroup"),
		catalogueGroup("7-1", "Команда", "NestedGroup"),
		catalogueGroup("7-2", "команда", "ProjectTeam"),
	}, ",") + "]"
}

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
			server := sharingATag(t, groupsNamedAlike(), respondWith(http.StatusOK, sharedTag("карта",
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

func TestTagCreateRefusesTheUnknownGroupsAndTheAmbiguousOnesTogether(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsNamedAlike(), noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Нет", "--updateable-by", "КОМАНДА")

	want := faultDocument{
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
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

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
			catalogue := "[" + catalogueGroup(tc.id, "Команда", "NestedGroup") + "]"
			server := sharingATag(t, catalogue, noCreation(t))

			got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "Команда")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server),
				"the server answers a malformed id with 400 that does not say where the id came from")
			assert.Empty(t, got.stdout)
		})
	}
}

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

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestTagCreateNamesTheBrokenIDBeforeTheNamesItCouldNotResolve(t *testing.T) {
	t.Parallel()
	catalogue := "[" + strings.Join([]string{
		catalogueGroup("6", "Своя", "NestedGroup"),
		catalogueGroup("7-1", "Команда", "NestedGroup"),
		catalogueGroup("7-2", "команда", "ProjectTeam"),
	}, ",") + "]"
	server := sharingATag(t, catalogue, noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Своя", "--updateable-by", "Нет", "--taggable-by", "КОМАНДА")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTagCreateChecksTheSetsItWroteAgainstTheOnesThatCameBack(t *testing.T) {
	t.Parallel()
	const name = "карта"
	tests := []struct {
		name     string
		kept     []sharedGroup
		mismatch []any
	}{
		{
			name: "the same two the other way round",
			kept: []sharedGroup{{id: "101-0", name: groupWithAComma}, {id: "6-1", name: "DEVELOPMENT Team"}},
		},
		{
			name: "one of the two dropped",
			kept: []sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
			mismatch: []any{[]detail{
				{"field", "readSharingSettings.permittedGroups"},
				{"expected", []any{"6-1", "101-0"}},
				{"actual", []any{"6-1"}},
			}},
		},
		{
			name: "a group nobody wrote standing in the set",
			kept: []sharedGroup{
				{id: "6-1", name: "DEVELOPMENT Team"},
				{id: "101-0", name: groupWithAComma},
				{id: "6-0", name: "Все пользователи"},
			},
			mismatch: []any{[]detail{
				{"field", "readSharingSettings.permittedGroups"},
				{"expected", []any{"6-1", "101-0"}},
				{"actual", []any{"6-1", "101-0", "6-0"}},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := sharingATag(t, groupsOfTheInstance(),
				respondWith(http.StatusOK, sharedTag(name, tc.kept, nil)))

			got := runWith(t, server.env(), "tag", "create", "--name", name,
				"--visible-for", "DEVELOPMENT Team", "--visible-for", groupWithAComma)

			if tc.mismatch == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				return
			}
			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
			assert.Equal(t, name, detailNamed(t, found, "tag"))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestTagCreateChecksOnlyTheSetsItWrote(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), respondWith(http.StatusOK, sharedTag("карта",
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
		[]sharedGroup{{id: "6-0", name: "Все пользователи"}})))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "DEVELOPMENT Team")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"name", "readSharingSettings"}, slices.Sorted(maps.Keys(sentBody(t, server))))
}

func TestTagCreateSendsNoWriteWhereTheGroupsWereRefused(t *testing.T) {
	t.Parallel()
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodGet, r.Method, "a creation reached the server") {
			return
		}
		respondWith(http.StatusForbidden, `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`)(w, r)
	})

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", everyoneRegistered)

	found := requireFault(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, detail{"request", groupsRequest(server.url)}, found.details[0])
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	assert.Empty(t, got.stdout)
}

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

func TestTagCreateRefusesTheLimitedTokenTheGroupsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}

	got := runWith(t, limited, "tag", "create", "--name", contractTagName(t), "--visible-for", everyoneRegistered)

	found := requireFault(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	assert.Equal(t, []string{"/api/groups"}, dev.sentPaths())
}

func TestTagCreateRefusesAGroupTheDevInstanceHasNone(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "tag", "create", "--name", contractTagName(t),
		"--visible-for", "ytrack contract no such group")

	found := requireFault(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	assert.Equal(t, []string{"/api/groups"}, dev.sentPaths())
	unknown, isList := detailNamed(t, found, "unknown").([]any)
	require.True(t, isList, "the refusal named no unknown group")
	assert.Len(t, unknown, 1)
}

func TestTagCreateMakesANameAmbiguousForTheTokenItIsSharedWith(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)

	own := runWith(t, limited, "tag", "create", "--name", name)
	require.Equal(t, 0, own.code, "stderr: %s", own.stderr)
	t.Cleanup(func() { removeTag(t, limited, name, "dev.limited") })

	ambiguousForTheLimited := runWith(t, dev.env(), "tag", "create", "--name", name, "--visible-for", everyoneRegistered)
	require.Equal(t, 0, ambiguousForTheLimited.code, "stderr: %s", ambiguousForTheLimited.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	before := len(dev.requests())

	got := runWith(t, limited, "tag", "delete", "--name", name)

	found := requireFault(t, got)
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
	assert.Equal(t, "rejected", requireFault(t, again).code,
		"the server checks a new name ignoring case against the tags shared with the token too")
}

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

	found := requireFault(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, name, detailNamed(t, found, "tag"), "the refusal names no tag the caller wrote")
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethodsFrom(dev, before))

	listed := requireTagListing(t, runWith(t, dev.env(), "tag", "list"))
	assert.True(t, slices.ContainsFunc(listed.Tags, func(record map[string]any) bool { return record["name"] == name }),
		"the tag is gone from the list of its owner after a deletion the server refused")
}
