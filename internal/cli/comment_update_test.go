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

// The one field the read before a write of a comment of an issue asks for, and the whole of what it is sent for.
const takenBackFields = "deleted"

// The two requests a write of a comment of an issue goes out as, which are the ones a refusal about it names.
func commentReadRequest(address, owner, comment string) string {
	return "GET " + address + "/api/issues/" + owner + "/comments/" + comment + "?fields=" + takenBackFields
}

func commentWriteRequest(address, owner, comment, fields string) string {
	return "POST " + address + "/api/issues/" + owner + "/comments/" + comment + "?fields=" + fields
}

// What the read before the write answers: whether the comment was taken back by whoever wrote it, and nothing
// else, since nothing else was asked for.
func commentTakenBack(gone bool) string {
	return `{"$type":"IssueComment","deleted":` + strconv.FormatBool(gone) + `}`
}

// sentText is the body of the write, read as JSON reads it. The read before it carries none, so it is the last
// request of the call that holds one whether or not that read was sent.
func sentText(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body))
	return body
}

// updatingAComment is the server of a write of a comment of an issue: read answers the GET that settles whether
// the comment was taken back, and write the POST that follows it.
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

// What a write of a comment takes: two arguments, the owner and the id, and the text as the value of a
// flag. A single id is no address, so a call carrying one is short of an argument rather than given a bad one.
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

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// The id of a comment is held to its form before anything is sent: the generated client resolves the
// segment against the server, so an empty id or a traversal would reach an endpoint other than the comment
// that was named: an empty one adds a second comment.
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
		// The digits are ASCII and no others: these two look like a 7 and a 1 and are written in bytes for it.
		{name: "digits in full width", id: "\xef\xbc\x97-\xef\xbc\x91"},
		{name: "letters", id: "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "comment", "update", "DEV-1", "--text", "x", "--", tc.id)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// The text of a write is held to the same two things as the text of a creation, and to nothing else: the
// comment already exists, and neither an empty text nor a byte the encoder would rewrite reaches it.
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

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
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

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
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

// The whole of the command on an issue: the read that settles whether the comment was taken back, the
// write that follows it, a body of the text and nothing else, and the answer as the document. The text is the
// most argv carries.
func TestCommentUpdateReadsAnIssueCommentBeforeWritingIt(t *testing.T) {
	t.Parallel()
	text := filling(hostileComment, 131_071)
	server := updatingAComment(t, answer(http.StatusOK, commentTakenBack(false)),
		answer(http.StatusOK, writtenComment("7-12", text)))

	got := runWith(t, server.env(), "comment", "update", "dev-7", "7-12", "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"/api/issues/dev-7/comments/7-12", "/api/issues/dev-7/comments/7-12"},
		server.sentPaths())
	assert.Equal(t, []string{takenBackFields, writtenCommentFields}, server.sentFields())
	assert.Equal(t, map[string]any{"text": text}, sentText(t, server))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "author", "created", "updated", "text"}, keysOf(mapping))
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, text, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style, "a carriage return keeps text out of a literal block")
}

// An article keeps no comment its author took back, so there is nothing to ask about and the write is the
// whole command: one POST to the knowledge base and not a request near the issues.
func TestCommentUpdateWritesOnAnArticleInOneRequest(t *testing.T) {
	t.Parallel()
	server := commenting(t, answer(http.StatusOK, writtenArticleComment("8-5", "x")))

	got := runWith(t, server.env(), "comment", "update", "DEV-A-3", "8-5", "--text", "x")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"/api/articles/DEV-A-3/comments/8-5"}, server.sentPaths())
	assert.NotContains(t, strings.Join(server.sentPaths(), " "), "/api/issues")
	assert.Equal(t, map[string]any{"text": "x"}, sentText(t, server))
}

