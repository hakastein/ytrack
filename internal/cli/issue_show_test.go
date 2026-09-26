package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestIssueShowPrintsTheIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","comments":[]}`))

	got := runWith(t, envOf(server), "issue", "show", "DEV-1", "--fields", "idReadable")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\ncomments: []\n"}, got)
	sent := server.Last(t)
	assert.Equal(t, http.MethodGet, sent.Method)
	assert.Equal(t, "/api/issues/DEV-1", sent.URL.Path)
}
