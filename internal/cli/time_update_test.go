package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The path one work item of an issue stands under, which is where an update and a removal are addressed.
func workItemPath(issue, id string) string {
	return workItemsPath(issue) + "/" + id
}

// The request an update of a work item goes out as, which is the one a refusal about it names.
func workItemUpdateRequest(address, issue, id, fields string) string {
	return "POST " + address + workItemPath(issue, id) + "?fields=" + fields
}

// workItemOn writes one work item on the issue and hands back the id it goes by, which is what an update and a
// removal address it with.
func workItemOn(t *testing.T, dev *upstream, issue string, argv ...string) string {
	t.Helper()
	got := runWith(t, dev.env(), append([]string{"time", "create", issue}, argv...)...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	id := nodeAt(t, requireMapping(t, "stdout", got.stdout), "id").Value
	require.Regexp(t, internalIDForm, id)
	return id
}

// theWorkItemsOf is every work item of the issue as time list prints them, which is what a scenario holds a
// write to without asking the server anything the command itself does not.
func theWorkItemsOf(t *testing.T, dev *upstream, issue string) []map[string]any {
	t.Helper()
	return requireWorkItemListing(t, runWith(t, dev.env(), "time", "list", issue)).WorkItems
}

// What an update writes is the parts it names, so a call that names none is refused before the network,
// and so is every part written two ways at once or written as something the server would not keep.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The duration, the day and the name of the type are read the way a creation reads them, so a length written
// as the server shows one, a moment of a day and a name of no type are refused here as well, before anything is
// sent.
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

			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

// The id of the work item is held to the form of an internal id before anything is sent, and a readable id
// of either kind is no address for one: the generated client resolves the segment against the server, so an
// empty one would turn the update into a creation and ".." into a write to the issue itself.
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

			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

// A leading zero is the server's to refuse and it does, by matching the id exactly: the form passes here,
// the request goes out as it was written, and the answer is the server's word about an id it has none of.
func TestTimeUpdateSendsAnIDWithALeadingZeroAsItWasWritten(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusNotFound, entityNotFound("199-06")))

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-06", "--text", "x")

	assert.Equal(t, "not_found", requireRefusal(t, got).code)
	assert.Equal(t, []string{workItemPath("DEV-1", "199-06")}, server.sentPaths())
}

