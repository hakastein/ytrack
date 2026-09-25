package cli_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// What --fields prints by default, which is what its help names and what goes out where the caller writes none
// of their own: an article needs no composition of the tool's, so this is the whole of the request as well.
const articleShowFields = "idReadable,summary,reporter(login),created,updated,tags(name)," +
	"parentArticle(idReadable,summary),childArticles(idReadable,summary),content"

// An article under the default expression, with $type on every object the server sends it on, an id that was
// not asked for and the keys in an order other than the one asked for.
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

// A refusal names the request article show sends: the id stands in the path escaped, the way it went out.
func articleRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + sentArticleFields
}

// The whole refusal a 404 for an article becomes, with what the server said about it word for word: said is
// its own sentence for an article that was never written and the sentence of any missing entity for one the
// token may not see.
func noSuchArticle(address, id, said string) faultDocument {
	return faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleRequest(address, id)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", said},
		},
	}
}

// articleWith is the answer for an article carrying keys, encoded the way the server encodes it: a control
// character arrives escaped rather than raw, and a character outside the basic plane as a surrogate pair.
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

// sentContent is the content of the one answer the server sent, as JSON read it.
func sentContent(t *testing.T, u *upstream) any {
	t.Helper()
	answers := u.answers()
	require.Len(t, answers, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal(answers[0], &body))
	return body["content"]
}

// One article per call, and a group that names no command of its own is a refusal rather than a help page.
// Nothing of this costs a request.
func TestArticleShowRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the group alone", argv: []string{"article"}},
		{name: "a command it does not have", argv: []string{"article", "bogus"}},
		{name: "no id", argv: []string{"article", "show"}},
		{name: "two ids", argv: []string{"article", "show", "DEV-A-1", "DEV-A-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestArticleShowHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "show", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, articleShowFields)
}

// The document is the expression asked for: the keys stand in the order they were asked in whatever order
// the server sent them, $type and an id nobody asked for are left out, and a summary is printed as it arrived.
// One request carries the whole article, and it asks nothing of the server beyond the fields.
func TestArticleShowPrintsTheFieldsAskedInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, articleWithAChild()))

	got := runWith(t, server.env(), "article", "show", "DEV-A-1")

	assert.Equal(t, outcome{stdout: printedArticleWithAChild}, got)
	requests := server.requests()
	require.Len(t, requests, 1)
	request := requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "/api/articles/DEV-A-1", request.URL.Path)
	assert.Equal(t, url.Values{"fields": {sentArticleFields}}, request.URL.Query())
}

// The content of an article comes back as it went out, whatever it holds: a literal block carries every
// text but the ones holding a rune it cannot, and those go to the writer of double-quoted strings, which
// carries them without loss too. An article keeps a carriage return, so that text reaches here for real.
func TestArticleShowPrintsContentAsALiteralBlockWhereverItCanCarryIt(t *testing.T) {
	t.Parallel()
	for _, tc := range textCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, articleWith(t, map[string]any{"content": tc.text})))

			got := runWith(t, server.env(), "article", "show", "DEV-A-1", "--comments=0", "--fields", "idReadable,content")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			printed := nodeAt(t, requireMapping(t, "stdout", got.stdout), "content")
			assert.Equal(t, tc.text, printed.Value, "stdout: %q", got.stdout)
			assert.Equal(t, tc.style(), printed.Style, "stdout: %q", got.stdout)
		})
	}
}

