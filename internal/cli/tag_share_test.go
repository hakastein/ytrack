package cli_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const shownGroupFields = "id,name"

func groupsRequest(address string) string {
	return "GET " + address + "/api/groups?fields=" + shownGroupFields + "&$top=-1"
}

func catalogueGroup(id, name, kind string) string {
	return `{"$type":` + strconv.Quote(kind) + `,"id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) + `}`
}

func everyGroupName() []any {
	return []any{"First", "Second", "Third"}
}

func groupsOfTheInstance() string {
	return "[" + strings.Join([]string{
		catalogueGroup("6-1", "First", "UserGroup"),
		catalogueGroup("6-2", "Second", "UserGroup"),
		catalogueGroup("6-3", "Third", "UserGroup"),
	}, ",") + "]"
}

type sharedGroup struct {
	id   string
	name string
}

func sharedSetOf(kind string, groups []sharedGroup) string {
	items := make([]string, 0, len(groups))
	for _, group := range groups {
		items = append(items, catalogueGroup(group.id, group.name, "UserGroup"))
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
	return sharedTagOf(name, read, update, nil)
}

func sharedTagOf(name string, read, update, tagging []sharedGroup) string {
	return `{"$type":"Tag","name":` + asJSON(name) + `,"owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":` + sharingOf(read) + `,"updateSharingSettings":` + sharingOf(update) +
		`,"tagSharingSettings":` + taggableBy(tagging) + `}`
}

func sharingATag(t *testing.T, catalogue string, creation http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creation(w, r)
			return
		}
		fake.JSON(http.StatusOK, catalogue)(w, r)
	})
}

func sentBody(t *testing.T, u *fake.Server) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(u.Last(t).Body), &body))
	return body
}

func TestTagCreateWritesTheGroupOfEachFlagToItsSet(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), fake.JSON(http.StatusOK, sharedTagOf("Early",
		[]sharedGroup{{id: "6-1", name: "First"}},
		[]sharedGroup{{id: "6-2", name: "Second"}},
		[]sharedGroup{{id: "6-3", name: "Third"}})))

	got := runWith(t, server.Env(), "tag", "create", "--name", "Early",
		"--visible-for", "First", "--updateable-by", "Second", "--taggable-by", "Third")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/groups", "/api/tags"}, server.Paths())
	assert.Equal(t, map[string]any{
		"name":                  "Early",
		"readSharingSettings":   map[string]any{"permittedGroups": []any{map[string]any{"id": "6-1"}}},
		"updateSharingSettings": map[string]any{"permittedGroups": []any{map[string]any{"id": "6-2"}}},
		"tagSharingSettings":    map[string]any{"permittedGroups": []any{map[string]any{"id": "6-3"}}},
	}, sentBody(t, server))
}

func TestTagCreateRefusesEveryGroupItCannotResolveAtOnce(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), noCreation(t))

	got := runWith(t, server.Env(), "tag", "create", "--name", "Early",
		"--visible-for", "Nobody", "--updateable-by", "None")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.URL)},
			{"unknown", []any{
				[]detail{{"group", "Nobody"}, {"nearest", everyGroupName()}},
				[]detail{{"group", "None"}, {"nearest", everyGroupName()}},
			}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTagCreateRefusesAGroupIDItCannotShareTheTagBy(t *testing.T) {
	t.Parallel()
	catalogue := "[" + catalogueGroup("..", "First", "UserGroup") + "]"
	server := sharingATag(t, catalogue, noCreation(t))

	got := runWith(t, server.Env(), "tag", "create", "--name", "Early", "--visible-for", "First")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", groupsRequest(server.URL)},
			{"upstream_status", 200},
			{"upstream_body", catalogue},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server),
		"the server answers a malformed id with 400 that does not say where the id came from")
}

func TestTagCreateSendsNoWriteWhereTheGroupsWereRefused(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodGet, r.Method, "a creation reached the server") {
			return
		}
		fake.JSON(http.StatusForbidden, `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`)(w, r)
	})

	got := runWith(t, server.Env(), "tag", "create", "--name", "Early", "--visible-for", "First")

	found := requireFault(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, detail{"request", groupsRequest(server.URL)}, found.details[0])
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	assert.Empty(t, got.stdout)
}
