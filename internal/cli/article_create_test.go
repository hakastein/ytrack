package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestArticleCreateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no title", argv: []string{"article", "create", "DEV"}},
		{name: "empty content", argv: []string{"article", "create", "DEV", "--summary", "Title", "--content", ""}},
		{name: "a parent of no id", argv: []string{"article", "create", "DEV", "--summary", "Title", "--parent", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleCreatePrintsTheArticleTheServerFiled(t *testing.T) {
	t.Parallel()
	const filed = `{"$type":"Article","idReadable":"DEV-A-7","summary":"Title","content":"Text",` +
		`"project":{"$type":"Project","shortName":"DEV"}}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, filed))

	got := runWith(t, envOf(server), "article", "create", "DEV", "--summary", "Title", "--content", "Text",
		"--fields", "idReadable,summary,content")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-A-7\"\nsummary: \"Title\"\ncontent: |-\n  Text\n"}, got)
	assert.Contains(t, server.Routes(), "POST /api/articles")
}
