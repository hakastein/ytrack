package cli_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// What the tool asks of every comment, whatever the caller asked of the issue: deleted is read to leave a
// deleted comment out and is printed nowhere.
const commentFields = "comments(id,author(login),created,text,deleted)"

// The form of a comment's id, which the server gives it and ytrack passes on.
const commentIDForm = `^[0-9]+-[0-9]+$`

// A comment of the server, with $type on every object as the server sends it.
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

// A comment whose author took it back: YouTrack keeps it in the list and takes its text away.
func deletedComment(id string, created int64) map[string]any {
	comment := receivedComment(id, created, "admin", "")
	comment["text"] = nil
	comment["deleted"] = true
	return comment
}

// commentDetails is one comment as the document prints it.
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
		{name: "no value at all", argv: []string{"--comments"}},
		{name: "the flag twice", argv: []string{"--comments=1", "--comments=2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"issue", "show", "DEV-1"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// Comments are asked for one way, and --fields is not it: the tool fills them wherever an issue stands, so an
// expression naming them there is refused before any request and the refusal says what to write instead.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The order is ytrack's own: the server sends the comments in an order of its own, the deleted one among them,
// and what is printed is the rest oldest first, cut to the last the caller asked for.
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

// None asked for is not the same as none there: at zero nothing about comments goes out and the key is absent,
// while an issue that has none prints the key empty.
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

// A comment is read before it is printed, to leave the deleted out and to put the rest in order, so an answer
// that is not shaped as the specification says is refused there rather than sorted on.
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
			}, requireRefusal(t, got))
		})
	}
}

func TestIssueShowPrintsTheCommentOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-1", "--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	sent := sentComments(t, dev)
	require.Len(t, sent, 1)
	root := requireMapping(t, "stdout", got.stdout)
	comments := nodeAt(t, root, "comments")
	require.Equal(t, yaml.SequenceNode, comments.Kind)
	require.Len(t, comments.Content, 1)
	assert.Regexp(t, commentIDForm, nodeAt(t, root, "comments", "id").Value)
	assert.Equal(t, "admin", nodeAt(t, root, "comments", "author", "login").Value)
	assert.Regexp(t, instantForm, nodeAt(t, root, "comments", "created").Value)
	text := nodeAt(t, root, "comments", "text")
	assert.Equal(t, sent[0]["text"], text.Value)
	assert.Equal(t, yaml.LiteralStyle, text.Style)
	assert.Len(t, dev.requests(), 1)
}

func TestIssueShowPrintsTheIssueOfTheDevInstanceWithNoCommentsAsAnEmptyList(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-2", "--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, sentComments(t, dev))
	assert.Equal(t, []detail{{"idReadable", "DEV-2"}, {"comments", []any{}}}, requireDocument(t, got.stdout))
}

func TestIssueShowAsksTheDevInstanceForNoCommentsAtZero(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-1", "--comments=0")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	for _, key := range requireDocument(t, got.stdout) {
		assert.NotEqual(t, "comments", key.key)
	}
	assert.Equal(t, []string{askedIssueFields}, dev.sentFields())
}

// The two comments of DEV-7 were written by two users one after the other, so they say both that the order is
// the order they were written in and that the count takes the last of them.
func TestIssueShowPrintsTheCommentsOfTheDevInstanceOldestFirst(t *testing.T) {
	t.Parallel()
	admin := commentDetails("7-2", "", "admin", "Комментарий администратора после правки.")
	member := commentDetails("7-3", "", "dev.member", "Комментарий участника: +1")
	tests := []struct {
		name    string
		argv    []string
		printed []any
	}{
		{name: "every one of them by default", printed: []any{admin, member}},
		{name: "the last of them", argv: []string{"--comments=1"}, printed: []any{member}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			argv := append([]string{"issue", "show", "DEV-7", "--fields", "idReadable"}, tc.argv...)
			got := runWith(t, dev.env(), argv...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			printed := requireDocument(t, got.stdout)
			require.Equal(t, []string{"idReadable", "comments"}, []string{printed[0].key, printed[1].key})
			comments, areRecords := printed[1].value.([]any)
			require.True(t, areRecords, "stdout: %q", got.stdout)
			require.Len(t, comments, len(tc.printed))
			for i, want := range tc.printed {
				record, areDetails := comments[i].([]detail)
				require.True(t, areDetails)
				assert.Regexp(t, instantForm, record[2].value)
				record[2].value = ""
				assert.Equal(t, want, record)
			}
		})
	}
}

// sentComments is the comments of the one answer the server sent, as JSON read them.
func sentComments(t *testing.T, u *upstream) []map[string]any {
	t.Helper()
	answers := u.answers()
	require.Len(t, answers, 1)
	var body struct {
		Comments []map[string]any `json:"comments"`
	}
	require.NoError(t, json.Unmarshal(answers[0], &body))
	return body.Comments
}
