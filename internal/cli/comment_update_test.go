package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func commentDeletedState(gone bool) string {
	return `{"$type":"IssueComment","deleted":` + strconv.FormatBool(gone) + `}`
}

func updatingAComment(t *testing.T, read, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			read(w, r)
			return
		}
		write(w, r)
	})
}

func TestCommentUpdatePrintsTheCommentTheServerWrote(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
		fake.JSON(http.StatusOK, createdComment("7-12", "Text")))

	got := runWith(t, envOf(server), "comment", "update", "DEV-7", "7-12", "--text", "Text")

	assert.Equal(t, outcome{stdout: printedComment}, got)
	assert.Contains(t, server.Routes(), "POST /api/issues/DEV-7/comments/7-12")
}
