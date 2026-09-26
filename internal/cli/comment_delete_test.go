package cli_test

import (
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestCommentDeletePrintsTheIDOfTheDeletedComment(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, deletionDone())

	got := runWith(t, envOf(server), "comment", "delete", "DEV-7", "7-12")

	assert.Equal(t, outcome{stdout: "id: \"7-12\"\n"}, got)
	assert.Contains(t, server.Routes(), "DELETE /api/issues/DEV-7/comments/7-12")
}
