package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func commentDeletionRequest(address, owner, comment string) string {
	return "DELETE " + address + "/api/issues/" + owner + "/comments/" + comment
}

func removingAComment(t *testing.T, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodDelete, r.Method, "a comment is removed by one DELETE and nothing else") {
			return
		}
		deletion(w, r)
	})
}

func TestCommentDeleteRefusesAnIDThatIsNoInternalID(t *testing.T) {
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

			got := runWith(t, server.Env(), "comment", "delete", "--", "DEV-1", tc.id)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestCommentDeleteRemovesACommentOfAnIssueInOneRequest(t *testing.T) {
	t.Parallel()
	server := removingAComment(t, deletionDone())

	got := runWith(t, server.Env(), "comment", "delete", "dev-7", "7-12")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "id: \"7-12\"\n", got.stdout)
	assert.Equal(t, []string{"/api/issues/dev-7/comments/7-12"}, server.Paths())
	assert.Empty(t, server.Request(t, 0).URL.RawQuery, "the specification declares no parameter for the removal")
	assert.Equal(t, []string{""}, server.Bodies())
}

func TestCommentDeleteReadsTheAnswerOfTheRemoval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		deletion http.HandlerFunc
		code     string
		exit     int
	}{
		{
			name:     "a comment the server has none of",
			deletion: fake.JSON(http.StatusNotFound, entityNotFound("7-12")),
			code:     "not_found",
			exit:     1,
		},
		{
			name: "a token that may see the issue and not remove the comment",
			deletion: fake.JSON(http.StatusForbidden,
				`{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`),
			code: "denied",
			exit: 1,
		},
		{
			name:     "an answer carrying an object",
			deletion: fake.JSON(http.StatusOK, `{"x":1}`),
			code:     "upstream_invalid",
			exit:     2,
		},
		{
			name:     "a page under a 200",
			deletion: gateway(http.StatusOK),
			code:     "upstream_invalid",
			exit:     2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removingAComment(t, tc.deletion)

			got := runWith(t, server.Env(), "comment", "delete", "DEV-7", "7-12")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", commentDeletionRequest(server.URL, "DEV-7", "7-12")},
				found.details[0])
			assert.Equal(t, []string{http.MethodDelete}, sentMethods(server))
		})
	}
}
