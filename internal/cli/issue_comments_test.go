package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestIssueShowPrintsNoCommentsAtZero(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1"}`))

	got := runWith(t, envOf(server), "issue", "show", "DEV-1", "--fields", "idReadable", "--comments=0")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\n"}, got)
}

func TestIssueShowRefusesACommentCountItCannotReadBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		count string
	}{
		{name: "a negative count", count: "-1"},
		{name: "a count larger than any number of comments", count: "99999999999999999999"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), "issue", "show", "DEV-1", "--comments="+tc.count)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}
