package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func workItemPath(issue, id string) string {
	return workItemsPath(issue) + "/" + id
}

func workItemUpdateRequest(address, issue, id, fields string) string {
	return "POST " + address + workItemPath(issue, id) + "?fields=" + fields
}

func workItemOn(t *testing.T, dev *upstream, issue string, argv ...string) string {
	t.Helper()
	got := runWith(t, dev.env(), append([]string{"time", "create", issue}, argv...)...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	id := nodeAt(t, requireMapping(t, "stdout", got.stdout), "id").Value
	require.Regexp(t, internalIDForm, id)
	return id
}

func theWorkItemsOf(t *testing.T, dev *upstream, issue string) []map[string]any {
	t.Helper()
	return requireWorkItemListing(t, runWith(t, dev.env(), "time", "list", issue)).WorkItems
}

func TestTimeUpdateRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing to write at all", argv: []string{}},
		{name: "the duration emptied", argv: []string{"--clear", "duration"}},
		{name: "the day emptied", argv: []string{"--clear", "date"}},
		{name: "nothing named to empty", argv: []string{"--clear", ""}},
		{name: "an attribute with no value", argv: []string{"--attribute", "Формат работы"}},
		{name: "an attribute emptied by an empty value", argv: []string{"--attribute", "Формат работы="}},
		{name: "an attribute set twice", argv: []string{"--attribute", "Формат работы=Сам", "--attribute", "формат работы=ИИагент"}},
		{name: "the type written and taken away", argv: []string{"--type", "Разработка", "--clear", "type"}},
		{name: "the text written and emptied", argv: []string{"--text", "x", "--clear", "TEXT"}},
		{name: "the duration twice", argv: []string{"--duration", "PT1H", "--duration", "PT2H"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"time", "update", "DEV-1", "199-6"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeUpdateRefusesAValueItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a length as the server shows one", argv: []string{"--duration", "1ч"}},
		{name: "a time of day", argv: []string{"--date", "2026-09-01T15:00:00Z"}},
		{name: "a name of no type at all", argv: []string{"--type", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"time", "update", "DEV-1", "199-6"}, tc.argv...)...)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
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
			server := serveNothing(t)

			got := runWith(t, server.env(), "time", "update", "--text", "x", "--", "DEV-1", tc.id)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeUpdateSendsAnIDWithALeadingZeroAsItWasWritten(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusNotFound, entityNotFound("199-06")))

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-06", "--text", "x")

	assert.Equal(t, "not_found", requireFault(t, got).code)
	assert.Equal(t, []string{workItemPath("DEV-1", "199-06")}, server.sentPaths())
}

func TestTimeUpdateSendsTheNamedPartsAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		argv     []string
		answered answeredWorkItem
		body     map[string]any
	}{
		{
			name:     "the text alone",
			argv:     []string{"--text", "x"},
			answered: answeredWorkItem{text: asJSON("x")},
			body:     map[string]any{"text": "x"},
		},
		{
			name:     "the text written empty",
			argv:     []string{"--text", ""},
			answered: answeredWorkItem{text: asJSON("")},
			body:     map[string]any{"text": ""},
		},
		{
			name: "the type taken away",
			argv: []string{"--clear", "TYPE"},
			body: map[string]any{"type": nil},
		},
		{
			name: "the text emptied",
			argv: []string{"--clear", "text"},
			body: map[string]any{"text": nil},
		},
		{
			name: "how long it is and the day it is written against",
			argv: []string{"--duration", "PT2H", "--date", "2026-09-02"},
			answered: answeredWorkItem{
				duration: `{"$type":"DurationValue","minutes":120}`,
				date:     "1788350400000",
			},
			body: map[string]any{
				"duration": map[string]any{"minutes": float64(120)},
				"date":     float64(1788350400000),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, respondWith(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), append([]string{"time", "update", "DEV-1", "199-6"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, tc.body, sentWorkItem(t, server))
			assert.Equal(t, []string{workItemPath("DEV-1", "199-6")}, server.sentPaths())
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

func TestTimeUpdateReadsTheTypesOfTheProjectBeforeTheWrite(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t,
		respondWith(http.StatusOK, devIssueWithWorkItemTypes()),
		respondWith(http.StatusOK, answeredWorkItem{
			workType: `{"$type":"WorkItemType","id":"` + workItemTypeID(1) + `","name":"Тестирование"}`,
		}.json()))

	got := runWith(t, server.env(), "time", "update", "dev-1", "199-6", "--type", "тестирование")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{
		"/api/issues/dev-1?fields=" + sentWorkItemTypesFields,
		workItemPath("DEV-1", "199-6") + "?fields=" + sentWorkItemWriteFieldsWithType,
	}, server.sentTargets())
	assert.Equal(t, map[string]any{"id": workItemTypeID(1)}, sentWorkItemType(t, server))
}

func TestTimeUpdateRefusesAPartTheServerDidNotEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		emptied  string
		answered answeredWorkItem
		mismatch []any
	}{
		{
			name:     "the type",
			emptied:  "type",
			answered: answeredWorkItem{workType: `{"$type":"WorkItemType","name":"Разработка"}`},
			mismatch: []any{[]detail{{"field", "type"}, {"expected", nil}, {"actual", "Разработка"}}},
		},
		{
			name:     "the text",
			emptied:  "text",
			answered: answeredWorkItem{text: asJSON("Разбор полигона")},
			mismatch: []any{[]detail{{"field", "text"}, {"expected", nil}, {"actual", "Разбор полигона"}}},
		},
		{
			name:     "the text emptied to an empty string",
			emptied:  "text",
			answered: answeredWorkItem{text: asJSON("")},
			mismatch: []any{[]detail{{"field", "text"}, {"expected", nil}, {"actual", ""}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, respondWith(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--clear", tc.emptied)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{"request", "issue", "id", "mismatch"}, detailKeys(found))
			assert.Equal(t, "DEV-1", detailNamed(t, found, "issue"))
			assert.Equal(t, "199-6", detailNamed(t, found, "id"))
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
		})
	}
}

func TestTimeUpdateAsksForWhatItChecksWhateverWasAskedToPrint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		argv     []string
		answered answeredWorkItem
		asked    string
	}{
		{
			name:     "the text",
			argv:     []string{"--text", "x"},
			answered: answeredWorkItem{text: asJSON("x")},
			asked:    "id,text",
		},
		{
			name:     "how long it is and the day it is written against",
			argv:     []string{"--duration", "PT2H", "--date", "2026-09-02"},
			answered: answeredWorkItem{duration: `{"$type":"DurationValue","minutes":120}`, date: "1788350400000"},
			asked:    "id,duration(minutes),date",
		},
		{name: "the text emptied", argv: []string{"--clear", "text"}, asked: "id,text"},
		{name: "the type taken away", argv: []string{"--clear", "type"}, asked: "id,type(name)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, respondWith(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), append([]string{"time", "update", "DEV-1", "199-6"},
				append(tc.argv, "--fields", "id")...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, `id: "199-7"`+"\n", got.stdout)
			assert.Equal(t, []string{tc.asked}, server.sentFields())
		})
	}
}

