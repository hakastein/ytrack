package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const shownUser = `{"banned":false,"$type":"User","email":"first@example.com","id":"1-1",` +
	`"name":"First","fullName":"First","login":"first"}`

const printedUser = `login: "first"
fullName: "First"
email: "first@example.com"
banned: false
`

func TestUserShowAddsFieldsToTheDefaultOfTheCommand(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, shownUser))

	got := runWith(t, server.Env(), "user", "show", "first", "--fields", "+id")

	assert.Equal(t, outcome{stdout: printedUser + `id: "1-1"` + "\n"}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,email,banned,id"}}}, server.Queries())
}
