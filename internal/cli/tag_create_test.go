package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func catalogueGroup(id, name, kind string) string {
	return `{"$type":` + strconv.Quote(kind) + `,"id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) + `}`
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

func TestTagCreatePrintsTheTagTheServerMade(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), fake.JSON(http.StatusOK, sharedTagOf("[bug] fix login",
		[]sharedGroup{{id: "6-1", name: "First"}},
		[]sharedGroup{{id: "6-2", name: "Second"}},
		[]sharedGroup{{id: "6-3", name: "Third"}})))

	got := runWith(t, envOf(server), "tag", "create", "--name", "[bug] fix login",
		"--visible-for", "First", "--updateable-by", "Second", "--taggable-by", "Third")

	want := `name: "[bug] fix login"` + "\n" +
		"owner:\n  login: \"admin\"\n" +
		"readSharingSettings:\n  permittedGroups:\n    - {name: \"First\"}\n  permittedUsers: []\n" +
		"updateSharingSettings:\n  permittedGroups:\n    - {name: \"Second\"}\n  permittedUsers: []\n" +
		"tagSharingSettings:\n  permittedGroups:\n    - {name: \"Third\"}\n  permittedUsers: []\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "POST /api/tags")
	written := server.LastJSON(t)
	assert.Equal(t, groupWritten("6-1"), written["readSharingSettings"], "--visible-for")
	assert.Equal(t, groupWritten("6-2"), written["updateSharingSettings"], "--updateable-by")
	assert.Equal(t, groupWritten("6-3"), written["tagSharingSettings"], "--taggable-by")
}

func groupWritten(id string) map[string]any {
	return map[string]any{"permittedGroups": []any{map[string]any{"id": id}}}
}
