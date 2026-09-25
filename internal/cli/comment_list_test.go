package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const issueCommentListFields = "id,author(login),created,text,deleted"

const (
	listedIssueComment = `{"deleted":false,"author":{"login":"admin","$type":"User"},"created":1789395789677,` +
		`"text":"First\nSecond","id":"7-2","$type":"IssueComment"}`
	listedDeletedComment = `{"deleted":true,"author":{"login":"member","$type":"User"},` +
		`"created":1789395790000,"text":null,"id":"7-3","$type":"IssueComment"}`
)

func TestCommentListPrintsTheCommentsOfAnIssueWithDeletedOnesAmongThem(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedIssueComment+","+listedDeletedComment+"]"))

	got := runWith(t, server.Env(), "comment", "list", "DEV-7")

	want := "total: 2\nreturned: 2\ntruncated: false\ncomments:\n" +
		`  - {id: "7-2", author: {login: "admin"}, created: "2026-09-14T14:23:09.677Z", text: "First\nSecond", deleted: false}` + "\n" +
		`  - {id: "7-3", author: {login: "member"}, created: "2026-09-14T14:23:10Z", text: null, deleted: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/issues/DEV-7/comments"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {issueCommentListFields}, "$top": {"50"}}}, server.Queries())
}
