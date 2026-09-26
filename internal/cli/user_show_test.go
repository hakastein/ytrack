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

func TestUserShowFieldsFlagAddsToItsDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		given []string
		want  string
	}{
		{name: "none given", want: "login,fullName,email,banned"},
		{name: "an expression", given: []string{"--fields", "login"}, want: "login"},
		{name: "an expression added to the default", given: []string{"--fields", "+ringId"}, want: "login,fullName,email,banned,ringId"},
		{name: "an empty expression", given: []string{"--fields", ""}, want: "login,fullName,email,banned"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, shownUser))

			runWith(t, envOf(server), append([]string{"user", "show", "first"}, tc.given...)...)

			assert.Equal(t, tc.want, server.Last(t).URL.Query().Get("fields"))
		})
	}
}
