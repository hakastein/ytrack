package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func articleNamed(readable string) string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(readable) + `}`
}

func TestArticleDeletePrintsTheDeletedArticle(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, articleNamed("DEV-A-7")), deletionDone())

	got := runWith(t, envOf(server), "article", "delete", "DEV-A-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-A-7\"\n"}, got)
	assert.Contains(t, server.Routes(), "DELETE /api/articles/DEV-A-7")
}
