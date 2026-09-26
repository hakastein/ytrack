package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func workItemPath(issue, id string) string {
	return workItemsPath(issue) + "/" + id
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

	got := runWith(t, envOf(server), "time", "update", "DEV-1", "199-6", "--duration", "PT2H", "--date", "2026-09-02",
		"--text", "x", "--type", "Second", "--attribute", "Mode=Solo", "--fields", "id,duration,date,text")

	assert.Equal(t, outcome{stdout: "id: \"199-6\"\nduration: \"PT2H\"\ndate: \"2026-09-02T00:00:00Z\"\ntext: |-\n  x\n"}, got)
	assert.Contains(t, server.Routes(), http.MethodPost+" "+workItemPath("DEV-1", "199-6"))
}

func TestTimeUpdateEmptiesTheTextTheTypeAndAnAttributeByName(t *testing.T) {
	t.Parallel()
	server := writingTimeAgainstTheSettings(t, fake.JSON(http.StatusOK, answeredWorkItem{
		id:         "199-6",
		attributes: `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode","value":null}]`,
	}.json()))

	got := runWith(t, envOf(server), "time", "update", "DEV-1", "199-6", "--clear", "Text", "--clear", "type",
		"--clear", "Mode", "--fields", "id,type,attributes,text")

	assert.Equal(t, outcome{stdout: "id: \"199-6\"\ntype: null\nattributes:\n  \"Mode\": null\ntext: null\n"}, got)
	assert.Contains(t, server.Routes(), http.MethodPost+" "+workItemPath("DEV-1", "199-6"))
}

func TestTimeUpdateRefusesArgumentsBeforeItAsksForAnything(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a duration of no period", argv: []string{"--duration", "2h"}},
		{name: "an empty duration", argv: []string{"--duration", ""}},
		{name: "emptying the duration", argv: []string{"--clear", "duration"}},
		{name: "emptying the date", argv: []string{"--clear", "Date"}},
		{name: "emptying nothing", argv: []string{"--clear", ""}},
		{name: "an attribute with no =", argv: []string{"--attribute", "Mode"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), append([]string{"time", "update", "DEV-1", "199-6"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}
