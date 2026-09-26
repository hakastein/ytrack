package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const shownUser = `{"banned":false,"$type":"User","email":"first@example.com","fullName":"First","login":"first"}`

const printedUser = `login: "first"
fullName: "First"
email: "first@example.com"
banned: false
`

func TestUserShowPrintsTheUserOfTheLogin(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, shownUser))

	got := runWith(t, envOf(server), "user", "show", "first")

	assert.Equal(t, outcome{stdout: printedUser}, got)
	assert.Contains(t, server.Routes(), http.MethodGet+" /api/users/first")
}
