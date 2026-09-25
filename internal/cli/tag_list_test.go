package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const tagFields = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))"

func tagsRequest(address, fields, top string) string {
	return "GET " + address + "/api/tags?fields=" + fields + "&$top=" + top
}

func TestTagListPrintsTheRecordsAsTheyWereAskedFor(t *testing.T) {
	t.Parallel()
	const records = `[{"$type":"Tag","owner":{"$type":"User","login":"first"},"name":"[bug] fix login",` +
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedUsers":[],"permittedGroups":[]}},` +
		`{"readSharingSettings":{"permittedGroups":[{"$type":"UserGroup","name":"First"},` +
		`{"$type":"UserGroup","name":"Second"}],` +
		`"permittedUsers":[{"login":"third","$type":"User"}],"$type":"WatchFolderSharingSettings"},` +
		`"name":"Early","$type":"Tag","owner":{"login":"second","$type":"User"}}]`
	server := fake.Serve(t, fake.JSON(http.StatusOK, records))

	got := runWith(t, server.Env(), "tag", "list")

	want := "total: 2\nreturned: 2\ntruncated: false\ntags:\n" +
		`  - {name: "[bug] fix login", owner: {login: "first"}, readSharingSettings: {permittedGroups: [], permittedUsers: []}}` + "\n" +
		`  - {name: "Early", owner: {login: "second"}, readSharingSettings: {permittedGroups: [{name: "First"}, {name: "Second"}], permittedUsers: [{login: "third"}]}}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/tags"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {tagFields}, "$top": {"50"}}}, server.Queries())
}
