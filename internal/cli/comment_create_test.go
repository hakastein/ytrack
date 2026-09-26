package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func answeredComment(schema, id, text string) string {
	return `{"$type":` + strconv.Quote(schema) + `,"id":` + strconv.Quote(id) +
		`,"author":{"$type":"User","login":"admin"},"created":1789035410875,"updated":null,"text":` + asJSON(text) + `}`
}

func createdComment(id, text string) string {
	return answeredComment("IssueComment", id, text)
}

const printedComment = `id: "7-12"` + "\n" + "author:\n" + `  login: "admin"` + "\n" +
	`created: "2026-09-10T10:16:50.875Z"` + "\n" + "updated: null\n" + "text: |-\n  Text\n"

func TestCommentCreatePrintsTheCommentTheServerWrote(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, createdComment("7-12", "Text")))

	got := runWith(t, envOf(server), "comment", "create", "DEV-7", "--text", "Text")

	assert.Equal(t, outcome{stdout: printedComment}, got)
	assert.Contains(t, server.Routes(), "POST /api/issues/DEV-7/comments")
}
