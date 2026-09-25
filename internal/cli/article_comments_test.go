package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const articleCommentFields = "comments(id,author(login),created,text)"

const sentArticleFields = articleShowFields + "," + articleCommentFields

func TestArticleShowRefusesCommentsOfAnotherShape(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Article","idReadable":"DEV-A-1","comments":null}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "article", "show", "DEV-A-1", "--fields", "idReadable")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "GET " + server.URL + "/api/articles/DEV-A-1?fields=idReadable," + articleCommentFields},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}
