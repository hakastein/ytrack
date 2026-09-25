package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func removingAComment(t *testing.T, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodDelete, r.Method, "a comment is removed by one DELETE and nothing else") {
			return
		}
		deletion(w, r)
	})
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
