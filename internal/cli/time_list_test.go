package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const sentWorkItemFields = "id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date,text"

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

func TestTimeListAsksTheWorkItemsOfTheIssueInOneRequest(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

	got := runWith(t, server.Env(), "time", "list", "DEV-1")

	want := "total: 2\nreturned: 2\ntruncated: false\nworkItems:\n" + printedWorkItemRow + printedSecondRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet + " " + workItemsPath("DEV-1")}, server.Routes())
	assert.Equal(t, url.Values{"fields": {sentWorkItemFields}, "$top": {"50"}}, server.Request(t, 0).URL.Query())
}

func TestTimeListRefusesAnAnswerOfAnotherShape(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, listedWorkItem))

	got := runWith(t, server.Env(), "time", "list", "DEV-1")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "GET " + server.URL + workItemsPath("DEV-1") + "?fields=" + sentWorkItemFields + "&$top=50"},
			{"upstream_status", 200},
			{"upstream_body", listedWorkItem},
		},
	}, requireFault(t, got))
}

func TestTimeListRefusesANameUnderTheDuration(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "time", "list", "DEV-1", "--fields", "duration(minutes)")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}
