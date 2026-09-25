package youtrack_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

type commentAnswers struct {
	read    string
	written string
	listed  string
}

func commentServer(t *testing.T, answers commentAnswers) *fake.Server {
	t.Helper()
	routes := http.NewServeMux()
	routes.HandleFunc("GET /api/{owners}/{owner}/comments", fake.JSON(http.StatusOK, answers.listed))
	routes.HandleFunc("GET /api/{owners}/{owner}/comments/{comment}", fake.JSON(http.StatusOK, answers.read))
	routes.HandleFunc("POST /", fake.JSON(http.StatusOK, answers.written))
	routes.HandleFunc("DELETE /", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return fake.Serve(t, routes.ServeHTTP)
}

func commentRoutes(server *fake.Server) []string {
	var routes []string
	for _, sent := range server.Requests() {
		routes = append(routes, sent.Method+" "+sent.URL.Path)
	}
	return routes
}

func commentWritten(t *testing.T, id, text string) string {
	t.Helper()
	written, err := json.Marshal(map[string]any{"$type": "IssueComment", "id": id, "text": text})
	require.NoError(t, err)
	return string(written)
}

const commentStanding = `{"$type":"IssueComment","deleted":false}`

func commentCreatedOnAnIssue(text string) (youtrack.Call, *diag.Fault) {
	return youtrack.CreateComment("DEV-7", text, new("id"))
}

func commentUpdatedOnAnIssue(text string) (youtrack.Call, *diag.Fault) {
	return youtrack.UpdateComment("DEV-7", "7-12", text, new("id"))
}

func TestParseCommentsReadsAllOrACount(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"all", "0", "7"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			comments, err := youtrack.ParseComments(text)

			require.NoError(t, err)
			assert.Equal(t, text, comments.String())
		})
	}
}

func TestParseCommentsRefusesAnythingElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{name: "a negative count", text: "-1"},
		{name: "a word of its own", text: "x"},
		{name: "nothing at all", text: ""},
		{name: "a fraction", text: "1.5"},
		{name: "a count past the largest int", text: "99999999999999999999"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comments, err := youtrack.ParseComments(tc.text)

			assert.Error(t, err)
			assert.Equal(t, youtrack.Comments{}, comments)
		})
	}
}

func TestCommentWritesRefuseATextTheyWillNotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func() (youtrack.Call, *diag.Fault)
	}{
		{name: "an empty text of a new comment", call: func() (youtrack.Call, *diag.Fault) {
			return commentCreatedOnAnIssue("")
		}},
		{name: "a new comment that is no UTF-8", call: func() (youtrack.Call, *diag.Fault) {
			return commentCreatedOnAnIssue("First\xffSecond")
		}},
		{name: "an empty text of a comment rewritten", call: func() (youtrack.Call, *diag.Fault) {
			return commentUpdatedOnAnIssue("")
		}},
		{name: "a comment rewritten with no UTF-8", call: func() (youtrack.Call, *diag.Fault) {
			return commentUpdatedOnAnIssue("First\xffSecond")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := tc.call()

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCommentCallsAddressTheCommentsOfTheOwnerTheyNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		call   func() (youtrack.Call, *diag.Fault)
		routes []string
	}{
		{
			name:   "a creation on an issue",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.CreateComment("DEV-7", "Text", new("id")) },
			routes: []string{"POST /api/issues/DEV-7/comments"},
		},
		{
			name:   "a creation on an article",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.CreateComment("DEV-A-3", "Text", new("id")) },
			routes: []string{"POST /api/articles/DEV-A-3/comments"},
		},
		{
			name: "a rewrite on an issue, which reads the comment first",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateComment("DEV-7", "7-12", "Text", new("id"))
			},
			routes: []string{"GET /api/issues/DEV-7/comments/7-12", "POST /api/issues/DEV-7/comments/7-12"},
		},
		{
			name: "a rewrite on an article",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateComment("DEV-A-3", "8-5", "Text", new("id"))
			},
			routes: []string{"POST /api/articles/DEV-A-3/comments/8-5"},
		},
		{
			name:   "a removal from an issue",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.DeleteComment("DEV-7", "7-12") },
			routes: []string{"DELETE /api/issues/DEV-7/comments/7-12"},
		},
		{
			name:   "a removal from an article",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.DeleteComment("DEV-A-3", "8-5") },
			routes: []string{"DELETE /api/articles/DEV-A-3/comments/8-5"},
		},
		{
			name: "a list of an issue",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListComments("DEV-7", new("id"), youtrack.Page{Limit: 1})
			},
			routes: []string{"GET /api/issues/DEV-7/comments"},
		},
		{
			name: "a list of an article",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListComments("DEV-A-3", new("id"), youtrack.Page{Limit: 1})
			},
			routes: []string{"GET /api/articles/DEV-A-3/comments"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{
				read:    commentStanding,
				written: `{"id":"7-12","text":"Text"}`,
				listed:  `[]`,
			})
			call, fault := tc.call()
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.routes, commentRoutes(server))
		})
	}
}

func TestCommentWritesSendTheTextAsGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		write func(text string) (youtrack.Call, *diag.Fault)
	}{
		{name: "one space", text: " ", write: commentCreatedOnAnIssue},
		{name: "one line feed", text: "\n", write: commentCreatedOnAnIssue},
		{name: "a vote", text: "+1", write: commentCreatedOnAnIssue},
		{name: "markup that reads as a tag", text: "[Tag] Title", write: commentCreatedOnAnIssue},
		{name: "a NUL", text: "First\x00Second", write: commentCreatedOnAnIssue},
		{name: "a new comment the server keeps byte for byte", text: articleAndCommentText, write: commentCreatedOnAnIssue},
		{name: "a comment rewritten byte for byte", text: articleAndCommentText, write: commentUpdatedOnAnIssue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{read: commentStanding, written: commentWritten(t, "7-12", tc.text)})
			call, fault := tc.write(tc.text)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			var sent map[string]any
			require.NoError(t, json.Unmarshal([]byte(server.Last(t).Body), &sent))
			assert.Equal(t, map[string]any{"text": tc.text}, sent)
		})
	}
}

func TestCommentWritesRefuseAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		call    func() (youtrack.Call, *diag.Fault)
		written string
		method  string
		target  string
		actual  *render.Node
	}{
		{
			name:    "a new comment the server stored in another letter case",
			call:    func() (youtrack.Call, *diag.Fault) { return commentCreatedOnAnIssue("Text") },
			written: `{"$type":"IssueComment","id":"7-12","text":"text"}`,
			method:  http.MethodPost,
			target:  "/api/issues/DEV-7/comments?fields=id,text",
			actual:  render.NewString("text"),
		},
		{
			name:    "a new comment the server kept no text of",
			call:    func() (youtrack.Call, *diag.Fault) { return commentCreatedOnAnIssue("Text") },
			written: `{"$type":"IssueComment","id":"7-12","text":null}`,
			method:  http.MethodPost,
			target:  "/api/issues/DEV-7/comments?fields=id,text",
			actual:  render.NewNull(),
		},
		{
			name:    "a comment taken back between the read and the rewrite",
			call:    func() (youtrack.Call, *diag.Fault) { return commentUpdatedOnAnIssue("Text") },
			written: `{"$type":"IssueComment","id":"7-12","text":null}`,
			method:  http.MethodPost,
			target:  "/api/issues/DEV-7/comments/7-12?fields=id,text",
			actual:  render.NewNull(),
		},
		{
			name:    "a rewrite answered under another id than the one it was addressed by",
			call:    func() (youtrack.Call, *diag.Fault) { return commentUpdatedOnAnIssue("Text") },
			written: `{"$type":"IssueComment","id":"7-99","text":"Other"}`,
			method:  http.MethodPost,
			target:  "/api/issues/DEV-7/comments/7-12?fields=id,text",
			actual:  render.NewString("Other"),
		},
		{
			name: "a rewrite answered with no id, which the expression does not ask for",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateComment("DEV-7", "7-12", "Text", new("author(login)"))
			},
			written: `{"$type":"IssueComment","author":{"$type":"User","login":"author"},"text":"Other"}`,
			method:  http.MethodPost,
			target:  "/api/issues/DEV-7/comments/7-12?fields=author(login),text",
			actual:  render.NewString("Other"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{read: commentStanding, written: tc.written})
			call, fault := tc.call()
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			mismatch := render.NewMap(
				render.Pair{Key: "field", Value: render.NewString("text")},
				render.Pair{Key: "expected", Value: render.NewString("Text")},
				render.Pair{Key: "actual", Value: tc.actual})
			want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
				{Key: "request", Value: render.NewString(tc.method + " " + server.URL + tc.target)},
				{Key: "comment", Value: render.NewString("7-12")},
				{Key: "mismatch", Value: render.NewList(mismatch)},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestUpdateCommentWritesNothingWhereTheReadSaysNo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		read    string
		code    diag.Code
		details []render.Pair
	}{
		{
			name:    "a comment taken back",
			read:    `{"$type":"IssueComment","deleted":true}`,
			code:    diag.BadUsage,
			details: []render.Pair{{Key: "comment", Value: render.NewString("7-12")}},
		},
		{
			name: "a null for whether it was taken back",
			read: `{"$type":"IssueComment","deleted":null}`,
			code: diag.UpstreamInvalid,
			details: []render.Pair{
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(`{"$type":"IssueComment","deleted":null}`)},
			},
		},
		{
			name: "a word for whether it was taken back",
			read: `{"$type":"IssueComment","deleted":"true"}`,
			code: diag.UpstreamInvalid,
			details: []render.Pair{
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(`{"$type":"IssueComment","deleted":"true"}`)},
			},
		},
		{
			name: "a number for whether it was taken back",
			read: `{"$type":"IssueComment","deleted":1}`,
			code: diag.UpstreamInvalid,
			details: []render.Pair{
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(`{"$type":"IssueComment","deleted":1}`)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{read: tc.read})
			call, fault := commentUpdatedOnAnIssue("Text")
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			request := render.Pair{Key: "request",
				Value: render.NewString("GET " + server.URL + "/api/issues/DEV-7/comments/7-12?fields=deleted")}
			want := diag.Fault{Code: tc.code, Details: append([]render.Pair{request}, tc.details...)}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{"GET /api/issues/DEV-7/comments/7-12"}, commentRoutes(server))
		})
	}
}

