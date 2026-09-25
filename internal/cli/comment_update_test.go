package cli_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const commentDeletedFields = "deleted"

func commentReadRequest(address, owner, comment string) string {
	return "GET " + address + "/api/issues/" + owner + "/comments/" + comment + "?fields=" + commentDeletedFields
}

func commentWriteRequest(address, owner, comment, fields string) string {
	return "POST " + address + "/api/issues/" + owner + "/comments/" + comment + "?fields=" + fields
}

func commentDeletedState(gone bool) string {
	return `{"$type":"IssueComment","deleted":` + strconv.FormatBool(gone) + `}`
}

func sentText(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body))
	return body
}

func updatingAComment(t *testing.T, read, write http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			read(w, r)
			return
		}
		if !assert.Equal(t, http.MethodPost, r.Method, "a write of a comment sends one GET and one POST") {
			return
		}
		write(w, r)
	})
}

func TestCommentUpdateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "neither owner nor id", argv: []string{"comment", "update"}},
		{name: "an owner and no id", argv: []string{"comment", "update", "DEV-1"}},
		{name: "the id of the comment alone", argv: []string{"comment", "update", "7-12", "--text", "x"}},
		{name: "no text", argv: []string{"comment", "update", "DEV-1", "7-1"}},
		{name: "the text as an argument", argv: []string{"comment", "update", "DEV-1", "7-1", "a"}},
		{name: "the text and one word more", argv: []string{"comment", "update", "DEV-1", "7-1", "a", "b"}},
		{name: "the text twice", argv: []string{"comment", "update", "DEV-1", "7-1", "--text", "a", "--text", "b"}},
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

func TestCommentUpdateRefusesAnIDThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "a dot", id: "."},
		{name: "two dots", id: ".."},
		{name: "a number alone", id: "7"},
		{name: "a number and a dash", id: "7-"},
		{name: "a number without a class", id: "-1"},
		{name: "three numbers", id: "7-1-1"},
		{name: "the readable id of an issue", id: "DEV-1"},
		{name: "the readable id of an article", id: "DEV-A-1"},
		{name: "a path after the id", id: "7-1/.."},
		{name: "an escaped slash after the id", id: "7-1%2F1"},
		{name: "a space before the id", id: " 7-1"},
		{name: "a line ending after the id", id: "7-1\n"},
		{name: "digits in full width", id: "\xef\xbc\x97-\xef\xbc\x91"},
		{name: "letters", id: "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "comment", "update", "DEV-1", "--text", "x", "--", tc.id)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentUpdateRefusesATextItWillNotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{name: "an empty text", text: ""},
		{name: "a text that is no UTF-8", text: "a\xffb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "comment", "update", "DEV-1", "7-1", "--text", tc.text)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentUpdateRefusesAnExpressionItCannotRead(t *testing.T) {
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

			got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "x",
				"--fields", tc.expression)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestCommentUpdateHelpNamesWhereTheIDComesFrom(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"comment", "update", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, writtenCommentFields)
	assert.Contains(t, got.stdout, "--text")
	assert.Contains(t, got.stdout, "+issue(")
	assert.Contains(t, got.stdout, "+article(")
	assert.NotContains(t, got.stdout, "-file")
	assert.NotContains(t, got.stdout, "stdin")
}

func TestCommentUpdateReadsAnIssueCommentBeforeWritingIt(t *testing.T) {
	t.Parallel()
	text := textOfSize(hostileComment, longestLinuxArgument)
	server := updatingAComment(t, respondWith(http.StatusOK, commentDeletedState(false)),
		respondWith(http.StatusOK, createdComment("7-12", text)))

	got := runWith(t, server.env(), "comment", "update", "dev-7", "7-12", "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"/api/issues/dev-7/comments/7-12", "/api/issues/dev-7/comments/7-12"},
		server.sentPaths())
	assert.Equal(t, []string{commentDeletedFields, writtenCommentFields}, server.sentFields())
	assert.Equal(t, map[string]any{"text": text}, sentText(t, server))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "author", "created", "updated", "text"}, keysOf(mapping))
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, text, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style, "a carriage return keeps text out of a literal block")
}

func TestCommentUpdateWritesOnAnArticleInOneRequest(t *testing.T) {
	t.Parallel()
	server := commenting(t, respondWith(http.StatusOK, writtenArticleComment("8-5", "x")))

	got := runWith(t, server.env(), "comment", "update", "DEV-A-3", "8-5", "--text", "x")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"/api/articles/DEV-A-3/comments/8-5"}, server.sentPaths())
	assert.NotContains(t, strings.Join(server.sentPaths(), " "), "/api/issues")
	assert.Equal(t, map[string]any{"text": "x"}, sentText(t, server))
}

