package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const removedWorkItemFields = "id,issue(idReadable)"

func workItemReadRequest(address, issue, id string) string {
	return "GET " + address + workItemPath(issue, id) + "?fields=" + removedWorkItemFields
}

func workItemOfAnIssue(id, issue string) string {
	return `{"$type":"IssueWorkItem","id":` + strconv.Quote(id) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(issue) + `}}`
}

func removingTime(t *testing.T, read, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, readThenDeletion(read, deletion))
}

func TestTimeDeleteReadsTheWorkItemAndThenRemovesIt(t *testing.T) {
	t.Parallel()
	server := removingTime(t, fake.JSON(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")), deletionDone())

	got := runWith(t, server.Env(), "time", "delete", "dev-1", "199-7")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "id: \"199-7\"\nissue:\n  idReadable: \"DEV-1\"\n", got.stdout)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		workItemPath("dev-1", "199-7") + "?fields=" + removedWorkItemFields,
		workItemPath("DEV-1", "199-7") + "?",
	}, server.Targets())
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestTimeDeleteRemovesNothingByAReadOfAnotherShape(t *testing.T) {
	t.Parallel()
	read := workItemOfAnIssue("..", "DEV-1")
	server := removingTime(t, fake.JSON(http.StatusOK, read), fake.Unexpected(t))

	got := runWith(t, server.Env(), "time", "delete", "DEV-1", "199-7")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", workItemReadRequest(server.URL, "DEV-1", "199-7")},
			{"upstream_status", 200},
			{"upstream_body", read},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}