func TestCommentWritesCheckMoreThanTheyPrint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		call   func() (youtrack.Call, *diag.Fault)
		fields string
	}{
		{
			name: "a creation",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateComment("DEV-7", "Text", new("author(login)"))
			},
			fields: "author(login),id,text",
		},
		{
			name: "a rewrite",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateComment("DEV-7", "7-12", "Text", new("author(login)"))
			},
			fields: "author(login),text",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{
				read:    commentStanding,
				written: `{"$type":"IssueComment","id":"7-12","author":{"$type":"User","login":"author"},"text":"Text"}`,
			})
			call, fault := tc.call()
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			author := render.NewMap(render.Pair{Key: "login", Value: render.NewString("author")})
			assert.Equal(t, render.NewMap(render.Pair{Key: "author", Value: author}), node)
			assert.Equal(t, tc.fields, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestCommentCallsCheckTheAnswerAgainstTheSchemaOfTheOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		call       func() (youtrack.Call, *diag.Fault)
		method     string
		target     string
		fields     string
		unknown    string
		nearest    *render.Node
		afterWrite bool
	}{
		{
			name: "a flag only a comment of an issue carries, asked of a new comment of an article",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateComment("DEV-A-3", "Text", new("id,deleted"))
			},
			method:  http.MethodPost,
			target:  "/api/articles/DEV-A-3/comments?fields=id,deleted,text",
			fields:  "id,deleted,text",
			unknown: "deleted",
			nearest: texts("$type", "article", "attachments", "author", "created", "id", "pinned", "reactions",
				"text", "updated", "visibility"),
			afterWrite: true,
		},
		{
			name: "the owner of a comment of an article, asked of a new comment of an issue",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateComment("DEV-7", "Text", new("id,article(idReadable)"))
			},
			method:  http.MethodPost,
			target:  "/api/issues/DEV-7/comments?fields=id,article(idReadable),text",
			fields:  "id,article(idReadable),text",
			unknown: "article",
			nearest: texts("$type", "attachments", "author", "created", "deleted", "id", "issue", "pinned",
				"reactions", "text", "textPreview", "updated", "visibility"),
			afterWrite: true,
		},
		{
			name: "a flag only a comment of an issue carries, asked of the comments of an article",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListComments("DEV-A-3", new("id,deleted"), youtrack.Page{Limit: 1})
			},
			method:  http.MethodGet,
			target:  "/api/articles/DEV-A-3/comments?fields=id,deleted&$top=1",
			fields:  "id,deleted",
			unknown: "deleted",
			nearest: texts("$type", "article", "attachments", "author", "created", "id", "pinned", "reactions",
				"text", "updated", "visibility"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{
				written: `{"id":"7-12","text":"Text"}`,
				listed:  `[{"id":"8-5"}]`,
			})
			call, fault := tc.call()
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			unknown := render.NewMap(
				render.Pair{Key: "field", Value: render.NewString(tc.unknown)},
				render.Pair{Key: "nearest", Value: tc.nearest})
			want := diag.Fault{Code: diag.UnknownName, AfterWrite: tc.afterWrite, Details: []render.Pair{
				{Key: "request", Value: render.NewString(tc.method + " " + server.URL + tc.target)},
				{Key: "fields", Value: render.NewString(tc.fields)},
				{Key: "unknown", Value: render.NewList(unknown)},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestListCommentsAsksForDeletedOnlyOfAnIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		owner      string
		expression *string
		fields     string
	}{
		{name: "an issue", owner: "DEV-7", fields: "id,author(login),created,text,deleted"},
		{name: "an article", owner: "DEV-A-3", fields: "id,author(login),created,text"},
		{name: "an article, with an addition", owner: "DEV-A-3", expression: new("+updated"),
			fields: "id,author(login),created,text,updated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{listed: `[]`})
			call, fault := youtrack.ListComments(tc.owner, tc.expression, youtrack.Page{Limit: 1})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, []string{tc.fields}, server.Fields())
		})
	}
}

