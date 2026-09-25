package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const articleCommentFields = "comments(id,author(login),created,text)"

const sentArticleFields = articleShowFields + "," + articleCommentFields

func receivedArticleComment(id string, created int64, login, text string) map[string]any {
	return map[string]any{
		"$type":   "ArticleComment",
		"id":      id,
		"author":  map[string]any{"$type": "User", "login": login},
		"created": created,
		"text":    text,
	}
}

func articleWithComments(t *testing.T, comments ...map[string]any) string {
	t.Helper()
	received := make([]any, 0, len(comments))
	for _, comment := range comments {
		received = append(received, comment)
	}
	return articleWith(t, map[string]any{"comments": received})
}

func TestArticleShowRefusesACommentsFlagThatIsNeitherAllNorACount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a negative count", argv: []string{"--comments=-1"}},
		{name: "a word of its own", argv: []string{"--comments=x"}},
		{name: "no value at all", argv: []string{"--comments"}},
		{name: "the flag twice", argv: []string{"--comments=1", "--comments=2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"article", "show", "DEV-A-1"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestArticleShowRefusesCommentsAskedForInTheExpression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "in place of the default", expression: "comments(text)"},
		{name: "added to the default", expression: "+comments"},
		{name: "under a child article", expression: "childArticles(comments(id))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "article", "show", "DEV-A-1", "--fields", tc.expression)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestArticleShowPrintsTheCommentsAskedForOldestFirst(t *testing.T) {
	t.Parallel()
	first := commentDetails("8-0", "2026-09-10T10:16:50Z", "admin", "первый")
	second := commentDetails("8-1", "2026-09-10T10:16:50.5Z", "dev.member", "второй")
	third := commentDetails("8-2", "2026-09-10T10:16:50.875Z", "admin", "третий")
	fourth := commentDetails("8-3", "2026-09-10T10:16:51Z", "admin", "четвёртый")
	fifth := commentDetails("8-4", "2026-09-10T10:16:52Z", "admin", "пятый")
	tests := []struct {
		name    string
		argv    []string
		printed []any
		sent    string
	}{
		{
			name:    "every one of them by default",
			printed: []any{first, second, third, fourth, fifth},
			sent:    "idReadable," + articleCommentFields,
		},
		{
			name:    "the last two",
			argv:    []string{"--comments=2"},
			printed: []any{fourth, fifth},
			sent:    "idReadable," + articleCommentFields,
		},
		{name: "none at all", argv: []string{"--comments=0"}, sent: "idReadable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := articleWithComments(t,
				receivedArticleComment("8-2", 1789035410875, "admin", "третий"),
				receivedArticleComment("8-0", 1789035410000, "admin", "первый"),
				receivedArticleComment("8-4", 1789035412000, "admin", "пятый"),
				receivedArticleComment("8-3", 1789035411000, "admin", "четвёртый"),
				receivedArticleComment("8-1", 1789035410500, "dev.member", "второй"),
			)
			server := serve(t, respondWith(http.StatusOK, body))

			argv := append([]string{"article", "show", "DEV-A-1", "--fields", "idReadable"}, tc.argv...)
			got := runWith(t, server.env(), argv...)

			assert.Equal(t, []string{tc.sent}, server.sentFields())
			assert.NotContains(t, server.sentFields()[0], "deleted")
			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			want := []detail{{"idReadable", "DEV-A-1"}}
			if tc.printed != nil {
				want = append(want, detail{"comments", tc.printed})
			}
			assert.Equal(t, want, requireDocument(t, got.stdout))
		})
	}
}

func TestArticleShowPrintsTheTextOfACommentAsReceived(t *testing.T) {
	t.Parallel()
	tests := []textCase{
		{name: "a line separator", text: "первая\xe2\x80\xa8вторая", quoted: true},
		{name: "a lone carriage return", text: "первая\rвторая", quoted: true},
		{name: "an empty line before a line beginning with a space", text: "\n первая"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := articleWithComments(t, receivedArticleComment("8-0", 1789035410875, "admin", tc.text))
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "article", "show", "DEV-A-1", "--fields", "idReadable")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			root := requireMapping(t, "stdout", got.stdout)
			printed := nodeAt(t, root, "comments", "text")
			assert.Equal(t, tc.text, printed.Value, "stdout: %q", got.stdout)
			assert.Equal(t, tc.style(), printed.Style, "stdout: %q", got.stdout)
			assert.Equal(t, yaml.MappingNode, nodeAt(t, root, "comments", "author").Kind)
			assert.Equal(t, "2026-09-10T10:16:50.875Z", nodeAt(t, root, "comments", "created").Value)
		})
	}
}

func TestArticleShowPrintsTheArticleOfTheDevInstanceWithNoCommentsAsAnEmptyList(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "show", "DEV-A-1", "--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{{"idReadable", "DEV-A-1"}, {"comments", []any{}}}, requireDocument(t, got.stdout))
	assert.Equal(t, []string{"idReadable," + articleCommentFields}, dev.sentFields())
	assert.Len(t, dev.requests(), 1)
}

func TestArticleShowAsksTheDevInstanceForNoCommentsAtZero(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "show", "DEV-A-1", "--comments=0")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	for _, key := range requireDocument(t, got.stdout) {
		assert.NotEqual(t, "comments", key.key)
	}
	assert.Equal(t, []string{articleShowFields}, dev.sentFields())
}
