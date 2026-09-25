package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const deletedFields = "idReadable"

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

func entityNotFound(id string) string {
	return `{"error":"Not Found","error_description":"Entity with id ` + id + ` not found"}`
}

func TestIssueDeleteReadsTheIDAndDeletesByIt(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), deletionDone())

	got := runWith(t, server.Env(), "issue", "delete", "dev-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, server.Methods())
	assert.Equal(t, []string{"/api/issues/dev-7?fields=" + deletedFields, "/api/issues/DEV-7?"}, server.Targets(t))
	assert.Equal(t, "Bearer "+fake.Token, server.Last(t).Header.Get("Authorization"))
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestIssueDeleteRefusesAReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","idReadable":"DEV-7/.."}`
	server := deleting(t, fake.JSON(http.StatusOK, body), fake.Unexpected(t))

	got := runWith(t, server.Env(), "issue", "delete", "dev-7")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", issueRequest(server.URL, "dev-7", deletedFields)},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}