func TestCommentUpdateRefusesADeletedComment(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, respondWith(http.StatusOK, commentDeletedState(true)), noUpdate(t))

	got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "x")

	want := faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", commentReadRequest(server.url, "DEV-7", "7-12")},
			{"comment", "7-12"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestCommentUpdateRefusesAReadThatSaysNothingOfDeleted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read string
	}{
		{name: "a null", read: `{"$type":"IssueComment","deleted":null}`},
		{name: "a word", read: `{"$type":"IssueComment","deleted":"true"}`},
		{name: "a number", read: `{"$type":"IssueComment","deleted":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := updatingAComment(t, respondWith(http.StatusOK, tc.read), noUpdate(t))

			got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "x")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", commentReadRequest(server.url, "DEV-7", "7-12")},
					{"upstream_status", 200},
					{"upstream_body", tc.read},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestCommentUpdateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	said := func(name, description string) string {
		return `{"error":` + strconv.Quote(name) + `,"error_description":` + strconv.Quote(description) + `}`
	}
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		argv    []string
		code    string
		methods []string
	}{
		{
			name: "a comment the read does not find",
			server: func(t *testing.T) *upstream {
				return updatingAComment(t, respondWith(http.StatusNotFound, entityNotFound("7-12")), noUpdate(t))
			},
			argv:    []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			code:    "not_found",
			methods: []string{http.MethodGet},
		},
		{
			name: "a comment of an article the server has none of",
			server: func(t *testing.T) *upstream {
				return commenting(t, respondWith(http.StatusNotFound, entityNotFound("8-5")))
			},
			argv:    []string{"comment", "update", "DEV-A-3", "8-5", "--text", "x"},
			code:    "not_found",
			methods: []string{http.MethodPost},
		},
		{
			name: "a token that may read the comment and not write it",
			server: func(t *testing.T) *upstream {
				return updatingAComment(t, respondWith(http.StatusOK, commentDeletedState(false)),
					respondWith(http.StatusForbidden, said("Forbidden", "HTTP 403 Forbidden")))
			},
			argv:    []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			code:    "denied",
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "a body the server disagreed with",
			server: func(t *testing.T) *upstream {
				return updatingAComment(t, respondWith(http.StatusOK, commentDeletedState(false)),
					respondWith(http.StatusBadRequest, said("bad_request", "Comment can't be empty.")))
			},
			argv:    []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			code:    "rejected",
			methods: []string{http.MethodGet, http.MethodPost},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, tc.code, requireFault(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestCommentUpdateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	takenBackMeanwhile := `{"$type":"IssueComment","id":"7-12","author":{"$type":"User","login":"admin"},` +
		`"created":1789035410875,"updated":1789035999000,"text":null,"deleted":true}`
	tests := []struct {
		name     string
		asked    []string
		fields   string
		written  string
		received any
	}{
		{
			name:     "a comment taken back between the read and the write",
			fields:   writtenCommentFields,
			written:  takenBackMeanwhile,
			received: nil,
		},
		{
			name:     "an answer standing under another id than the write was addressed by",
			fields:   writtenCommentFields,
			written:  createdComment("7-99", "другое"),
			received: "другое",
		},
		{
			name:     "an answer carrying no id, which no expression of the caller's asks for",
			asked:    []string{"--fields", "author(login)"},
			fields:   "author(login),text",
			written:  `{"$type":"IssueComment","author":{"$type":"User","login":"admin"},"text":"другое"}`,
			received: "другое",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := updatingAComment(t, respondWith(http.StatusOK, commentDeletedState(false)),
				respondWith(http.StatusOK, tc.written))

			argv := append([]string{"comment", "update", "DEV-7", "7-12", "--text", "первая"}, tc.asked...)
			got := runWith(t, server.env(), argv...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", commentWriteRequest(server.url, "DEV-7", "7-12", tc.fields)},
					{"comment", "7-12"},
					{"mismatch", []any{[]detail{{"field", "text"}, {"expected", "первая"}, {"actual", tc.received}}}},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
		})
	}
}

func TestCommentUpdateChecksTheResponseAgainstTheSchemaOfTheOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		argv    []string
		unknown []detail
	}{
		{
			name: "a flag only a comment of an issue carries, asked of an article",
			server: func(t *testing.T) *upstream {
				return commenting(t, respondWith(http.StatusOK, writtenArticleComment("8-5", "первая")))
			},
			argv:    []string{"comment", "update", "DEV-A-3", "8-5", "--text", "первая", "--fields", "+deleted"},
			unknown: unknownEntry("deleted", articleCommentNames()...),
		},
		{
			name: "the owner of a comment of an article, asked of an issue",
			server: func(t *testing.T) *upstream {
				return updatingAComment(t, respondWith(http.StatusOK, commentDeletedState(false)),
					respondWith(http.StatusOK, createdComment("7-12", "первая")))
			},
			argv: []string{"comment", "update", "DEV-7", "7-12", "--text", "первая",
				"--fields", "+article(idReadable)"},
			unknown: unknownEntry("article", issueCommentNames()...),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), tc.argv...)

			found := requireUncertainty(t, got)
			assert.Equal(t, "unknown_name", found.code)
			assert.Equal(t, []any{tc.unknown}, detailNamed(t, found, "unknown"))
		})
	}
}

func TestCommentUpdateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, respondWith(http.StatusOK, commentDeletedState(false)),
		respondWith(http.StatusOK, createdComment("7-12", "первая")))

	got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "первая",
		"--fields", "author(login)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "author:\n  login: \"admin\"\n", got.stdout)
	assert.Equal(t, []string{commentDeletedFields, "author(login),text"}, server.sentFields())
}

func commentedArticle(t *testing.T, dev *upstream, role string) string {
	t.Helper()
	article := fileArticle(t, dev, contractCommentOwner(t, role))
	t.Cleanup(func() { removeArticle(t, dev, article) })
	return article
}

func commentOn(t *testing.T, dev *upstream, owner, text string) string {
	t.Helper()
	got := runWith(t, dev.env(), "comment", "create", owner, "--text", text)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	id := nodeAt(t, requireMapping(t, "stdout", got.stdout), "id").Value
	require.Regexp(t, commentIDForm, id)
	return id
}

func textOfTheComment(t *testing.T, dev *upstream, show []string, comment string) string {
	t.Helper()
	got := runWith(t, dev.env(), show...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	comments := nodeAt(t, requireMapping(t, "stdout", got.stdout), "comments")
	for _, held := range comments.Content {
		if nodeAt(t, held, "id").Value == comment {
			return nodeAt(t, held, "text").Value
		}
	}
	require.Fail(t, "the owner holds no comment "+comment, "%v", got.stdout)
	return ""
}

func TestCommentUpdateWritesOnAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")
	const text = "  ytrack\r\nправка  "

	got := runWith(t, dev.env(), "comment", "update", issue, comment, "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, comment, nodeAt(t, mapping, "id").Value)
	assert.Equal(t, "admin", nodeAt(t, mapping, "author", "login").Value)
	assert.Regexp(t, instantForm, nodeAt(t, mapping, "created").Value)
	assert.Regexp(t, instantForm, nodeAt(t, mapping, "updated").Value)
	assert.Equal(t, text, nodeAt(t, mapping, "text").Value)
}

func TestCommentUpdateWritesOnAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := commentedArticle(t, dev, "article")
	comment := commentOn(t, dev, article, "ytrack contract первая")
	const text = "ytrack\xe2\x80\xa8правка"

	got := runWith(t, dev.env(), "comment", "update", article, comment, "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, comment, nodeAt(t, mapping, "id").Value)
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, text, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style)
}

func TestCommentUpdateRefusesACommentOfAnotherOwner(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	otherIssue := commentedIssue(t, dev, "other issue")
	article := commentedArticle(t, dev, "article")
	otherArticle := commentedArticle(t, dev, "other article")
	const onTheIssue = "ytrack contract комментарий задачи"
	const onTheArticle = "ytrack contract комментарий статьи"
	comment := commentOn(t, dev, issue, onTheIssue)
	articleComment := commentOn(t, dev, article, onTheArticle)

	tests := []struct {
		name    string
		owner   string
		comment string
	}{
		{name: "another issue", owner: otherIssue, comment: comment},
		{name: "another article", owner: otherArticle, comment: articleComment},
		{name: "an article for the comment of an issue", owner: article, comment: comment},
		{name: "an issue for the comment of an article", owner: issue, comment: articleComment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := len(dev.requests())

			got := runWith(t, dev.env(), "comment", "update", tc.owner, tc.comment, "--text", "ytrack contract x")

			assert.Equal(t, "not_found", requireFault(t, got).code)
			assert.Len(t, dev.requests()[before:], 1)
		})
	}

	assert.Equal(t, onTheIssue, textOfTheComment(t, dev, []string{"issue", "show", issue, "--fields", "idReadable"}, comment))
	assert.Equal(t, onTheArticle,
		textOfTheComment(t, dev, []string{"article", "show", article, "--fields", "idReadable"}, articleComment))
}

func TestCommentUpdateRefusesAnIDWithALeadingZero(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")
	class, number, found := strings.Cut(comment, "-")
	require.True(t, found, "the id of a comment is a class, a dash and a number: %q", comment)
	before := len(dev.requests())

	got := runWith(t, dev.env(), "comment", "update", issue, class+"-0"+number, "--text", "ytrack contract x")

	assert.Equal(t, "not_found", requireFault(t, got).code)
	assert.Len(t, dev.requests()[before:], 1)
}

func TestCommentUpdateRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	const first = "ytrack contract первая"
	comment := commentOn(t, dev, issue, first)
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"comment", "update", issue, comment, "--text", "ytrack contract x")

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, detail{"upstream_message", "Entity with id " + issue + " not found"}, found.details[3])
	assert.Len(t, dev.requests()[before:], 1)

	assert.Equal(t, first, textOfTheComment(t, dev, []string{"issue", "show", issue, "--fields", "idReadable"}, comment))
}
