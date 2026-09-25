package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/youtrack/fake"
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

func updatingAComment(t *testing.T, read, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestCommentUpdateReadsAnIssueCommentBeforeWritingItAndPrintsTheComment(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
		fake.JSON(http.StatusOK, createdComment("7-12", hostileText)))

	got := runWith(t, server.Env(), "comment", "update", "dev-7", "7-12", "--text", hostileText)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"/api/issues/dev-7/comments/7-12", "/api/issues/dev-7/comments/7-12"},
		server.Paths())
	assert.Equal(t, []string{commentDeletedFields, writtenCommentFields}, server.Fields())
	assert.Equal(t, map[string]any{"text": hostileText}, server.LastJSON(t))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "author", "created", "updated", "text"}, keysOf(mapping))
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, hostileText, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style, "a carriage return keeps text out of a literal block")
}

func TestCommentUpdateRefusesAReadThatSaysNothingOfDeleted(t *testing.T) {
	t.Parallel()
	const read = `{"$type":"IssueComment","deleted":null}`
	server := updatingAComment(t, fake.JSON(http.StatusOK, read), fake.Unexpected(t))

	got := runWith(t, server.Env(), "comment", "update", "DEV-7", "7-12", "--text", "x")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", commentReadRequest(server.URL, "DEV-7", "7-12")},
			{"upstream_status", 200},
			{"upstream_body", read},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}

func TestCommentUpdateChecksTheResponseAgainstTheSchemaOfTheOwner(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
		fake.JSON(http.StatusOK, createdComment("7-12", "Text")))

	got := runWith(t, server.Env(), "comment", "update", "DEV-7", "7-12", "--text", "Text",
		"--fields", "text,article(idReadable)")

	const asked = "text,article(idReadable)"
	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", commentWriteRequest(server.URL, "DEV-7", "7-12", asked)},
			{"fields", asked},
			{"unknown", []any{unknownEntry("article", issueCommentNames()...)}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}
