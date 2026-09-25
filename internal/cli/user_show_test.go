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

func TestUserShowPrintsTheFieldsAskedInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, shownUser))

	got := runWith(t, server.Env(), "user", "show", "first")

	assert.Equal(t, outcome{stdout: printedUser}, got)
	assert.Equal(t, []string{"/api/users/first"}, server.Paths())
	request := server.Last(t)
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, url.Values{"fields": {"login,fullName,email,banned"}}, request.URL.Query())
}

func TestUserShowAddsFieldsToTheDefaultOfTheCommand(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, shownUser))

	got := runWith(t, server.Env(), "user", "show", "first", "--fields", "+id")

	assert.Equal(t, outcome{stdout: printedUser + `id: "1-1"` + "\n"}, got)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,email,banned,id"}}}, server.Queries())
}

func TestUserShowRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "user", "show", "first", "--fields", "+")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}
