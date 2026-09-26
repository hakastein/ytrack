package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const (
	listedIssueComment = `{"deleted":false,"author":{"login":"admin","$type":"User"},"created":1789395789677,` +
		`"text":"First\nSecond","id":"7-2","$type":"IssueComment"}`
	listedDeletedComment = `{"deleted":true,"author":{"login":"member","$type":"User"},` +
		`"created":1789395790000,"text":null,"id":"7-3","$type":"IssueComment"}`
)

func TestCommentListPrintsTheCommentsOfTheOwner(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedIssueComment+","+listedDeletedComment+"]"))

	got := runWith(t, envOf(server), "comment", "list", "DEV-7")

	want := "total: 2\nreturned: 2\ntruncated: false\ncomments:\n" +
		`  - {id: "7-2", author: {login: "admin"}, created: "2026-09-14T14:23:09.677Z", text: "First\nSecond", deleted: false}` + "\n" +
		`  - {id: "7-3", author: {login: "member"}, created: "2026-09-14T14:23:10Z", text: null, deleted: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "GET /api/issues/DEV-7/comments")
}
