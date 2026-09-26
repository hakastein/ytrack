package cli_test

import (
	"cmp"
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const workItemSettings = `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
	`"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings",` +
	`"workItemTypes":[{"$type":"WorkItemType","id":"8-1","name":"First"},` +
	`{"$type":"WorkItemType","id":"8-2","name":"Second"}],` +
	`"attributes":[{"$type":"WorkItemProjectAttribute","id":"9-1","name":"Mode","values":[` +
	`{"$type":"WorkItemAttributeValue","id":"9-2","name":"Solo"},` +
	`{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}]}]}}}}`

type answeredWorkItem struct {
	id         string
	duration   string
	workType   string
	date       string
	text       string
	fields     string
	attributes string
}

func (a answeredWorkItem) json() string {
	return `{"$type":"IssueWorkItem","id":` + strconv.Quote(cmp.Or(a.id, "199-7")) +
		`,"duration":` + cmp.Or(a.duration, `{"$type":"DurationValue","minutes":90}`) +
		`,"type":` + cmp.Or(a.workType, "null") + `,"attributes":` + cmp.Or(a.attributes, "[]") +
		`,"author":{"$type":"User","login":"author"}` +
		`,"date":` + cmp.Or(a.date, "1788220800000") +
		`,"issue":{"$type":"Issue","idReadable":"DEV-1","customFields":[` + a.fields + `]}` +
		`,"text":` + cmp.Or(a.text, "null") + `}`
}

func writingTimeAgainstTheSettings(t *testing.T, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fake.JSON(http.StatusOK, workItemSettings)(w, r)
			return
		}
		write(w, r)
	})
}

func TestTimeCreateWritesTheWorkItemAndPrintsWhatTheServerKept(t *testing.T) {
	t.Parallel()
	server := writingTimeAgainstTheSettings(t, fake.JSON(http.StatusOK, answeredWorkItem{
		workType: `{"$type":"WorkItemType","id":"8-1","name":"First"}`,
		attributes: `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
			`"value":{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}}]`,
		text: asJSON("first\nsecond"),
	}.json()))

	got := runWith(t, envOf(server), "time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01",
		"--text", "first\nsecond", "--type", "First", "--attribute", "Mode=Pair")

	assert.Equal(t, outcome{stdout: "id: \"199-7\"\n" +
		"duration: \"PT1H30M\"\n" +
		"type:\n  name: \"First\"\n" +
		"attributes:\n  \"Mode\": \"Pair\"\n" +
		"author:\n  login: \"author\"\n" +
		"date: \"2026-09-01T00:00:00Z\"\n" +
		"issue:\n  idReadable: \"DEV-1\"\n  customFields: {}\n" +
		"text: |-\n  first\n  second\n"}, got)
	assert.Contains(t, server.Routes(), http.MethodPost+" "+workItemsPath("DEV-1"))
}

func TestTimeCreateRefusesArgumentsBeforeItAsksForAnything(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a duration of no period", argv: []string{"PT1H30"}},
		{name: "a duration in days", argv: []string{"P1D"}},
		{name: "a duration in seconds", argv: []string{"PT30S"}},
		{name: "a duration with no hours and no minutes", argv: []string{"PT"}},
		{name: "a duration longer than a work item holds", argv: []string{"PT153722868M"}},
		{name: "hours longer than a work item holds", argv: []string{"PT2562048H"}},
		{name: "an empty date", argv: []string{"PT1H", "--date", ""}},
		{name: "an empty type", argv: []string{"PT1H", "--type", ""}},
		{name: "an attribute with no =", argv: []string{"PT1H", "--attribute", "Mode"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), append([]string{"time", "create", "DEV-1"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}
