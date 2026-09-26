package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func issueNamed(readable string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `}`
}

func readThenDeletion(read, deletion http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletion(w, r)
			return
		}
		read(w, r)
	}
}

func deleting(t *testing.T, read, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, readThenDeletion(read, deletion))
}

func deletionDone() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
}

func TestIssueDeletePrintsTheDeletedIssue(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), deletionDone())

	got := runWith(t, envOf(server), "issue", "delete", "DEV-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\n"}, got)
	sent := server.Last(t)
	assert.Equal(t, http.MethodDelete, sent.Method)
	assert.Equal(t, "/api/issues/DEV-7", sent.URL.Path)
}
