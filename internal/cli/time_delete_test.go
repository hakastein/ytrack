package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func workItemOfAnIssue(id, issue string) string {
	return `{"$type":"IssueWorkItem","id":` + strconv.Quote(id) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(issue) + `}}`
}

func TestTimeDeleteRemovesTheWorkItemAndPrintsIt(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, readThenDeletion(fake.JSON(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")), deletionDone()))

	got := runWith(t, envOf(server), "time", "delete", "DEV-1", "199-7")

	assert.Equal(t, outcome{stdout: "id: \"199-7\"\nissue:\n  idReadable: \"DEV-1\"\n"}, got)
	assert.Contains(t, server.Routes(), http.MethodDelete+" "+workItemPath("DEV-1", "199-7"))
}
