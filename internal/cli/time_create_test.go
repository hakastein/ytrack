package cli_test

import (
	"cmp"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const sentWorkItemWriteFields = "id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date," +
	"issue(idReadable," + customFieldsFields + "),text"

const sentWorkItemSettingsFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name)," +
	"attributes(id,name,values(id,name)))))"

const sentWorkItemTypesFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name))))"

const workItemSettings = `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
	`"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings",` +
	`"workItemTypes":[{"$type":"WorkItemType","id":"8-1","name":"First"},` +
	`{"$type":"WorkItemType","id":"8-2","name":"Second"}],` +
	`"attributes":[{"$type":"WorkItemProjectAttribute","id":"9-1","name":"Mode","values":[` +
	`{"$type":"WorkItemAttributeValue","id":"9-2","name":"Solo"},` +
	`{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}]}]}}}}`

func workItemWriteRequest(address, issue, fields string) string {
	return "POST " + address + workItemsPath(issue) + "?fields=" + fields
}

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

func writingTime(t *testing.T, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a work item is written by one POST and nothing else") {
			return
		}
		write(w, r)
	})
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

func TestTimeCreateRefusesADurationLongerThanTheServerKeeps(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "time", "create", "DEV-1", "PT2147483648M")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestTimeCreateWritesTheWorkItemAndPrintsWhatTheServerKept(t *testing.T) {
	t.Parallel()
	spent := receivedField{name: "Spent", valueType: "period", value: `{"$type":"DurationValue","minutes":90}`}
	server := writingTimeAgainstTheSettings(t, fake.JSON(http.StatusOK, answeredWorkItem{
		workType: `{"$type":"WorkItemType","id":"8-1","name":"First"}`,
		attributes: `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
			`"value":{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}}]`,
		text:   asJSON("first\nsecond"),
		fields: spent.sent(),
	}.json()))

	got := runWith(t, server.Env(), "time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01",
		"--text", "first\nsecond", "--type", "First", "--attribute", "Mode=Pair")

	assert.Equal(t, outcome{stdout: "id: \"199-7\"\n" +
		"duration: \"PT1H30M\"\n" +
		"type:\n  name: \"First\"\n" +
		"attributes:\n  \"Mode\": \"Pair\"\n" +
		"author:\n  login: \"author\"\n" +
		"date: \"2026-09-01T00:00:00Z\"\n" +
		"issue:\n  idReadable: \"DEV-1\"\n  customFields:\n    \"Spent\": \"PT1H30M\"\n" +
		"text: |-\n  first\n  second\n"}, got)
	assert.Equal(t, []string{
		"/api/issues/DEV-1?fields=" + sentWorkItemSettingsFields,
		workItemsPath("DEV-1") + "?fields=id,duration(minutes),type(name,id),attributes(id,name,value(id,name))," +
			"author(login),date,issue(idReadable," + customFieldsFields + "),text",
	}, server.Targets())
	assert.Equal(t, http.MethodPost, server.Last(t).Method)
	assert.Equal(t, `{"duration":{"minutes":90},"type":{"id":"8-1"},"date":1788264000000,"text":"first\nsecond",`+
		`"attributes":[{"id":"9-1","value":{"id":"9-3"}}]}`, server.Last(t).Body)
}

func TestTimeCreateRefusesATypeTheProjectDoesNotHave(t *testing.T) {
	t.Parallel()
	server := writingTimeAgainstTheSettings(t, noWorkItemWritten(t))

	got := runWith(t, server.Env(), "time", "create", "DEV-1", "PT1H", "--type", "Secnd")

	assert.Equal(t, faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", sentWorkItemTypesFields)},
			{"project", "DEV"},
			{"unknown", []any{[]detail{{"type", "Secnd"}, {"nearest", []any{"Second"}}}}},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTimeCreateRefusesADurationTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	server := writingTime(t, fake.JSON(http.StatusOK, answeredWorkItem{
		duration: `{"$type":"DurationValue","minutes":60}`,
	}.json()))

	got := runWith(t, server.Env(), "time", "create", "DEV-1", "PT1H30M")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", workItemWriteRequest(server.URL, "DEV-1", sentWorkItemWriteFields)},
			{"issue", "DEV-1"},
			{"id", "199-7"},
			{"mismatch", []any{[]detail{{"field", "duration"}, {"expected", "PT1H30M"}, {"actual", "PT1H"}}}},
		},
	}, requireUncertainty(t, got))
}

func noWorkItemWritten(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a work item was written", "%s %s", r.Method, r.URL)
	}
}
