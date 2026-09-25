package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestProjectShowPrintsAListOneFlowItemALine(t *testing.T) {
	t.Parallel()
	const body = `{"shortName": "DEV", "team": {"name": "DEV Team", "users": [` +
		`{"login": "admin", "banned": false, "online": 1, "profile": {}, "groups": [{"name": "All Users"}, {"name": "DEV \"Team\""}], "tags": []}, ` +
		`{"login": "dev.member", "banned": true, "online": null, "profile": null, "groups": [], "tags": ["a", "b"]}]}, ` +
		`"watchers": [], "codes": [1, null, "DEV", true, [], {}]}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "project", "show", "DEV", "--fields", "shortName,team(name,users(login,banned,online,profile,groups(name),tags)),watchers,codes")

	const want = `shortName: "DEV"
team:
  name: "DEV Team"
  users:
    - {login: "admin", banned: false, online: 1, profile: {}, groups: [{name: "All Users"}, {name: "DEV \"Team\""}], tags: []}
    - {login: "dev.member", banned: true, online: null, profile: null, groups: [], tags: ["a", "b"]}
watchers: []
codes:
  - 1
  - null
  - "DEV"
  - true
  - []
  - {}
`
	assert.Equal(t, outcome{stdout: want}, got)
	var printed, received any
	require.NoError(t, yaml.Unmarshal([]byte(got.stdout), &printed))
	require.NoError(t, yaml.Unmarshal([]byte(body), &received))
	assert.Equal(t, received, printed)
}
