package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
)

const writtenCommentFields = "id,author(login),created,updated,text"

func issueCommentRequest(address, owner, fields string) string {
	return "POST " + address + "/api/issues/" + owner + "/comments?fields=" + fields
}

func answeredComment(schema, id, text string) string {
	return `{"$type":` + strconv.Quote(schema) + `,"id":` + strconv.Quote(id) +
		`,"author":{"$type":"User","login":"admin"},"created":1789035410875,"updated":null,"text":` + asJSON(text) + `}`
}

func createdComment(id, text string) string {
	return answeredComment("IssueComment", id, text)
}

func writtenArticleComment(id, text string) string {
	return answeredComment("ArticleComment", id, text)
}

func issueCommentNames() []any {
	return []any{"$type", "attachments", "author", "created", "deleted", "id", "issue", "pinned", "reactions",
		"text", "textPreview", "updated", "visibility"}
}

func articleCommentNames() []any {
	return []any{"$type", "article", "attachments", "author", "created", "id", "pinned", "reactions", "text",
		"updated", "visibility"}
}

func commenting(t *testing.T, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a comment is written by one POST and nothing else") {
			return
		}
		write(w, r)
	})
}

func TestCommentCreateRefusesACallWithNoText(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "comment", "create", "DEV-1")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestCommentCreateWritesOnAnIssueInOneRequest(t *testing.T) {
	t.Parallel()
	server := commenting(t, fake.JSON(http.StatusOK, createdComment("7-12", hostileText)))

	got := runWith(t, server.Env(), "comment", "create", "DEV-7", "--text", hostileText)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"/api/issues/DEV-7/comments"}, server.Paths())
	assert.Equal(t, []string{writtenCommentFields}, server.Fields())
	assert.Equal(t, map[string]any{"text": hostileText}, server.LastJSON(t))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "author", "created", "updated", "text"}, keysOf(mapping))
	assert.Equal(t, "7-12", nodeAt(t, mapping, "id").Value)
	assert.Equal(t, "admin", nodeAt(t, mapping, "author", "login").Value)
	assert.Equal(t, "2026-09-10T10:16:50.875Z", nodeAt(t, mapping, "created").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "updated")))
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, hostileText, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style, "a carriage return keeps text out of a literal block")
}

func TestCommentCreateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	server := commenting(t, fake.JSON(http.StatusOK, createdComment("7-12", "text")))

	got := runWith(t, server.Env(), "comment", "create", "DEV-7", "--text", "Text")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", issueCommentRequest(server.URL, "DEV-7", writtenCommentFields)},
			{"comment", "7-12"},
			{"mismatch", []any{[]detail{{"field", "text"}, {"expected", "Text"}, {"actual", "text"}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodPost}, server.Methods())
}

func TestCommentCreateChecksTheResponseAgainstTheSchemaOfTheOwner(t *testing.T) {
	t.Parallel()
	server := commenting(t, fake.JSON(http.StatusOK, writtenArticleComment("8-5", "Text")))

	got := runWith(t, server.Env(), "comment", "create", "DEV-A-3", "--text", "Text", "--fields", "+deleted")

	asked := writtenCommentFields + ",deleted"
	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", "POST " + server.URL + "/api/articles/DEV-A-3/comments?fields=" + asked},
			{"fields", asked},
			{"unknown", []any{unknownEntry("deleted", articleCommentNames()...)}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}
