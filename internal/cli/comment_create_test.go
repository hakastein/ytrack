package cli_test

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const writtenCommentFields = "id,author(login),created,updated,text"

func issueCommentRequest(address, owner, fields string) string {
	return "POST " + address + "/api/issues/" + owner + "/comments?fields=" + fields
}

type answeredComment struct {
	schema   string
	id       string
	textJSON string
	author   string
	updated  string
}

func (a answeredComment) json() string {
	return `{"$type":"` + cmp.Or(a.schema, "IssueComment") + `","id":` + strconv.Quote(a.id) +
		`,"author":{"$type":"User","login":` + strconv.Quote(cmp.Or(a.author, "admin")) + `}` +
		`,"created":1789035410875,"updated":` + cmp.Or(a.updated, "null") +
		`,"text":` + cmp.Or(a.textJSON, "null") + `}`
}

func createdComment(id, text string) string {
	return answeredComment{id: id, textJSON: asJSON(text)}.json()
}

func writtenArticleComment(id, text string) string {
	return answeredComment{schema: "ArticleComment", id: id, textJSON: asJSON(text)}.json()
}

func issueCommentNames() []any {
	return []any{"$type", "attachments", "author", "created", "deleted", "id", "issue", "pinned", "reactions",
		"text", "textPreview", "updated", "visibility"}
}

func articleCommentNames() []any {
	return []any{"$type", "article", "attachments", "author", "created", "id", "pinned", "reactions", "text",
		"updated", "visibility"}
}

func commenting(t *testing.T, write http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a comment is written by one POST and nothing else") {
			return
		}
		write(w, r)
	})
}

func sentComment(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	asks := u.asks()
	require.Len(t, asks, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(asks[0]), &body))
	return body
}

func TestCommentCreateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no subcommand", argv: []string{"comment"}},
		{name: "a subcommand the command has none of", argv: []string{"comment", "bogus"}},
		{name: "no owner", argv: []string{"comment", "create"}},
		{name: "no text", argv: []string{"comment", "create", "DEV-1"}},
		{name: "the text as an argument", argv: []string{"comment", "create", "DEV-1", "a"}},
		{name: "an owner, the text and one word more", argv: []string{"comment", "create", "DEV-1", "a", "b"}},
		{
			name: "the text twice",
			argv: []string{"comment", "create", "DEV-1", "--text", "a", "--text", "b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentCreateRefusesAnOwnerOrATextItWillNotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an internal id for an owner", argv: []string{"comment", "create", "3-19", "--text", "x"}},
		{name: "two dots for an owner", argv: []string{"comment", "create", "..", "--text", "x"}},
		{
			name: "the marker of an article in lower case",
			argv: []string{"comment", "create", "DEV-a-1", "--text", "x"},
		},
		{name: "a space before the owner", argv: []string{"comment", "create", " DEV-1", "--text", "x"}},
		{name: "an empty text", argv: []string{"comment", "create", "DEV-1", "--text", ""}},
		{name: "a text that is no UTF-8", argv: []string{"comment", "create", "DEV-1", "--text", "a\xffb"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentCreateTakesTheTextByItsFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a list item as an argument", argv: []string{"comment", "create", "DEV-1", "- пункт"}},
		{name: "a word of one dash as an argument", argv: []string{"comment", "create", "DEV-1", "-x"}},
		{name: "the text out of a file", argv: []string{"comment", "create", "DEV-1", "--text-file", "note.md"}},
		{name: "a file of any name", argv: []string{"comment", "create", "DEV-1", "--file", "note.md"}},
		{name: "the text off the standard input", argv: []string{"comment", "create", "DEV-1", "--stdin"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentCreateRefusesAnExpressionItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "an unclosed parenthesis", expression: "id("},
		{name: "nothing at all", expression: ""},
		{name: "a plus alone", expression: "+"},
		{name: "a comma at the end of what is added to the default", expression: "+issue(idReadable),"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "comment", "create", "DEV-7", "--text", "x", "--fields", tc.expression)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentCreateHelpNamesTheDefaultAndNoFile(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"comment", "create", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, writtenCommentFields)
	assert.Contains(t, got.stdout, "--text")
	assert.Contains(t, got.stdout, "DEV-A-1")
	assert.Contains(t, got.stdout, "+issue(", "a wrong owner key fails after the comment is already written")
	assert.Contains(t, got.stdout, "+article(", "a wrong owner key fails after the comment is already written")
	assert.NotContains(t, got.stdout, "-file")
	assert.NotContains(t, got.stdout, "stdin")
}

const hostileComment = "Шаги:  \r\n1. открыть\rи закрыть   \n---\n~~~\n\u0085\u2028\ufeffи ещё \U0001F600\n"

const longestLinuxArgument = 131_071

func TestCommentCreateWritesOnAnIssueInOneRequest(t *testing.T) {
	t.Parallel()
	text := textOfSize(hostileComment, longestLinuxArgument)
	server := commenting(t, respondWith(http.StatusOK, createdComment("7-12", text)))

	got := runWith(t, server.env(), "comment", "create", "DEV-7", "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"/api/issues/DEV-7/comments"}, server.sentPaths())
	assert.Equal(t, []string{writtenCommentFields}, server.sentFields())
	assert.Equal(t, map[string]any{"text": text}, sentComment(t, server))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "author", "created", "updated", "text"}, keysOf(mapping))
	assert.Equal(t, "7-12", nodeAt(t, mapping, "id").Value)
	assert.Equal(t, "admin", nodeAt(t, mapping, "author", "login").Value)
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, nodeAt(t, mapping, "created").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "updated")))
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, text, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style, "a carriage return keeps text out of a literal block")
}

