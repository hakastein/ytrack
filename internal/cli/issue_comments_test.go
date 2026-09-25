package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const commentFields = "comments(id,author(login),created,text,deleted)"

func TestIssueShowRefusesACommentsFlagThatIsNeitherAllNorACount(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=every")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestIssueShowRefusesCommentsAskedForInTheExpression(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--fields", "+comments")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestIssueShowAsksForNoCommentsAtZero(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1"}`))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--fields", "idReadable", "--comments=0")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\n"}, got)
	assert.Equal(t, []string{"idReadable"}, server.Fields())
}

func TestIssueShowRefusesCommentsTheServerShapedOtherwise(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","idReadable":"DEV-1","comments":[null]}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--fields", "idReadable")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", "idReadable,"+commentFields)},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}, requireFault(t, got))
}
