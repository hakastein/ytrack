package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestTagCreateWritesTheGroupOfEachFlagToItsSet(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), fake.JSON(http.StatusOK, sharedTagOf("Early",
		[]sharedGroup{{id: "6-1", name: "First"}},
		[]sharedGroup{{id: "6-2", name: "Second"}},
		[]sharedGroup{{id: "6-3", name: "Third"}})))

	got := runWith(t, envOf(server), "tag", "create", "--name", "Early",
		"--visible-for", "First", "--updateable-by", "Second", "--taggable-by", "Third", "--fields", "name")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, map[string]any{
		"name":                  "Early",
		"readSharingSettings":   map[string]any{"permittedGroups": []any{map[string]any{"id": "6-1"}}},
		"updateSharingSettings": map[string]any{"permittedGroups": []any{map[string]any{"id": "6-2"}}},
		"tagSharingSettings":    map[string]any{"permittedGroups": []any{map[string]any{"id": "6-3"}}},
	}, server.LastJSON(t))
}
