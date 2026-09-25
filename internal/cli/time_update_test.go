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

func TestTimeUpdateRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing to write at all", argv: []string{}},
		{name: "the duration twice", argv: []string{"--duration", "PT1H", "--duration", "PT2H"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"time", "update", "DEV-1", "199-6"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTimeUpdateRefusesAnIDThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "the readable id of an issue", id: "DEV-1"},
		{name: "the readable id of an article", id: "DEV-A-1"},
		{name: "two dots", id: ".."},
		{name: "a number and a dash", id: "199-"},
		{name: "a number without a class", id: "-1"},
		{name: "a letter after the number", id: "199-1x"},
		{name: "a space before the id", id: " 199-1"},
		{name: "an underscore in place of the dash", id: "199_1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "time", "update", "--text", "x", "--", "DEV-1", tc.id)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTimeUpdateSendsAnIDWithALeadingZeroAsItWasWritten(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusNotFound, entityNotFound("199-06")))

	got := runWith(t, server.Env(), "time", "update", "DEV-1", "199-06", "--text", "x")

	assert.Equal(t, "not_found", requireFault(t, got).code)
	assert.Equal(t, []string{workItemPath("DEV-1", "199-06")}, server.Paths())
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
	}, server.Targets())
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

	got := runWith(t, server.Env(), "time", "update", "DEV-1", "199-6", "--clear", "Mode")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", workItemUpdateRequest(server.URL, "DEV-1", "199-6", sentWorkItemWriteFields)},
			{"issue", "DEV-1"},
			{"id", "199-6"},
			{"mismatch", []any{[]detail{{"field", "Mode"}, {"expected", nil}, {"actual", "Pair"}}}},
		},
	}, requireUncertainty(t, got))
}

func TestTimeUpdateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	server := writingTime(t, fake.JSON(http.StatusNotFound,
		`{"error":"Not Found","error_description":"Entity with id 199-6 not found"}`))

	got := runWith(t, server.Env(), "time", "update", "DEV-1", "199-6", "--text", "x")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", workItemUpdateRequest(server.URL, "DEV-1", "199-6", sentWorkItemWriteFields)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id 199-6 not found"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
}
