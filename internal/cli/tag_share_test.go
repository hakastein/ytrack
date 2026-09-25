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