func TestTimeUpdateChecksNothingItNeverWrote(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{
		duration: `{"$type":"DurationValue","minutes":45}`,
		workType: `{"$type":"WorkItemType","name":"Кодревью"}`,
		date:     "1788307200000",
		text:     asJSON("x"),
	}.json()))

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--text", "x")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "PT45M", nodeAt(t, mapping, "duration").Value)
	assert.Equal(t, "Кодревью", nodeAt(t, mapping, "type", "name").Value)
	assert.Equal(t, "2026-09-02T00:00:00Z", nodeAt(t, mapping, "date").Value)
}

func TestTimeUpdateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusNotFound,
		`{"error":"Not Found","error_description":"Entity with id 199-6 not found"}`))

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--text", "x")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", workItemUpdateRequest(server.url, "DEV-1", "199-6", sentWorkItemWriteFields)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id 199-6 not found"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
}

func TestTimeUpdateWritesIntoAWorkItemOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	item := workItemOn(t, dev, issue, "PT1H", "--type", "Разработка", "--text", "ytrack contract a")

	got := runWith(t, dev.env(), "time", "update", issue, item, "--duration", "PT2H")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "PT2H", nodeAt(t, mapping, "duration").Value)
	assert.Equal(t, "Разработка", nodeAt(t, mapping, "type", "name").Value)
	assert.Equal(t, "ytrack contract a", nodeAt(t, mapping, "text").Value)
	assert.Equal(t, "PT2H", nodeAt(t, mapping, "issue", "customFields", "Затраченное время").Value)

	emptied := runWith(t, dev.env(), "time", "update", issue, item, "--clear", "type", "--clear", "text")
	require.Equal(t, 0, emptied.code, "stderr: %s", emptied.stderr)
	afterwards := requireMapping(t, "stdout", emptied.stdout)
	assert.Nil(t, requireValue(t, nodeAt(t, afterwards, "type")))
	assert.Nil(t, requireValue(t, nodeAt(t, afterwards, "text")))

	moved := runWith(t, dev.env(), "time", "update", issue, item, "--date", "2026-09-05")
	require.Equal(t, 0, moved.code, "stderr: %s", moved.stderr)
	assert.Equal(t, "2026-09-05T00:00:00Z", nodeAt(t, requireMapping(t, "stdout", moved.stdout), "date").Value)
}

func TestTimeUpdateRefusesAWorkItemOfAnotherIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	other := contractWorkItemIssue(t, dev, "other issue")
	item := workItemOn(t, dev, issue, "PT1H", "--text", "ytrack contract a")
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "update", other, item, "--text", "ytrack contract via-B")

	assert.Equal(t, "not_found", requireFault(t, got).code)
	assert.Equal(t, []string{workItemPath(other, item)}, pathsSince(dev, before))

	kept := theWorkItemsOf(t, dev, issue)
	require.Len(t, kept, 1)
	assert.Equal(t, "ytrack contract a", kept[0]["text"])
}

func TestTimeUpdateIsRefusedTheDurationWrittenAsAnID(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	item := workItemOn(t, dev, issue, "PT1H")
	dev.replacing(asDurationID)
	defer dev.replacing(nil)

	got := runWith(t, dev.env(), "time", "update", issue, item, "--duration", "PT3H")

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, "Для единицы работы должна быть задана длительность",
		detailNamed(t, found, "upstream_message"))

	dev.replacing(nil)
	kept := theWorkItemsOf(t, dev, issue)
	require.Len(t, kept, 1)
	assert.Equal(t, "PT1H", kept[0]["duration"])
}

func TestTimeUpdateRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	item := workItemOn(t, dev, issue, "PT1H", "--text", "ytrack contract a")
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"time", "update", issue, item, "--text", "ytrack contract x")

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Entity with id "+issue+" not found", detailNamed(t, found, "upstream_message"))
	assert.Len(t, pathsSince(dev, before), 1)

	kept := theWorkItemsOf(t, dev, issue)
	require.Len(t, kept, 1)
	assert.Equal(t, "ytrack contract a", kept[0]["text"])
}