func TestCommentCreateWritesOnAnArticleInOneRequest(t *testing.T) {
	t.Parallel()
	const text = "первая\n  вторая   \n"
	server := commenting(t, respondWith(http.StatusOK, writtenArticleComment("8-5", text)))

	got := runWith(t, server.env(), "comment", "create", "dev-A-3", "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"/api/articles/dev-A-3/comments"}, server.sentPaths())
	assert.NotContains(t, strings.Join(server.sentPaths(), " "), "/api/issues")
	assert.Equal(t, map[string]any{"text": text}, sentComment(t, server))

	written := nodeAt(t, requireMapping(t, "stdout", got.stdout), "text")
	assert.Equal(t, text, written.Value)
	assert.Equal(t, yaml.LiteralStyle, written.Style)
}

func TestCommentCreateWritesTheTextItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "one dash", argv: []string{"--text", "-"}, want: "-"},
		{name: "a list item", argv: []string{"--text", "- пункт"}, want: "- пункт"},
		{name: "the word --help", argv: []string{"--text", "--help"}, want: "--help"},
		{name: "a number with a sign, written with an equals sign", argv: []string{"--text=-1"}, want: "-1"},
		{name: "a word of one dash", argv: []string{"--text", "-x"}, want: "-x"},
		{name: "one space", argv: []string{"--text", " "}, want: " "},
		{name: "one line feed", argv: []string{"--text", "\n"}, want: "\n"},
		{name: "a vote", argv: []string{"--text", "+1"}, want: "+1"},
		{name: "markup that reads as a tag", argv: []string{"--text", "[bug] fix login"}, want: "[bug] fix login"},
		{name: "a NUL", argv: []string{"--text", "a\x00b"}, want: "a\x00b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commenting(t, respondWith(http.StatusOK, createdComment("7-12", tc.want)))

			got := runWith(t, server.env(), append([]string{"comment", "create", "DEV-7"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, map[string]any{"text": tc.want}, sentComment(t, server))
		})
	}
}

func TestCommentCreateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		written  string
		mismatch []any
	}{
		{
			name:     "a text the server stored otherwise",
			written:  createdComment("7-12", "другое"),
			mismatch: []any{[]detail{{"field", "text"}, {"expected", "первая"}, {"actual", "другое"}}},
		},
		{
			name:     "a comment the server kept no text of",
			written:  answeredComment{id: "7-12", textJSON: "null"}.json(),
			mismatch: []any{[]detail{{"field", "text"}, {"expected", "первая"}, {"actual", nil}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commenting(t, respondWith(http.StatusOK, tc.written))

			got := runWith(t, server.env(), "comment", "create", "DEV-7", "--text", "первая")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueCommentRequest(server.url, "DEV-7", writtenCommentFields)},
					{"comment", "7-12"},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

func TestCommentCreateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := commenting(t, respondWith(http.StatusOK, createdComment("7-12", "первая")))

	got := runWith(t, server.env(), "comment", "create", "DEV-7", "--text", "первая", "--fields", "author(login)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "author:\n  login: \"admin\"\n", got.stdout)
	assert.Equal(t, []string{"author(login),id,text"}, server.sentFields())
}

func TestCommentCreatePrintsTheOwnerWhereItWasAskedFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		owner  string
		schema string
		key    string
		stood  string
	}{
		{name: "an issue", owner: "DEV-7", schema: "IssueComment", key: "issue", stood: "Issue"},
		{name: "an article", owner: "DEV-A-3", schema: "ArticleComment", key: "article", stood: "Article"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			written := `{"$type":"` + tc.schema + `","id":"7-12","author":{"$type":"User","login":"admin"},` +
				`"created":1789035410875,"updated":null,"text":"первая",` +
				`"` + tc.key + `":{"$type":"` + tc.stood + `","idReadable":"` + tc.owner + `"}}`
			server := commenting(t, respondWith(http.StatusOK, written))

			got := runWith(t, server.env(), "comment", "create", tc.owner, "--text", "первая",
				"--fields", "+"+tc.key+"(idReadable)")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{writtenCommentFields + "," + tc.key + "(idReadable)"}, server.sentFields())
			mapping := requireMapping(t, "stdout", got.stdout)
			assert.Equal(t, []string{"id", "author", "created", "updated", "text", tc.key}, keysOf(mapping))
			assert.Equal(t, tc.owner, nodeAt(t, mapping, tc.key, "idReadable").Value)
		})
	}
}

func TestCommentCreateChecksTheResponseAgainstTheSchemaOfTheOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		owner      string
		expression string
		written    string
		unknown    []detail
	}{
		{
			name:       "a flag only a comment of an issue carries, asked of an article",
			owner:      "DEV-A-3",
			expression: "+deleted",
			written:    writtenArticleComment("8-5", "первая"),
			unknown:    unknownEntry("deleted", articleCommentNames()...),
		},
		{
			name:       "the owner of a comment of an article, asked of an issue",
			owner:      "DEV-7",
			expression: "+article(idReadable)",
			written:    createdComment("7-12", "первая"),
			unknown:    unknownEntry("article", issueCommentNames()...),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commenting(t, respondWith(http.StatusOK, tc.written))

			got := runWith(t, server.env(), "comment", "create", tc.owner, "--text", "первая",
				"--fields", tc.expression)

			found := requireUncertainty(t, got)
			assert.Equal(t, "unknown_name", found.code)
			assert.Equal(t, []any{tc.unknown}, detailNamed(t, found, "unknown"))
		})
	}
}

func TestCommentCreateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		status          int
		code            string
		upstreamError   string
		upstreamMessage string
		details         []detail
	}{
		{
			name:            "an issue the instance has none of",
			status:          http.StatusNotFound,
			code:            "not_found",
			upstreamError:   "Not Found",
			upstreamMessage: "Entity with id DEV-99999 not found",
		},
		{
			name:            "a body the server disagreed with",
			status:          http.StatusBadRequest,
			code:            "rejected",
			upstreamError:   "bad_request",
			upstreamMessage: "Comment can't be empty.",
		},
		{
			name:            "a token that may read the issue and not comment on it",
			status:          http.StatusForbidden,
			code:            "denied",
			upstreamError:   "Forbidden",
			upstreamMessage: "HTTP 403 Forbidden",
			details:         []detail{authFromEnv()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := `{"error":` + strconv.Quote(tc.upstreamError) + `,"error_description":` +
				strconv.Quote(tc.upstreamMessage) + `}`
			server := commenting(t, respondWith(tc.status, said))

			got := runWith(t, server.env(), "comment", "create", "DEV-7", "--text", "x")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", issueCommentRequest(server.url, "DEV-7", writtenCommentFields)},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

func contractCommentOwner(t *testing.T, role string) string {
	t.Helper()
	return "ytrack contract " + t.Name() + " " + role
}

func commentedIssue(t *testing.T, dev *upstream, role string) string {
	t.Helper()
	argv := append([]string{"issue", "create", "DEV", "--summary", contractCommentOwner(t, role)}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

func TestCommentCreateWritesOnAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	text := textOfSize(hostileComment, 81_033)

	got := runWith(t, dev.env(), "comment", "create", issue, "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Regexp(t, commentIDForm, nodeAt(t, mapping, "id").Value)
	assert.Equal(t, "admin", nodeAt(t, mapping, "author", "login").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "updated")))
	assert.Equal(t, text, nodeAt(t, mapping, "text").Value)

	answers := dev.answers()
	var kept struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(answers[len(answers)-1], &kept))
	assert.Equal(t, text, kept.Text, "the dev instance keeps the text of a comment byte for byte")
}

func TestCommentCreateWritesOnAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := fileArticle(t, dev, contractCommentOwner(t, "article"))
	t.Cleanup(func() { removeArticle(t, dev, article) })

	got := runWith(t, dev.env(), "comment", "create", article, "--text", "первая\r\nвторая")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Regexp(t, commentIDForm, nodeAt(t, mapping, "id").Value)
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, "первая\r\nвторая", written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style)
}

func TestCommentCreateAnswersTheMemberEveryKeyOfTheDefault(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	member := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}
	filed := runWith(t, member, "article", "create", "DEV", "--summary", contractCommentOwner(t, "article"),
		"--fields", "idReadable")
	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	article := nodeAt(t, requireMapping(t, "stdout", filed.stdout), "idReadable").Value
	t.Cleanup(func() { removeArticle(t, dev, article) })

	got := runWith(t, member, "comment", "create", article, "--text", "комментарий участника")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "author", "created", "updated", "text"}, keysOf(mapping))
	assert.Equal(t, "dev.member", nodeAt(t, mapping, "author", "login").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "updated")))
}

func TestCommentCreateWritesAVoteWithoutTheWorkflowRewritingIt(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")

	got := runWith(t, dev.env(), "comment", "create", issue, "--text", "+1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "+1", nodeAt(t, requireMapping(t, "stdout", got.stdout), "text").Value)

	read := runWith(t, dev.env(), "issue", "show", issue, "--fields", "votes", "--comments=0")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	assert.Equal(t, "0", nodeAt(t, requireMapping(t, "stdout", read.stdout), "votes").Value)
}

func TestCommentCreateRefusesAnOwnerTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		owner string
		path  string
	}{
		{name: "an issue", owner: "DEV-99999", path: "/api/issues/DEV-99999/comments"},
		{name: "an article", owner: "DEV-A-99999", path: "/api/articles/DEV-A-99999/comments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, dev.env(), "comment", "create", tc.owner, "--text", "x")

			assert.Equal(t, "not_found", requireFault(t, got).code)
			assert.Equal(t, []string{tc.path}, dev.sentPaths())
		})
	}
}

func TestCommentCreateRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"comment", "create", issue, "--text", "x")

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, detail{"upstream_message", "Entity with id " + issue + " not found"}, found.details[3])

	read := runWith(t, dev.env(), "issue", "show", issue, "--fields", "idReadable")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	assert.Empty(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", read.stdout), "comments")))
}