// A comment of an issue its author took back still answers a write with a 200 and changes the text where
// nothing prints it, so the read before the write is what stands between the caller and a write into the
// invisible. Nothing is sent after it, and the refusal names the comment the caller addressed.
func TestCommentUpdateRefusesACommentTakenBack(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, answer(http.StatusOK, commentTakenBack(true)), noUpdate(t))

	got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "x")

	want := refusal{
		code: "bad_usage",
		details: []detail{
			{"request", commentReadRequest(server.url, "DEV-7", "7-12")},
			{"comment", "7-12"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// Не-булево deleted прошло бы как «комментарий не удалён», и запись ушла бы в невидимый комментарий.
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
			server := updatingAComment(t, answer(http.StatusOK, tc.read), noUpdate(t))

			got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "x")

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", commentReadRequest(server.url, "DEV-7", "7-12")},
					{"upstream_status", 200},
					{"upstream_body", tc.read},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// What the server says passes on word for word wherever it says it, and a read that found no comment is
// the end of the call: the write never goes out, so the caller may fix the address and send the same call again.
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
				return updatingAComment(t, answer(http.StatusNotFound, entityNotFound("7-12")), noUpdate(t))
			},
			argv:    []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			code:    "not_found",
			methods: []string{http.MethodGet},
		},
		{
			name: "a comment of an article the server has none of",
			server: func(t *testing.T) *upstream {
				return commenting(t, answer(http.StatusNotFound, entityNotFound("8-5")))
			},
			argv:    []string{"comment", "update", "DEV-A-3", "8-5", "--text", "x"},
			code:    "not_found",
			methods: []string{http.MethodPost},
		},
		{
			name: "a token that may read the comment and not write it",
			server: func(t *testing.T) *upstream {
				return updatingAComment(t, answer(http.StatusOK, commentTakenBack(false)),
					answer(http.StatusForbidden, said("Forbidden", "HTTP 403 Forbidden")))
			},
			argv:    []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			code:    "denied",
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "a body the server disagreed with",
			server: func(t *testing.T) *upstream {
				return updatingAComment(t, answer(http.StatusOK, commentTakenBack(false)),
					answer(http.StatusBadRequest, said("bad_request", "Comment can't be empty.")))
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

			assert.Equal(t, tc.code, requireRefusal(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The read before the write is no promise about the moment of the write: a comment taken back in between
// is answered with a 200 that carries no text, and the check of the answer is what catches it. The comment is
func TestCommentUpdateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	takenBackMeanwhile := `{"$type":"IssueComment","id":"7-12","author":{"$type":"User","login":"admin"},` +
		`"created":1789035410875,"updated":1789035999000,"text":null,"deleted":true}`
	tests := []struct {
		name    string
		asked   []string
		fields  string
		written string
		arrived any
	}{
		{
			name:    "a comment taken back between the read and the write",
			fields:  writtenCommentFields,
			written: takenBackMeanwhile,
			arrived: nil,
		},
		{
			name:    "an answer standing under another id than the write was addressed by",
			fields:  writtenCommentFields,
			written: writtenComment("7-99", "другое"),
			arrived: "другое",
		},
		{
			name:    "an answer carrying no id, which no expression of the caller's asks for",
			asked:   []string{"--fields", "author(login)"},
			fields:  "author(login),text",
			written: `{"$type":"IssueComment","author":{"$type":"User","login":"admin"},"text":"другое"}`,
			arrived: "другое",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := updatingAComment(t, answer(http.StatusOK, commentTakenBack(false)),
				answer(http.StatusOK, tc.written))

			argv := append([]string{"comment", "update", "DEV-7", "7-12", "--text", "первая"}, tc.asked...)
			got := runWith(t, server.env(), argv...)

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", commentWriteRequest(server.url, "DEV-7", "7-12", tc.fields)},
					{"comment", "7-12"},
					{"mismatch", []any{[]detail{{"field", "text"}, {"written", "первая"}, {"arrived", tc.arrived}}}},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
		})
	}
}

func TestCommentUpdateJudgesTheAnswerAtTheSchemaOfTheOwner(t *testing.T) {
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
				return commenting(t, answer(http.StatusOK, writtenArticleComment("8-5", "первая")))
			},
			argv:    []string{"comment", "update", "DEV-A-3", "8-5", "--text", "первая", "--fields", "+deleted"},
			unknown: unknownEntry("deleted", articleCommentNames()...),
		},
		{
			name: "the owner of a comment of an article, asked of an issue",
			server: func(t *testing.T) *upstream {
				return updatingAComment(t, answer(http.StatusOK, commentTakenBack(false)),
					answer(http.StatusOK, writtenComment("7-12", "первая")))
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

// What the caller asks to print and what the check of the write reads are two things: the text that went
// out is asked for whatever the expression says, and only the expression reaches the document. The owner
// stands in no document of a comment, and a caller who wants it there asks for it.
func TestCommentUpdateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, answer(http.StatusOK, commentTakenBack(false)),
		answer(http.StatusOK, writtenComment("7-12", "первая")))

	got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "первая",
		"--fields", "author(login)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "author:\n  login: \"admin\"\n", got.stdout)
	assert.Equal(t, []string{takenBackFields, "author(login),text"}, server.sentFields())
}

