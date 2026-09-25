package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func workItemPath(issue, id string) string {
	return workItemsPath(issue) + "/" + id
}

func workItemUpdateRequest(address, issue, id, fields string) string {
	return "POST " + address + workItemPath(issue, id) + "?fields=" + fields
}

func TestTimeUpdatePrintsTheDefaultFieldsOfTheWorkItem(t *testing.T) {
	t.Parallel()
	server := writingTime(t, fake.JSON(http.StatusOK, answeredWorkItem{id: "199-6", text: asJSON("x")}.json()))

	got := runWith(t, server.Env(), "time", "update", "DEV-1", "199-6", "--text", "x")

	assert.Equal(t, outcome{stdout: "id: \"199-6\"\n" +
		"duration: \"PT1H30M\"\n" +
		"type: null\n" +
		"attributes: {}\n" +
		"author:\n  login: \"author\"\n" +
		"date: \"2026-09-01T00:00:00Z\"\n" +
		"issue:\n  idReadable: \"DEV-1\"\n  customFields: {}\n" +
		"text: |-\n  x\n"}, got)
	assert.Equal(t, []string{sentWorkItemWriteFields}, server.Fields())
}

func TestTimeUpdateWritesTheNamedPartsAndPrintsWhatTheServerKept(t *testing.T) {
	t.Parallel()
	server := writingTimeAgainstTheSettings(t, fake.JSON(http.StatusOK, answeredWorkItem{
		id:       "199-6",
		duration: `{"$type":"DurationValue","minutes":120}`,
		workType: `{"$type":"WorkItemType","id":"8-2","name":"Second"}`,
		attributes: `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
			`"value":{"$type":"WorkItemAttributeValue","id":"9-2","name":"Solo"}}]`,
		date: "1788307200000",
		text: asJSON("x"),
	}.json()))

	got := runWith(t, server.Env(), "time", "update", "DEV-1", "199-6", "--duration", "PT2H", "--date", "2026-09-02",
		"--text", "x", "--type", "Second", "--attribute", "Mode=Solo", "--fields", "id,duration,date,text")

	assert.Equal(t, outcome{stdout: "id: \"199-6\"\nduration: \"PT2H\"\ndate: \"2026-09-02T00:00:00Z\"\ntext: |-\n  x\n"}, got)
	assert.Equal(t, []string{
		"/api/issues/DEV-1?fields=" + sentWorkItemSettingsFields,
		workItemPath("DEV-1", "199-6") + "?fields=id,duration(minutes),date,text,type(id,name)," +
			"attributes(id,name,value(id,name))",
	}, server.Targets(t))
	assert.Equal(t, http.MethodPost, server.Last(t).Method)
	assert.Equal(t, `{"duration":{"minutes":120},"type":{"id":"8-2"},"date":1788350400000,"text":"x",`+
		`"attributes":[{"id":"9-1","value":{"id":"9-2"}}]}`, server.Last(t).Body)
}

func TestTimeUpdateRefusesAnAttributeTheServerDidNotTakeAway(t *testing.T) {
	t.Parallel()
	server := writingTimeAgainstTheSettings(t, fake.JSON(http.StatusOK, answeredWorkItem{
		id: "199-6",
		attributes: `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
			`"value":{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}}]`,
	}.json()))

	got := runWith(t, server.Env(), "time", "update", "DEV-1", "199-6", "--clear", "Mode", "--fields", "id")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", workItemUpdateRequest(server.URL, "DEV-1", "199-6", "id,attributes(id,name,value(id,name))")},
			{"issue", "DEV-1"},
			{"id", "199-6"},
			{"mismatch", []any{[]detail{{"field", "Mode"}, {"expected", nil}, {"actual", "Pair"}}}},
		},
	}, requireUncertainty(t, got))
}
