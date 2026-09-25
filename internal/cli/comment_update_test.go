package cli_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
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

func sentText(t *testing.T, u *fake.Server) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(u.Last(t).Body), &body))
	return body
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

func TestCommentUpdateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no text", argv: []string{"comment", "update", "DEV-1", "7-1"}},
		{name: "the text twice", argv: []string{"comment", "update", "DEV-1", "7-1", "--text", "a", "--text", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.Requests())
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
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "comment", "update", "DEV-1", "--text", "x", "--", tc.id)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCommentUpdateReadsAnIssueCommentBeforeWritingIt(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
		fake.JSON(http.StatusOK, createdComment("7-12", hostileText)))

	got := runWith(t, server.Env(), "comment", "update", "dev-7", "7-12", "--text", hostileText)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"/api/issues/dev-7/comments/7-12", "/api/issues/dev-7/comments/7-12"},
		server.Paths())
	assert.Equal(t, []string{commentDeletedFields, writtenCommentFields}, server.Fields())
	assert.Equal(t, map[string]any{"text": hostileText}, sentText(t, server))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "author", "created", "updated", "text"}, keysOf(mapping))
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, hostileText, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style, "a carriage return keeps text out of a literal block")
}

func TestCommentUpdateRefusesAReadThatSaysNothingOfDeleted(t *testing.T) {
	t.Parallel()
	const read = `{"$type":"IssueComment","deleted":null}`
	server := updatingAComment(t, fake.JSON(http.StatusOK, read), noUpdate(t))

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
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestCommentUpdateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	said := func(name, description string) string {
		return `{"error":` + strconv.Quote(name) + `,"error_description":` + strconv.Quote(description) + `}`
	}
	tests := []struct {
		name    string
		server  func(t *testing.T) *fake.Server
		argv    []string
		code    string
		methods []string
	}{
		{
			name: "a comment the read does not find",
			server: func(t *testing.T) *fake.Server {
				return updatingAComment(t, fake.JSON(http.StatusNotFound, entityNotFound("7-12")), noUpdate(t))
			},
			argv:    []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			code:    "not_found",
			methods: []string{http.MethodGet},
		},
		{
			name: "a comment of an article the server has none of",
			server: func(t *testing.T) *fake.Server {
				return commenting(t, fake.JSON(http.StatusNotFound, entityNotFound("8-5")))
			},
			argv:    []string{"comment", "update", "DEV-A-3", "8-5", "--text", "x"},
			code:    "not_found",
			methods: []string{http.MethodPost},
		},
		{
			name: "a token that may read the comment and not write it",
			server: func(t *testing.T) *fake.Server {
				return updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
					fake.JSON(http.StatusForbidden, said("Forbidden", "HTTP 403 Forbidden")))
			},
			argv:    []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			code:    "denied",
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "a body the server disagreed with",
			server: func(t *testing.T) *fake.Server {
				return updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
					fake.JSON(http.StatusBadRequest, said("bad_request", "Comment can't be empty.")))
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

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, tc.code, requireFault(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestCommentUpdateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
		fake.JSON(http.StatusOK, createdComment("7-12", "text")))

	got := runWith(t, server.Env(), "comment", "update", "DEV-7", "7-12", "--text", "Text")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", commentWriteRequest(server.URL, "DEV-7", "7-12", writtenCommentFields)},
			{"comment", "7-12"},
			{"mismatch", []any{[]detail{{"field", "text"}, {"expected", "Text"}, {"actual", "text"}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
}

func TestCommentUpdateChecksTheResponseAgainstTheSchemaOfTheOwner(t *testing.T) {
	t.Parallel()
	server := updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
		fake.JSON(http.StatusOK, createdComment("7-12", "Text")))

	got := runWith(t, server.Env(), "comment", "update", "DEV-7", "7-12", "--text", "Text",
		"--fields", "+article(idReadable)")

	asked := writtenCommentFields + ",article(idReadable)"
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