// The body carries the parts the call named and not one key more: a part it said nothing about keeps its
// key out altogether, which is what leaves the work item holding what it held, and a part under --clear goes
// out as an explicit null. Nothing of the work item is read first — the pair is the server's to check.
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
			// An empty string and no key at all are two different writes: YouTrack keeps an empty text as an
			// empty text, so only a null empties one.
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
			server := writingTime(t, answer(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), append([]string{"time", "update", "DEV-1", "199-6"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, tc.body, sentWorkItem(t, server))
			assert.Equal(t, []string{workItemPath("DEV-1", "199-6")}, server.sentPaths())
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

// A type named by a caller is resolved before the write, as it is in a creation, and the write that follows
// goes out to the readable id that read gave: two requests, and the id of the type is the whole of what the
// body says about it.
func TestTimeUpdateReadsTheTypesOfTheProjectBeforeTheWrite(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t,
		answer(http.StatusOK, devIssueWithWorkItemTypes()),
		answer(http.StatusOK, answeredWorkItem{
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

// A part the call emptied is a part the answer holds nothing in, and one still standing there is the server
// disagreeing with the write as much as a value that came back another. The work item holds it by then, which
// is what the exit code says without the document being read.
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
			mismatch: []any{[]detail{{"field", "type"}, {"written", nil}, {"arrived", "Разработка"}}},
		},
		{
			name:     "the text",
			emptied:  "text",
			answered: answeredWorkItem{text: asJSON("Разбор полигона")},
			mismatch: []any{[]detail{{"field", "text"}, {"written", nil}, {"arrived", "Разбор полигона"}}},
		},
		{
			// An empty string is not an empty text here: YouTrack keeps one, and --clear text sent the null
			// that empties the part outright.
			name:     "the text emptied to an empty string",
			emptied:  "text",
			answered: answeredWorkItem{text: asJSON("")},
			mismatch: []any{[]detail{{"field", "text"}, {"written", nil}, {"arrived", ""}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, answer(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--clear", tc.emptied)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, []string{"request", "issue", "id", "mismatch"}, detailKeys(found))
			assert.Equal(t, "DEV-1", detailNamed(t, found, "issue"))
			assert.Equal(t, "199-6", detailNamed(t, found, "id"))
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
		})
	}
}

// What the tool needs of the answer goes out whatever the caller asked to print: the parts the check holds
// against what was written, and those alone — the pair a refusal names the work item by is the caller's own
// here, so nothing is asked for it. The document stays theirs.
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
			server := writingTime(t, answer(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), append([]string{"time", "update", "DEV-1", "199-6"},
				append(tc.argv, "--fields", "id")...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, `id: "199-7"`+"\n", got.stdout)
			assert.Equal(t, []string{tc.asked}, server.sentFields())
		})
	}
}

// What the call named nothing for is held to nothing: the work item holds what it held, a workflow may have
// moved it, and either way the answer is the only word there is on it. It is printed all the same.
func TestTimeUpdateHoldsNothingItNeverWrote(t *testing.T) {
	t.Parallel()
	server := writingTime(t, answer(http.StatusOK, answeredWorkItem{
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

// What the server says about a write it refused passes on word for word: a work item of another issue and
// one the instance has none of are the same 404, and nothing was written either way.
func TestTimeUpdateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	server := writingTime(t, answer(http.StatusNotFound,
		`{"error":"Not Found","error_description":"Entity with id 199-6 not found"}`))

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--text", "x")

	want := refusal{
		code: "not_found",
		details: []detail{
			{"request", workItemUpdateRequest(server.url, "DEV-1", "199-6", sentWorkItemWriteFields)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id 199-6 not found"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
}

// A work item of the polygon changed for real: each call writes the part it names and leaves the rest as
// the work item holds it, the parts under --clear come back empty, and the time spent on the issue moves with
// the duration, in the answer to the write itself.
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

// The pair is the server's to check, and it does: a work item addressed through an issue it does not hang
// from is answered as if it were not there, in the one request the command sends, and the work item is left
// exactly as it was.
func TestTimeUpdateRefusesAWorkItemOfAnotherIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	other := contractWorkItemIssue(t, dev, "other issue")
	item := workItemOn(t, dev, issue, "PT1H", "--text", "ytrack contract a")
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "update", other, item, "--text", "ytrack contract via-B")

	assert.Equal(t, "not_found", requireRefusal(t, got).code)
	assert.Equal(t, []string{workItemPath(other, item)}, pathsSince(dev, before))

	kept := theWorkItemsOf(t, dev, issue)
	require.Len(t, kept, 1)
	assert.Equal(t, "ytrack contract a", kept[0]["text"])
}

// ytrack sends the minutes and has no way to send the ISO period as the id of a duration, so that body is put
// on the wire between ytrack and the polygon: the server refuses it word for word here as it does on a
// creation, and the work item keeps the length it had.
func TestTimeUpdateIsRefusedTheDurationWrittenAsAnID(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	item := workItemOn(t, dev, issue, "PT1H")
	dev.replacing(asDurationID)
	defer dev.replacing(nil)

	got := runWith(t, dev.env(), "time", "update", issue, item, "--duration", "PT3H")

	found := requireRefusal(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, "Для единицы работы должна быть задана длительность",
		detailNamed(t, found, "upstream_message"))

	dev.replacing(nil)
	kept := theWorkItemsOf(t, dev, issue)
	require.Len(t, kept, 1)
	assert.Equal(t, "PT1H", kept[0]["duration"])
}

// A token that may not see the issue is answered as if the issue were not there, in the one request the
// command sends, and the admin finds the work item exactly as it was.
func TestTimeUpdateRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	item := workItemOn(t, dev, issue, "PT1H", "--text", "ytrack contract a")
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"time", "update", issue, item, "--text", "ytrack contract x")

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Entity with id "+issue+" not found", detailNamed(t, found, "upstream_message"))
	assert.Len(t, pathsSince(dev, before), 1)

	kept := theWorkItemsOf(t, dev, issue)
	require.Len(t, kept, 1)
	assert.Equal(t, "ytrack contract a", kept[0]["text"])
}
