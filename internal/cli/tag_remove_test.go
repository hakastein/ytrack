package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func takingATagOff(t *testing.T, owner, catalogue, removal http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			removal(w, r)
		case r.URL.Path == tagsCollection:
			catalogue(w, r)
		default:
			owner(w, r)
		}
	})
}

func TestTagRemovePrintsTheTagTakenOffTheOwner(t *testing.T) {
	t.Parallel()
	server := takingATagOff(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(), deletionDone())

	got := runWith(t, envOf(server), "tag", "remove", "DEV-7", "--name", "Ready")

	want := "idReadable: \"DEV-7\"\n" + "removed:\n  name: \"Ready\"\n  owner:\n    login: \"first\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "DELETE /api/issues/DEV-7/tags/10-5")
}