func TestListCommentsCountsTheCommentsOfAnArticleWhereTheyFillThePage(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			fake.JSON(http.StatusOK, `[{"id":"8-1"},{"id":"8-2"},{"id":"8-3"}]`)(w, r)
			return
		}
		fake.JSON(http.StatusOK, `[{"$type":"ArticleComment","id":"8-1","text":"Text"}]`)(w, r)
	})
	call, fault := youtrack.ListComments("DEV-A-3", new("id,text"), youtrack.Page{Limit: 1})
	require.Nil(t, fault)

	node, fault := call(t.Context(), client(t, server))

	require.Nil(t, fault)
	comment := render.NewMap(
		render.Pair{Key: "id", Value: render.NewString("8-1")},
		render.Pair{Key: "text", Value: render.NewString("Text")})
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "total", Value: render.NewNumber("3")},
		render.Pair{Key: "returned", Value: render.NewNumber("1")},
		render.Pair{Key: "truncated", Value: render.NewBool(true)},
		render.Pair{Key: "comments", Value: render.NewList(comment)}), node)
	assert.Equal(t, []string{"/api/articles/DEV-A-3/comments", "/api/articles/DEV-A-3/comments"}, server.Paths())
	assert.Equal(t, []url.Values{
		{"fields": {"id,text"}, "$top": {"1"}},
		{"fields": {"id"}, "$top": {"-1"}},
	}, server.Queries())
}
