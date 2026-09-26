package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestTagListPrintsTheTagsTheServerListed(t *testing.T) {
	t.Parallel()
	const records = `[{"$type":"Tag","owner":{"$type":"User","login":"first"},"name":"[bug] fix login",` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedUsers":[],"permittedGroups":[]}},` +
		`{"readSharingSettings":{"permittedGroups":[{"$type":"UserGroup","name":"First"},` +
		`{"$type":"UserGroup","name":"Second"}],` +
		`"permittedUsers":[{"login":"third","$type":"User"}],"$type":"WatchFolderSharingSettings"},` +
		`"name":"Early","$type":"Tag","owner":{"login":"second","$type":"User"}}]`
	server := fake.Serve(t, fake.JSON(http.StatusOK, records))

	got := runWith(t, envOf(server), "tag", "list")

	want := "total: 2\nreturned: 2\ntruncated: false\ntags:\n" +
		`  - {name: "[bug] fix login", owner: {login: "first"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n" +
		`  - {name: "Early", owner: {login: "second"}, readSharingSettings: {permittedGroups: [{name: "First"}, {name: "Second"}], permittedUsers: [{login: "third"}]}}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "GET /api/tags")
}
