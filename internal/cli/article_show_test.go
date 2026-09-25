package cli_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const articleShowFields = "idReadable,summary,reporter(login),created,updated,tags(name)," +
	"parentArticle(idReadable,summary),childArticles(idReadable,summary),content"

func articleWithAChild() string {
	return `{"summary":"[bug] fix login","$type":"Article","id":"177-1",` +
		`"childArticles":[{"$type":"Article","idReadable":"DEV-A-2","summary":"Дочерняя статья"}],` +
		`"content":"Первая строка\nвторая строка","updated":1787942509046,"comments":[],` +
		`"tags":[{"name":"история-полигона","$type":"Tag"}],"reporter":{"$type":"User","login":"admin"},` +
		`"created":1789035410875,"idReadable":"DEV-A-1","parentArticle":null}`
}

const printedArticleWithAChild = `idReadable: "DEV-A-1"
summary: "[bug] fix login"
reporter:
  login: "admin"
created: "2026-09-10T10:16:50.875Z"
updated: "2026-08-28T18:41:49.046Z"
tags:
  - {name: "история-полигона"}
parentArticle: null
childArticles:
  - {idReadable: "DEV-A-2", summary: "Дочерняя статья"}
content: |-
  Первая строка
  вторая строка
comments: []
`

func articleWith(t *testing.T, keys map[string]any) string {
	t.Helper()
	object := map[string]any{"$type": "Article", "idReadable": "DEV-A-1"}
	for key, value := range keys {
		object[key] = value
	}
	encoded, err := json.Marshal(object)
	require.NoError(t, err)
	return string(encoded)
}

func TestArticleShowPrintsTheFieldsAskedInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, articleWithAChild()))

	got := runWith(t, server.Env(), "article", "show", "DEV-A-1")

	assert.Equal(t, outcome{stdout: printedArticleWithAChild}, got)
	requests := server.Requests()
	require.Len(t, requests, 1)
	request := requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "/api/articles/DEV-A-1", request.URL.Path)
	assert.Equal(t, url.Values{"fields": {sentArticleFields}}, request.URL.Query())
}

func TestArticleShowPrintsContentAsALiteralBlockWhereverItCanCarryIt(t *testing.T) {
	t.Parallel()
	for _, tc := range textCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, articleWith(t, map[string]any{"content": tc.text})))

			got := runWith(t, server.Env(), "article", "show", "DEV-A-1", "--comments=0", "--fields", "idReadable,content")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			printed := nodeAt(t, requireMapping(t, "stdout", got.stdout), "content")
			assert.Equal(t, tc.text, printed.Value, "stdout: %q", got.stdout)
			assert.Equal(t, tc.style(), printed.Style, "stdout: %q", got.stdout)
		})
	}
}

func TestArticleShowPrintsAnEmptyArticle(t *testing.T) {
	t.Parallel()
	empty := map[string]any{
		"summary": "Пустая статья", "reporter": map[string]any{"$type": "User", "login": "admin"},
		"created": 1789035410875, "updated": 1789035410875, "tags": []any{},
		"parentArticle": nil, "childArticles": []any{}, "content": nil, "comments": []any{},
	}
	server := fake.Serve(t, fake.JSON(http.StatusOK, articleWith(t, empty)))

	got := runWith(t, server.Env(), "article", "show", "DEV-A-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []detail{
		{"idReadable", "DEV-A-1"},
		{"summary", "Пустая статья"},
		{"reporter", []detail{{"login", "admin"}}},
		{"created", "2026-09-10T10:16:50.875Z"},
		{"updated", "2026-09-10T10:16:50.875Z"},
		{"tags", []any{}},
		{"parentArticle", nil},
		{"childArticles", []any{}},
		{"content", nil},
		{"comments", []any{}},
	}, requireDocument(t, got.stdout))
}

func TestArticleShowPassesOnWhatTheServerAnswered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{
			name: "a web page under a 200", status: http.StatusOK,
			body: "<html><body>login</body></html>", code: "upstream_invalid",
		},
		{
			name: "a refusal of the server", status: http.StatusForbidden,
			body: `{"error":"Forbidden","error_description":"Insufficient rights"}`, code: "denied",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(tc.status, tc.body))

			got := runWith(t, server.Env(), "article", "show", "DEV-A-1")

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Len(t, server.Requests(), 1)
		})
	}
}
