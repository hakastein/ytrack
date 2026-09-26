package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func catalogueTag(id, name, owner string) string {
	return `{"$type":"Tag","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) +
		`,"owner":{"$type":"User","login":` + strconv.Quote(owner) + `}}`
}

func tagsOfTwoOwners() string {
	return "[" + strings.Join([]string{
		catalogueTag("10-5", "Ready", "first"),
		catalogueTag("10-6", "Shared", "first"),
		catalogueTag("10-7", "Shared", "second"),
	}, ",") + "]"
}

func resolvingTags(t *testing.T, catalogue string, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, readThenDeletion(fake.JSON(http.StatusOK, catalogue), deletion))
}

func TestTagDeletePrintsTheDeletedTag(t *testing.T) {
	t.Parallel()
	server := resolvingTags(t, tagsOfTwoOwners(), deletionDone())

	got := runWith(t, envOf(server), "tag", "delete", "--name", "Ready")

	want := `name: "Ready"` + "\n" + "owner:\n" + `  login: "first"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "DELETE /api/tags/10-5")
}
