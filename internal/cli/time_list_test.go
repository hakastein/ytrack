package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func workItemsPath(id string) string {
	return "/api/issues/" + id + "/timeTracking/workItems"
}

const (
	listedWorkItem = `{"author":{"login":"author","$type":"User"},"text":"First text","date":1788220800000,` +
		`"duration":{"minutes":90,"$type":"DurationValue"},` +
		`"attributes":[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode","value":null}],` +
		`"type":{"name":"First","$type":"WorkItemType"},"id":"199-6","$type":"IssueWorkItem"}`
	listedSecondWorkItem = `{"text":"Second text","$type":"IssueWorkItem","id":"199-7","date":1788307200000,` +
		`"duration":{"$type":"DurationValue","minutes":30},` +
		`"attributes":[{"name":"Mode","$type":"WorkItemAttribute","id":"9-1",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}}],` +
		`"type":{"name":"Second","$type":"WorkItemType"},"author":{"login":"second.author","$type":"User"}}`
)

const (
	printedWorkItemRow = `  - {id: "199-6", duration: "PT1H30M", type: {name: "First"}, attributes: {"Mode": null}, ` +
		`author: {login: "author"}, date: "2026-09-01T00:00:00Z", text: "First text"}` + "\n"
	printedSecondRow = `  - {id: "199-7", duration: "PT30M", type: {name: "Second"}, attributes: {"Mode": "Pair"}, ` +
		`author: {login: "second.author"}, date: "2026-09-02T00:00:00Z", text: "Second text"}` + "\n"
)

func TestTimeListPrintsTheWorkItemsOfTheIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

	got := runWith(t, envOf(server), "time", "list", "DEV-1")

	want := "total: 2\nreturned: 2\ntruncated: false\nworkItems:\n" + printedWorkItemRow + printedSecondRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), http.MethodGet+" "+workItemsPath("DEV-1"))
}
