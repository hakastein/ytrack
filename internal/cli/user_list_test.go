package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestUserListPrintsTheUsersOfTheSearch(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"login":"first","id":"1-1","$type":"User"}]`))

	got := runWith(t, envOf(server), "user", "list", "--query", "fir", "--fields", "login,id")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + `  - {login: "first", id: "1-1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), http.MethodGet+" /api/users")
	assert.Equal(t, "fir", server.Last(t).URL.Query().Get("query"))
}
