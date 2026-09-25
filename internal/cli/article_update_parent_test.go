package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const ancestorsPerRequest = 10

func articleLineFields() string {
	line := "parentArticle(id,idReadable)"
	for range ancestorsPerRequest - 1 {
		line = "parentArticle(id,idReadable," + line + ")"
	}
	return articleToWriteFields + "," + line
}

func articleLineRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + articleLineFields()
}

func movingAnArticle(t *testing.T, reads map[string]http.HandlerFunc, update http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			update(w, r)
			return
		}
		if !assert.Equal(t, http.MethodGet, r.Method, "an update reads and writes and does nothing else") {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/articles/")
		read, expected := reads[id]
		if !assert.True(t, expected, "the article %q was read and no answer was given for it", id) {
			return
		}
		read(w, r)
	})
}

func TestArticleUpdateRefusesAParentBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the id of an issue for a parent", argv: []string{"--parent", "DEV-1"}},
		{name: "an internal id for a parent", argv: []string{"--parent", "177-1"}},
		{name: "the marker of the parent in lower case", argv: []string{"--parent", "DEV-a-1"}},
		{name: "no parent at all", argv: []string{"--parent", ""}},
		{name: "a parent twice", argv: []string{"--parent", "DEV-A-1", "--parent", "DEV-A-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"article", "update", "DEV-A-7"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleUpdateRefusesAParentTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7":     fake.JSON(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-99999": fake.JSON(http.StatusNotFound, said),
	}, noUpdate(t))

	got := runWith(t, server.Env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-99999")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleLineRequest(server.URL, "DEV-A-99999")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Can't find article with id DEV-A-99999"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-99999"}, server.Paths())
}
