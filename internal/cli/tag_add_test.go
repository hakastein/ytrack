package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const tagsCollection = "/api/tags"

func addingATag(t *testing.T, owner, catalogue, tagging http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			tagging(w, r)
		case r.URL.Path == tagsCollection:
			catalogue(w, r)
		default:
			owner(w, r)
		}
	})
}

func shownTags() http.HandlerFunc {
	return fake.JSON(http.StatusOK, tagsOfTwoOwners())
}

func TestTagAddPrintsTheTagAddedToTheOwner(t *testing.T) {
	t.Parallel()
	server := addingATag(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		fake.JSON(http.StatusOK, catalogueTag("10-5", "Ready", "first")))

	got := runWith(t, envOf(server), "tag", "add", "DEV-7", "--name", "Ready")

	want := "idReadable: \"DEV-7\"\n" + "added:\n  name: \"Ready\"\n  owner:\n    login: \"first\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "POST /api/issues/DEV-7/tags")
}
