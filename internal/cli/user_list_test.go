package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestUserListAddsFieldsToTheDefaultOfTheList(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"id":"1-1","fullName":"First","$type":"User","banned":false,"login":"first"}]`))

	got := runWith(t, server.Env(), "user", "list", "--query", "fir", "--fields", "+id")

	want := "total: 1\nreturned: 1\ntruncated: false\nusers:\n" + `  - {login: "first", fullName: "First", banned: false, id: "1-1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,banned,id"}, "$top": {"50"}, "query": {"fir"}}}, server.Queries())
}
