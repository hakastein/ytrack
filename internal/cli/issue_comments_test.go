package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const commentFields = "comments(id,author(login),created,text,deleted)"

func receivedComment(id string, created int64, login, text string) map[string]any {
	return map[string]any{
		"$type":   "IssueComment",
		"id":      id,
		"author":  map[string]any{"$type": "User", "login": login},
		"created": created,
		"text":    text,
		"deleted": false,
	}
}

func deletedComment(id string, created int64) map[string]any {
	comment := receivedComment(id, created, "admin", "")
	comment["text"] = nil
	comment["deleted"] = true
	return comment
}

func commentDetails(id, created, login, text string) []detail {
	return []detail{
		{"id", id},
		{"author", []detail{{"login", login}}},
		{"created", created},
		{"text", text},
	}
}

func issueWithComments(t *testing.T, comments ...map[string]any) string {
	t.Helper()
	received := make([]any, 0, len(comments))
	for _, comment := range comments {
		received = append(received, comment)
	}
	return issueWith(t, map[string]any{"comments": received})
}

func TestIssueShowRefusesACommentsFlagThatIsNeitherAllNorACount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a negative count", argv: []string{"--comments=-1"}},
		{name: "a word of its own", argv: []string{"--comments=x"}},
		{name: "a fraction", argv: []string{"--comments=1.5"}},
		{name: "a count past what a number holds", argv: []string{"--comments=9223372036854775808"}},
		{name: "the flag twice", argv: []string{"--comments=1", "--comments=2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"issue", "show", "DEV-1"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueShowRefusesCommentsAskedForInTheExpression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "in place of the default", expression: "comments(text)"},
		{name: "added to the default", expression: "+comments"},
		{name: "under the issues of a link", expression: "links(issues(comments(id)))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--fields", tc.expression)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueShowPrintsTheCommentsAskedForOldestFirst(t *testing.T) {
	t.Parallel()
	first := commentDetails("7-1", "2026-09-10T10:16:50Z", "admin", "первый")
	second := commentDetails("7-2", "2026-09-10T10:16:50.5Z", "dev.member", "второй")
	third := commentDetails("7-3", "2026-09-10T10:16:50.875Z", "admin", "третий")
	last := commentDetails("7-5", "2026-09-10T10:16:52Z", "admin", "пятый")
	tests := []struct {
		name    string
		argv    []string
		printed []any
	}{
		{name: "every one of them by default", printed: []any{first, second, third, last}},
		{name: "the last two", argv: []string{"--comments=2"}, printed: []any{third, last}},
		{name: "more than the issue has", argv: []string{"--comments=10"}, printed: []any{first, second, third, last}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithComments(t,
				receivedComment("7-3", 1789035410875, "admin", "третий"),
				receivedComment("7-1", 1789035410000, "admin", "первый"),
				deletedComment("7-4", 1789035411000),
				receivedComment("7-5", 1789035412000, "admin", "пятый"),
				receivedComment("7-2", 1789035410500, "dev.member", "второй"),
			)
			server := serve(t, respondWith(http.StatusOK, body))

			argv := append([]string{"issue", "show", "DEV-1", "--fields", "idReadable"}, tc.argv...)
			got := runWith(t, server.env(), argv...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []detail{{"idReadable", "DEV-1"}, {"comments", tc.printed}}, requireDocument(t, got.stdout))
			assert.Equal(t, []string{"idReadable," + commentFields}, server.sentFields())
		})
	}
}

func TestIssueShowAsksForNoCommentsAtZero(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1"}`))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--fields", "idReadable", "--comments=0")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\n"}, got)
	assert.Equal(t, []string{"idReadable"}, server.sentFields())
}

func TestIssueShowPrintsTheIssueWithNoCommentsAsAnEmptyList(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, issueWithComments(t)))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--fields", "idReadable")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\ncomments: []\n"}, got)
}

func TestIssueShowPrintsTheTextOfACommentAsReceived(t *testing.T) {
	t.Parallel()
	tests := []textCase{
		{name: "an empty line before a line beginning with a space", text: "\n первая"},
		{name: "a line separator", text: "первая\xe2\x80\xa8вторая", quoted: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithComments(t, receivedComment("7-1", 1789035410875, "admin", tc.text))
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--fields", "idReadable")

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

func TestIssueShowRefusesCommentsTheServerShapedOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		comments string
	}{
		{name: "no array at all", comments: `null`},
		{name: "a null in place of a comment", comments: `[null]`},
		{
			name:     "deleted as a word",
			comments: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"admin"},"created":0,"text":"","deleted":"true"}]`,
		},
		{
			name:     "the moment written as a string",
			comments: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"admin"},"created":"0","text":"","deleted":false}]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","idReadable":"DEV-1","comments":` + tc.comments + `}`
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--fields", "idReadable")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", "idReadable,"+commentFields)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireFault(t, got))
		})
	}
}
