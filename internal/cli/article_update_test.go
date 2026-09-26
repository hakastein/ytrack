package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func articleOfDEVToWrite(id, readable string) string {
	return `{"$type":"Article","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":"DEV"}}`
}

func updatingAnArticle(t *testing.T, read, update http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			read(w, r)
			return
		}
		if !assert.Equal(t, http.MethodPost, r.Method, "an update sends one GET and one POST") {
			return
		}
		update(w, r)
	})
}

func TestArticleUpdateRefusesAPartItCannotEmptyBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		part string
	}{
		{name: "a word that names no part", part: "bogus"},
		{name: "the title every article holds", part: "summary"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), "article", "update", "DEV-A-7", "--clear", tc.part)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleUpdatePrintsTheArticleTheServerWrote(t *testing.T) {
	t.Parallel()
	const written = `{"$type":"Article","idReadable":"DEV-A-7","summary":"Title",` +
		`"project":{"$type":"Project","shortName":"DEV"}}`
	server := updatingAnArticle(t,
		fake.JSON(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		fake.JSON(http.StatusOK, written))

	got := runWith(t, envOf(server), "article", "update", "DEV-A-7", "--summary", "Title",
		"--fields", "idReadable,summary")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-A-7\"\nsummary: \"Title\"\n"}, got)
	assert.Contains(t, server.Routes(), "POST /api/articles/DEV-A-7")
}