// A root article with nothing under it and nothing on it prints every key it was asked for: an empty
// parent, an empty list of children, an empty list of tags, no content at all and no comment.
func TestArticleShowPrintsAnEmptyArticle(t *testing.T) {
	t.Parallel()
	empty := map[string]any{
		"summary": "Пустая статья", "reporter": map[string]any{"$type": "User", "login": "admin"},
		"created": 1789035410875, "updated": 1789035410875, "tags": []any{},
		"parentArticle": nil, "childArticles": []any{}, "content": nil, "comments": []any{},
	}
	server := serve(t, respondWith(http.StatusOK, articleWith(t, empty)))

	got := runWith(t, server.env(), "article", "show", "DEV-A-1")

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

// What the server answers with is read the way every answer is: a body that is no JSON under a 200 is the
// answer of something other than the endpoint asked for, and a 403 is the server saying no.
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
			server := serve(t, respondWith(tc.status, tc.body))

			got := runWith(t, server.env(), "article", "show", "DEV-A-1")

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestArticleShowPrintsAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "show", "DEV-A-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	printed := requireDocument(t, got.stdout)
	require.Len(t, printed, 10)
	assert.Equal(t, []detail{
		{"idReadable", "DEV-A-1"},
		{"summary", "Родительская статья"},
		{"reporter", []detail{{"login", "admin"}}},
	}, printed[:3])
	assert.Equal(t, []string{"created", "updated"}, []string{printed[3].key, printed[4].key})
	assert.Regexp(t, instantForm, printed[3].value)
	assert.Regexp(t, instantForm, printed[4].value)
	assert.Equal(t, []detail{
		{"tags", []any{}},
		{"parentArticle", nil},
		{"childArticles", []any{[]detail{{"idReadable", "DEV-A-2"}, {"summary", "Дочерняя статья"}}}},
	}, printed[5:8])
	content := nodeAt(t, requireMapping(t, "stdout", got.stdout), "content")
	assert.Equal(t, sentContent(t, dev), content.Value)
	assert.Equal(t, yaml.LiteralStyle, content.Style)
	assert.Equal(t, detail{"comments", []any{}}, printed[9])
	assert.NotContains(t, got.stdout, "$type")
	assert.Equal(t, []string{sentArticleFields}, dev.sentFields())
	assert.Len(t, dev.requests(), 1)
}

func TestArticleShowPrintsTheChildArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "show", "dev-A-02")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	root := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "DEV-A-2", nodeAt(t, root, "idReadable").Value)
	assert.Equal(t, []detail{{"idReadable", "DEV-A-1"}, {"summary", "Родительская статья"}},
		requireValue(t, nodeAt(t, root, "parentArticle")))
	assert.Equal(t, []any{}, requireValue(t, nodeAt(t, root, "childArticles")))
	assert.Len(t, dev.requests(), 1)
}

func TestArticleShowRefusesAnArticleTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "show", "DEV-A-99999")

	assert.Equal(t, noSuchArticle(dev.url, "DEV-A-99999", "Can't find article with id DEV-A-99999"),
		requireRefusal(t, got))
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/articles/DEV-A-99999", dev.sentPaths()[0])
}

// $top and $skip are ignored on one article, and the contract is worth a test of its own because
// ytrack cannot break it from this side: the specification declares neither parameter for the operation, so the
// proxy of the scenario writes them onto the wire. A $top of 0 obeyed would answer with no child at all, and a
// $skip of 1 would step past the one there is; the article that comes back is the same either way, and the same
// as the one that arrives with neither parameter sent.
func TestArticleShowGetsTheSameArticleOfTheDevInstanceWhateverTopAndSkipSay(t *testing.T) {
	t.Parallel()
	const asked = "idReadable,childArticles(idReadable)"
	tests := []struct {
		name  string
		added string
		sent  url.Values
	}{
		{name: "plain", sent: url.Values{"fields": {asked}}},
		{
			name:  "top_skip",
			added: "$top=0&$skip=1",
			sent:  url.Values{"fields": {asked}, "$top": {"0"}, "$skip": {"1"}},
		},
		{name: "top_abc", added: "$top=abc", sent: url.Values{"fields": {asked}, "$top": {"abc"}}},
	}
	printed := make([]string, 0, len(tests))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := devInstanceAsking(t, tc.added)

			got := runWith(t, dev.env(), "article", "show", "DEV-A-1", "--comments=0", "--fields", asked)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []detail{
				{"idReadable", "DEV-A-1"},
				{"childArticles", []any{[]detail{{"idReadable", "DEV-A-2"}}}},
			}, requireDocument(t, got.stdout))
			assert.Equal(t, []url.Values{tc.sent}, dev.sentQueries())
			printed = append(printed, got.stdout)
		})
	}
	require.Len(t, printed, len(tests), "a run answered with no document of its own")
	for i, document := range printed[1:] {
		assert.Equal(t, printed[0], document, "%s was answered with another article", tests[i+1].name)
	}
}

// An article hidden from a token is no article at all to it: the server answers 404 and says the entity is
// not there, and ytrack passes that on rather than guessing at rights.
func TestArticleShowRefusesTheArticleTheLimitedUserCannotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"article", "show", "DEV-A-1")

	assert.Equal(t, noSuchArticle(dev.url, "DEV-A-1", "Entity with id DEV-A-1 not found"), requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}