// commentedArticle is the fixture of a contract test that needs an article of its own to write comments on: it
// is filed by the command that files articles and taken away again afterwards.
func commentedArticle(t *testing.T, dev *upstream, role string) string {
	t.Helper()
	article := fileArticle(t, dev, contractCommentOwner(t, role))
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeArticle(t, dev, article) })
	return article
}

// commentOn writes one comment on an owner of the polygon and gives back the id the server put it under, which
// is the only way a test learns an id: a refused creation eats one, so no id is ever counted on beforehand.
func commentOn(t *testing.T, dev *upstream, owner, text string) string {
	t.Helper()
	got := runWith(t, dev.env(), "comment", "create", owner, "--text", text)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	id := nodeAt(t, requireMapping(t, "stdout", got.stdout), "id").Value
	require.Regexp(t, commentIDForm, id)
	return id
}

// textOfTheComment is the text the polygon holds under that comment of that owner, read back through the show
// of the owner, which is where comments are read from at all.
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

// A comment of an issue of the polygon written afresh, carriage returns and trailing spaces and all: the
// text comes back byte for byte, the moment of the write stands under updated, and the moment the comment was
// first written stands untouched.
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

// The same on an article of the polygon, with a line separator in the text: an article has no comment
// taken back to ask about, so the write is the whole call, and the polygon keeps the separator as it went out.
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

// The server checks which owner a comment hangs from, on both kinds and across them, and answers a comment
// of somebody else's owner as if it were not there at all. One request settles it, so ytrack neither guesses
// nor asks twice, and neither comment is touched.
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

			assert.Equal(t, "not_found", requireRefusal(t, got).code)
			assert.Len(t, dev.requests()[before:], 1)
		})
	}

	assert.Equal(t, onTheIssue, textOfTheComment(t, dev, []string{"issue", "show", issue, "--fields", "idReadable"}, comment))
	assert.Equal(t, onTheArticle,
		textOfTheComment(t, dev, []string{"article", "show", article, "--fields", "idReadable"}, articleComment))
}

// The polygon matches the id of a comment exactly: a leading zero in the number names no comment at all,
// which is why the form lets one through rather than reading the number and deciding for the server.
func TestCommentUpdateRefusesAnIDWithALeadingZero(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	comment := commentOn(t, dev, issue, "ytrack contract первая")
	class, number, found := strings.Cut(comment, "-")
	require.True(t, found, "the id of a comment is a class, a dash and a number: %q", comment)
	before := len(dev.requests())

	got := runWith(t, dev.env(), "comment", "update", issue, class+"-0"+number, "--text", "ytrack contract x")

	assert.Equal(t, "not_found", requireRefusal(t, got).code)
	assert.Len(t, dev.requests()[before:], 1)
}

// A token that may not see the issue is answered as if the issue were not there, at the read before the
// write, so nothing is written and the comment stands as the admin left it.
func TestCommentUpdateRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := commentedIssue(t, dev, "issue")
	const first = "ytrack contract первая"
	comment := commentOn(t, dev, issue, first)
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"comment", "update", issue, comment, "--text", "ytrack contract x")

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, detail{"upstream_message", "Entity with id " + issue + " not found"}, found.details[3])
	assert.Len(t, dev.requests()[before:], 1)

	assert.Equal(t, first, textOfTheComment(t, dev, []string{"issue", "show", issue, "--fields", "idReadable"}, comment))
}
