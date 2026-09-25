package youtrack_test

import (
	"cmp"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	workItemsOfTheIssue = "/api/issues/DEV-1/timeTracking/workItems"
	workItemOfTheIssue  = workItemsOfTheIssue + "/7-1"
)

const (
	writtenDayMidnight = "1788220800000"
	nextDayMidnight    = "1788307200000"
)

const (
	firstWorkItemType  = `{"$type":"WorkItemType","id":"8-1","name":"First"}`
	secondWorkItemType = `{"$type":"WorkItemType","id":"8-2","name":"Second"}`
	lowerTwinType      = `{"$type":"WorkItemType","id":"8-4","name":"Twin"}`
	upperTwinType      = `{"$type":"WorkItemType","id":"8-3","name":"TWIN"}`
	modeAttribute      = `{"$type":"WorkItemProjectAttribute","id":"9-1","name":"Mode","values":[` +
		`{"$type":"WorkItemAttributeValue","id":"9-2","name":"Solo"},` +
		`{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}]}`
)

const (
	modeKeptEmpty = `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode","value":null}]`
	modeKeptSolo  = `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"9-2","name":"Solo"}}]`
	modeKeptPair = `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}}]`
)

func workItemSettings(types, attributes string) string {
	return `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
		`"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings",` +
		`"workItemTypes":[` + types + `],"attributes":[` + attributes + `]}}}}`
}

func workItemProjectSettings() string {
	return workItemSettings(firstWorkItemType+","+secondWorkItemType, modeAttribute)
}

type workItemAnswer struct {
	duration   string
	date       string
	text       string
	workType   string
	attributes string
}

func (a workItemAnswer) json() string {
	return `{"$type":"IssueWorkItem","id":"7-1","duration":` + cmp.Or(a.duration, workItemMinutes("90")) +
		`,"date":` + cmp.Or(a.date, "null") + `,"text":` + cmp.Or(a.text, "null") +
		`,"type":` + cmp.Or(a.workType, "null") + `,"attributes":` + cmp.Or(a.attributes, "[]") +
		`,"author":{"$type":"User","login":"author"},"issue":{"$type":"Issue","idReadable":"DEV-1"}}`
}

func workItemMinutes(minutes string) string {
	return `{"$type":"DurationValue","minutes":` + minutes + `}`
}

func writingWorkItem(t *testing.T, settings string, answer workItemAnswer) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fake.JSON(http.StatusOK, settings)(w, r)
			return
		}
		fake.JSON(http.StatusOK, answer.json())(w, r)
	})
}

func workItemProjectOf(project string) string {
	return `{"$type":"Issue","idReadable":"DEV-1","project":` + project + `}`
}

func workItemTimeTracking(settings string) string {
	return workItemProjectOf(`{"$type":"Project","shortName":"DEV","plugins":{"$type":"ProjectPlugins",` +
		`"timeTrackingSettings":` + settings + `}}`)
}

func workItemNearest(names ...string) render.Pair {
	listed := make([]*render.Node, 0, len(names))
	for _, name := range names {
		listed = append(listed, render.NewString(name))
	}
	return render.Pair{Key: "nearest", Value: render.NewList(listed...)}
}

func workItemListing(records ...*render.Node) *render.Node {
	return render.NewMap(
		render.Pair{Key: "total", Value: render.NewNumber("1")},
		render.Pair{Key: "returned", Value: render.NewNumber("1")},
		render.Pair{Key: "truncated", Value: render.NewBool(false)},
		render.Pair{Key: "workItems", Value: render.NewList(records...)})
}

func TestCreateWorkItemRefusesADurationItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		spent string
	}{
		{name: "the way a person writes one", spent: "1h 30m"},
		{name: "a bare number", spent: "90"},
		{name: "minutes with no period at all", spent: "90m"},
		{name: "a day", spent: "P1D"},
		{name: "a week", spent: "P1W"},
		{name: "a day and hours", spent: "P1DT2H"},
		{name: "a fraction of an hour", spent: "PT1.5H"},
		{name: "seconds", spent: "PT30S"},
		{name: "hours, minutes and seconds", spent: "PT1H30M15S"},
		{name: "minutes before hours", spent: "PT30M1H"},
		{name: "minutes with no mark after them", spent: "PT1H30"},
		{name: "a negative count", spent: "PT-1H"},
		{name: "in lower case", spent: "pt1h"},
		{name: "the marker alone", spent: "PT"},
		{name: "nothing at all", spent: ""},
		{name: "a space before it", spent: " PT1H"},
		{name: "a fullwidth digit", spent: "PT１H"},
		{name: "one minute past the largest the server keeps", spent: "PT2147483648M"},
		{name: "one hour past the largest", spent: "PT35791395H"},
		{name: "hours and minutes one minute past the largest", spent: "PT35791394H8M"},
		{name: "minutes of more than 19 digits that wrap to one", spent: "PT18446744073709551617M"},
		{name: "hours of more than 19 digits that wrap to one", spent: "PT18446744073709551617H"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.CreateWorkItem("DEV-1", tc.spent, nil, nil, nil, nil, nil)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCreateWorkItemRefusesADayItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		day  string
	}{
		{name: "the day first", day: "01.09.2026"},
		{name: "the month first", day: "09/01/2026"},
		{name: "a word for today", day: "today"},
		{name: "a month and a day of one digit", day: "2026-9-1"},
		{name: "a day the month has none of", day: "2026-02-30"},
		{name: "a time of day", day: "2026-09-01T15:30:00Z"},
		{name: "midnight of another time zone", day: "2026-09-01T00:00:00+03:00"},
		{name: "midnight UTC carried in an offset", day: "2026-08-31T21:00:00-03:00"},
		{name: "a moment with no offset", day: "2026-09-01T00:00:00"},
		{name: "nothing at all", day: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.CreateWorkItem("DEV-1", "PT1H", new(tc.day), nil, nil, nil, nil)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCreateWorkItemRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       *string
		workType   *string
		attributes []string
		expression *string
	}{
		{name: "a text that is no UTF-8", text: new("bad\xffbyte")},
		{name: "a type of no name", workType: new("")},
		{name: "an attribute with no =", attributes: []string{"Mode"}},
		{name: "an attribute with no name", attributes: []string{"=Pair"}},
		{name: "an attribute with no value", attributes: []string{"Mode="}},
		{name: "an attribute set twice in another letter case", attributes: []string{"Mode=Solo", "MODE=Pair"}},
		{name: "a name under the duration", expression: new("duration(minutes)")},
		{name: "a custom field of the issue named", expression: new("+issue(customFields(State))")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.CreateWorkItem("DEV-1", "PT1H", nil, tc.text, tc.workType, tc.attributes, tc.expression)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestUpdateWorkItemRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		spent      *string
		day        *string
		text       *string
		workType   *string
		attributes []string
		cleared    []string
		expression *string
	}{
		{name: "nothing to write at all"},
		{name: "the duration emptied", cleared: []string{"duration"}},
		{name: "the day emptied in another letter case", cleared: []string{"DATE"}},
		{name: "nothing named to empty", cleared: []string{""}},
		{name: "the type written and taken away", workType: new("First"), cleared: []string{"type"}},
		{name: "the text written and emptied", text: new("x"), cleared: []string{"TEXT"}},
		{name: "an attribute set and taken away", attributes: []string{"Mode=Solo"}, cleared: []string{"MODE"}},
		{name: "an attribute with no value", attributes: []string{"Mode"}},
		{name: "a duration that is no period", spent: new("1h")},
		{name: "a duration one minute past the largest", spent: new("PT2147483648M")},
		{name: "a duration of more than 19 digits", spent: new("PT18446744073709551617M")},
		{name: "a time of day", day: new("2026-09-01T15:00:00Z")},
		{name: "a type of no name", workType: new("")},
		{name: "a text that is no UTF-8", text: new("bad\xffbyte")},
		{name: "a name under the duration", text: new("x"), expression: new("+duration(minutes)")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.UpdateWorkItem("DEV-1", "7-1", tc.spent, tc.day, tc.text, tc.workType,
				tc.attributes, tc.cleared, tc.expression)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestListWorkItemsRefusesAnExpressionItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the minutes of the duration", expression: "duration(minutes)"},
		{name: "the form the server writes a duration in for a person", expression: "duration(presentation)"},
		{name: "the id of the duration", expression: "+duration(id)"},
		{name: "a name under the attributes", expression: "id,attributes(id)"},
		{name: "a part of a link slot of the issue", expression: "issue(links(direction))"},
		{name: "a part of the parent slot of the issue", expression: "issue(parent(id))"},
		{name: "a custom field of the issue named", expression: "issue(customFields(State))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ListWorkItems("DEV-1", new(tc.expression), youtrack.Page{Limit: 50})

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCreateWorkItemWritesWhatTheCallGives(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		spent      string
		day        *string
		text       *string
		attributes []string
		answer     workItemAnswer
		sent       string
	}{
		{name: "hours and minutes", spent: "PT1H30M", sent: `{"duration":{"minutes":90}}`},
		{name: "minutes alone", spent: "PT90M", sent: `{"duration":{"minutes":90}}`},
		{
			name:   "hours alone",
			spent:  "PT24H",
			answer: workItemAnswer{duration: workItemMinutes("1440")},
			sent:   `{"duration":{"minutes":1440}}`,
		},
		{
			name:   "no time at all",
			spent:  "PT0M",
			answer: workItemAnswer{duration: workItemMinutes("0")},
			sent:   `{"duration":{"minutes":0}}`,
		},
		{
			name:   "the largest the server keeps",
			spent:  "PT2147483647M",
			answer: workItemAnswer{duration: workItemMinutes("2147483647")},
			sent:   `{"duration":{"minutes":2147483647}}`,
		},
		{
			name:   "the largest written in hours and minutes",
			spent:  "PT35791394H7M",
			answer: workItemAnswer{duration: workItemMinutes("2147483647")},
			sent:   `{"duration":{"minutes":2147483647}}`,
		},
		{
			name:   "a calendar day, as its noon UTC",
			spent:  "PT1H30M",
			day:    new("2026-09-01"),
			answer: workItemAnswer{date: writtenDayMidnight},
			sent:   `{"duration":{"minutes":90},"date":1788264000000}`,
		},
		{
			name:   "midnight UTC as ytrack prints it",
			spent:  "PT1H30M",
			day:    new("2026-09-01T00:00:00.000Z"),
			answer: workItemAnswer{date: writtenDayMidnight},
			sent:   `{"duration":{"minutes":90},"date":1788264000000}`,
		},
		{
			name:   "midnight UTC with an offset of none",
			spent:  "PT1H30M",
			day:    new("2026-09-01T00:00:00+00:00"),
			answer: workItemAnswer{date: writtenDayMidnight},
			sent:   `{"duration":{"minutes":90},"date":1788264000000}`,
		},
		{
			name:   "a text byte for byte",
			spent:  "PT1H30M",
			text:   new(" a\r\nb\x00 "),
			answer: workItemAnswer{text: `" a\r\nb\u0000 "`},
			sent:   `{"duration":{"minutes":90},"text":" a\r\nb\u0000 "}`,
		},
		{
			name:   "an empty text",
			spent:  "PT1H30M",
			text:   new(""),
			answer: workItemAnswer{text: `""`},
			sent:   `{"duration":{"minutes":90},"text":""}`,
		},
		{
			name:       "an attribute by the ids of the project, named in another letter case",
			spent:      "PT1H30M",
			attributes: []string{"mode=pair"},
			answer:     workItemAnswer{attributes: modeKeptPair},
			sent:       `{"duration":{"minutes":90},"attributes":[{"id":"9-1","value":{"id":"9-3"}}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, workItemProjectSettings(), tc.answer)

			_, fault := callOn(t, server)(youtrack.CreateWorkItem("DEV-1", tc.spent, tc.day, tc.text, nil,
				tc.attributes, new("id")))

			require.Nil(t, fault)
			assert.Equal(t, tc.sent, server.Last(t).Body)
		})
	}
}

func TestUpdateWorkItemWritesTheNamedPartsAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		spent      *string
		day        *string
		text       *string
		workType   *string
		attributes []string
		cleared    []string
		answer     workItemAnswer
		sent       string
	}{
		{name: "the text alone", text: new("x"), answer: workItemAnswer{text: `"x"`}, sent: `{"text":"x"}`},
		{name: "the text written empty", text: new(""), answer: workItemAnswer{text: `""`}, sent: `{"text":""}`},
		{name: "the text emptied", cleared: []string{"text"}, sent: `{"text":null}`},
		{name: "the type taken away, named in another letter case", cleared: []string{"TYPE"}, sent: `{"type":null}`},
		{
			name:   "how long it is and the day it is written against",
			spent:  new("PT2H"),
			day:    new("2026-09-02"),
			answer: workItemAnswer{duration: workItemMinutes("120"), date: nextDayMidnight},
			sent:   `{"duration":{"minutes":120},"date":1788350400000}`,
		},
		{
			name:     "the type by the id of the project",
			workType: new("Second"),
			answer:   workItemAnswer{workType: secondWorkItemType},
			sent:     `{"type":{"id":"8-2"}}`,
		},
		{
			name:       "an attribute set",
			attributes: []string{"Mode=Solo"},
			answer:     workItemAnswer{attributes: modeKeptSolo},
			sent:       `{"attributes":[{"id":"9-1","value":{"id":"9-2"}}]}`,
		},
		{
			name:    "an attribute taken away, named in another letter case",
			cleared: []string{"MODE"},
			answer:  workItemAnswer{attributes: modeKeptEmpty},
			sent:    `{"attributes":[{"id":"9-1","value":null}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, workItemProjectSettings(), tc.answer)

			_, fault := callOn(t, server)(youtrack.UpdateWorkItem("DEV-1", "7-1", tc.spent, tc.day, tc.text,
				tc.workType, tc.attributes, tc.cleared, new("id")))

			require.Nil(t, fault)
			assert.Equal(t, tc.sent, server.Last(t).Body)
		})
	}
}

func TestWorkItemWriteAsksForWhatItChecksWhateverWasAskedToPrint(t *testing.T) {
	t.Parallel()
	const settingsFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name))))"
	const settingsWithAttributes = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name)," +
		"attributes(id,name,values(id,name)))))"
	const attributesAsked = "attributes(id,name,value(id,name))"
	tests := []struct {
		name    string
		write   func() (youtrack.Call, *diag.Fault)
		answer  workItemAnswer
		targets []string
	}{
		{
			name: "a creation that names no type and no attribute",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("dev-1", "PT1H30M", nil, nil, nil, nil, new("id"))
			},
			targets: []string{"/api/issues/dev-1/timeTracking/workItems?fields=id,duration(minutes),date,text,issue(idReadable)"},
		},
		{
			name: "a creation that names a type",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("dev-1", "PT1H30M", nil, nil, new("First"), nil, new("id"))
			},
			answer: workItemAnswer{workType: firstWorkItemType},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsFields,
				workItemsOfTheIssue + "?fields=id,duration(minutes),date,text,issue(idReadable),type(id,name)",
			},
		},
		{
			name: "a creation that names a type and an attribute",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("dev-1", "PT1H30M", nil, nil, new("First"), []string{"Mode=Solo"}, new("id"))
			},
			answer: workItemAnswer{workType: firstWorkItemType, attributes: modeKeptSolo},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsWithAttributes,
				workItemsOfTheIssue + "?fields=id,duration(minutes),date,text,issue(idReadable),type(id,name)," +
					attributesAsked,
			},
		},
		{
			name: "an update of the text",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("dev-1", "7-1", nil, nil, new("x"), nil, nil, nil, new("id"))
			},
			answer:  workItemAnswer{text: `"x"`},
			targets: []string{"/api/issues/dev-1/timeTracking/workItems/7-1?fields=id,text"},
		},
		{
			name: "an update of how long it is and the day",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("DEV-1", "7-1", new("PT1H30M"), new("2026-09-01"), nil, nil, nil, nil, new("id"))
			},
			answer:  workItemAnswer{date: writtenDayMidnight},
			targets: []string{workItemOfTheIssue + "?fields=id,duration(minutes),date"},
		},
		{
			name: "an update emptying the text",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("DEV-1", "7-1", nil, nil, nil, nil, nil, []string{"text"}, new("id"))
			},
			targets: []string{workItemOfTheIssue + "?fields=id,text"},
		},
		{
			name: "an update taking the type away",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("DEV-1", "7-1", nil, nil, nil, nil, nil, []string{"type"}, new("id"))
			},
			targets: []string{workItemOfTheIssue + "?fields=id,type(name)"},
		},
		{
			name: "an update of the type",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("dev-1", "7-1", nil, nil, nil, new("First"), nil, nil, new("id"))
			},
			answer: workItemAnswer{workType: firstWorkItemType},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsFields,
				workItemOfTheIssue + "?fields=id,type(id,name)",
			},
		},
		{
			name: "an update taking an attribute away",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("dev-1", "7-1", nil, nil, nil, nil, nil, []string{"Mode"}, new("id"))
			},
			answer: workItemAnswer{attributes: modeKeptEmpty},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsWithAttributes,
				workItemOfTheIssue + "?fields=id," + attributesAsked,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, workItemProjectSettings(), tc.answer)

			node, fault := callOn(t, server)(tc.write())

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(render.Pair{Key: "id", Value: render.NewString("7-1")}), node)
			assert.Equal(t, tc.targets, server.Targets())
		})
	}
}

func TestCreateWorkItemRefusesWhatTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		day        *string
		text       *string
		workType   *string
		attributes []string
		answer     workItemAnswer
		field      string
		expected   *render.Node
		actual     *render.Node
	}{
		{
			name:     "a duration of another length",
			answer:   workItemAnswer{duration: workItemMinutes("60")},
			field:    "duration",
			expected: render.NewString("PT1H30M"),
			actual:   render.NewString("PT1H"),
		},
		{
			name:     "no duration at all",
			answer:   workItemAnswer{duration: "null"},
			field:    "duration",
			expected: render.NewString("PT1H30M"),
			actual:   render.NewNull(),
		},
		{
			name:     "a day after the one written",
			day:      new("2026-09-01"),
			answer:   workItemAnswer{date: nextDayMidnight},
			field:    "date",
			expected: render.NewString("2026-09-01"),
			actual:   render.NewString("2026-09-02T00:00:00Z"),
		},
		{
			name:     "a day that is no number of milliseconds",
			day:      new("2026-09-01"),
			answer:   workItemAnswer{date: `"2026-09-01"`},
			field:    "date",
			expected: render.NewString("2026-09-01"),
			actual:   render.NewNull(),
		},
		{
			name:     "no day at all",
			day:      new("2026-09-01"),
			field:    "date",
			expected: render.NewString("2026-09-01"),
			actual:   render.NewNull(),
		},
		{
			name:     "a text the server rewrote",
			text:     new("a\rb"),
			answer:   workItemAnswer{text: `"a\nb"`},
			field:    "text",
			expected: render.NewString("a\rb"),
			actual:   render.NewString("a\nb"),
		},
		{
			name:     "a text in another letter case",
			text:     new("Upper"),
			answer:   workItemAnswer{text: `"upper"`},
			field:    "text",
			expected: render.NewString("Upper"),
			actual:   render.NewString("upper"),
		},
		{
			name:     "another type of the project",
			workType: new("first"),
			answer:   workItemAnswer{workType: secondWorkItemType},
			field:    "type",
			expected: render.NewString("first"),
			actual:   render.NewString("Second"),
		},
		{
			name:     "no type at all",
			workType: new("First"),
			field:    "type",
			expected: render.NewString("First"),
			actual:   render.NewNull(),
		},
		{
			name:       "an attribute kept empty",
			attributes: []string{"Mode=Pair"},
			answer:     workItemAnswer{attributes: modeKeptEmpty},
			field:      "Mode",
			expected:   render.NewString("Pair"),
			actual:     render.NewNull(),
		},
		{
			name:       "an attribute kept with another value",
			attributes: []string{"Mode=Pair"},
			answer:     workItemAnswer{attributes: modeKeptSolo},
			field:      "Mode",
			expected:   render.NewString("Pair"),
			actual:     render.NewString("Solo"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, workItemProjectSettings(), tc.answer)

			_, fault := callOn(t, server)(youtrack.CreateWorkItem("DEV-1", "PT1H30M", tc.day, tc.text, tc.workType,
				tc.attributes, new("id")))

			assert.Equal(t, diag.Fault{
				Code:       diag.UpstreamInvalid,
				AfterWrite: true,
				Details: []render.Pair{
					lastRequest(t, server),
					{Key: "issue", Value: render.NewString("DEV-1")},
					{Key: "id", Value: render.NewString("7-1")},
					{Key: "mismatch", Value: render.NewList(render.NewMap(
						render.Pair{Key: "field", Value: render.NewString(tc.field)},
						render.Pair{Key: "expected", Value: tc.expected},
						render.Pair{Key: "actual", Value: tc.actual}))},
				},
			}, refusal(t, fault))
		})
	}
}

func TestUpdateWorkItemRefusesWhatTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		spent      *string
		day        *string
		text       *string
		workType   *string
		attributes []string
		cleared    []string
		answer     workItemAnswer
		field      string
		expected   *render.Node
		actual     *render.Node
	}{
		{
			name:     "a duration of another length",
			spent:    new("PT2H"),
			field:    "duration",
			expected: render.NewString("PT2H"),
			actual:   render.NewString("PT1H30M"),
		},
		{
			name:     "no duration at all",
			spent:    new("PT2H"),
			answer:   workItemAnswer{duration: "null"},
			field:    "duration",
			expected: render.NewString("PT2H"),
			actual:   render.NewNull(),
		},
		{
			name:     "a day after the one written",
			day:      new("2026-09-01"),
			answer:   workItemAnswer{date: nextDayMidnight},
			field:    "date",
			expected: render.NewString("2026-09-01"),
			actual:   render.NewString("2026-09-02T00:00:00Z"),
		},
		{
			name:     "another text",
			text:     new("x"),
			answer:   workItemAnswer{text: `"y"`},
			field:    "text",
			expected: render.NewString("x"),
			actual:   render.NewString("y"),
		},
		{
			name:     "a text in another letter case",
			text:     new("Upper"),
			answer:   workItemAnswer{text: `"upper"`},
			field:    "text",
			expected: render.NewString("Upper"),
			actual:   render.NewString("upper"),
		},
		{
			name:     "a text left after it was emptied",
			cleared:  []string{"text"},
			answer:   workItemAnswer{text: `"x"`},
			field:    "text",
			expected: render.NewNull(),
			actual:   render.NewString("x"),
		},
		{
			name:     "a text emptied to an empty string",
			cleared:  []string{"text"},
			answer:   workItemAnswer{text: `""`},
			field:    "text",
			expected: render.NewNull(),
			actual:   render.NewString(""),
		},
		{
			name:     "a type left after it was taken away",
			cleared:  []string{"type"},
			answer:   workItemAnswer{workType: firstWorkItemType},
			field:    "type",
			expected: render.NewNull(),
			actual:   render.NewString("First"),
		},
		{
			name:     "another type of the project",
			workType: new("First"),
			answer:   workItemAnswer{workType: secondWorkItemType},
			field:    "type",
			expected: render.NewString("First"),
			actual:   render.NewString("Second"),
		},
		{
			name:     "an attribute left after it was taken away",
			cleared:  []string{"Mode"},
			answer:   workItemAnswer{attributes: modeKeptPair},
			field:    "Mode",
			expected: render.NewNull(),
			actual:   render.NewString("Pair"),
		},
		{
			name:       "an attribute kept with another value",
			attributes: []string{"Mode=Pair"},
			answer:     workItemAnswer{attributes: modeKeptSolo},
			field:      "Mode",
			expected:   render.NewString("Pair"),
			actual:     render.NewString("Solo"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, workItemProjectSettings(), tc.answer)

			_, fault := callOn(t, server)(youtrack.UpdateWorkItem("DEV-1", "7-1", tc.spent, tc.day, tc.text,
				tc.workType, tc.attributes, tc.cleared, new("id")))

			assert.Equal(t, diag.Fault{
				Code:       diag.UpstreamInvalid,
				AfterWrite: true,
				Details: []render.Pair{
					lastRequest(t, server),
					{Key: "issue", Value: render.NewString("DEV-1")},
					{Key: "id", Value: render.NewString("7-1")},
					{Key: "mismatch", Value: render.NewList(render.NewMap(
						render.Pair{Key: "field", Value: render.NewString(tc.field)},
						render.Pair{Key: "expected", Value: tc.expected},
						render.Pair{Key: "actual", Value: tc.actual}))},
				},
			}, refusal(t, fault))
		})
	}
}

func TestCreateWorkItemTellsAKeptTypeByItsIDFromOneOfTheSameName(t *testing.T) {
	t.Parallel()
	server := writingWorkItem(t, workItemSettings(lowerTwinType+","+upperTwinType, ""),
		workItemAnswer{workType: lowerTwinType})

	_, fault := callOn(t, server)(youtrack.CreateWorkItem("DEV-1", "PT1H30M", nil, nil, new("TWIN"), nil, new("id")))

	assert.Equal(t, diag.Fault{
		Code:       diag.UpstreamInvalid,
		AfterWrite: true,
		Details: []render.Pair{
			lastRequest(t, server),
			{Key: "issue", Value: render.NewString("DEV-1")},
			{Key: "id", Value: render.NewString("7-1")},
			{Key: "mismatch", Value: render.NewList(render.NewMap(
				render.Pair{Key: "field", Value: render.NewString("type")},
				render.Pair{Key: "expected", Value: render.NewString("TWIN")},
				render.Pair{Key: "actual", Value: render.NewString("Twin")}))},
		},
	}, refusal(t, fault))
}

func TestWorkItemWriteChecksOnlyWhatItWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		write   func() (youtrack.Call, *diag.Fault)
		answer  workItemAnswer
		printed *render.Node
	}{
		{
			name: "a day kept at midnight UTC of the day written",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("DEV-1", "PT1H30M", new("2026-09-01"), nil, nil, nil, new("date"))
			},
			answer:  workItemAnswer{date: writtenDayMidnight},
			printed: render.NewMap(render.Pair{Key: "date", Value: render.NewString("2026-09-01T00:00:00Z")}),
		},
		{
			name: "a creation that wrote no day and no text",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("DEV-1", "PT1H30M", nil, nil, nil, nil, new("date,text"))
			},
			answer: workItemAnswer{date: nextDayMidnight, text: `"first\nsecond"`},
			printed: render.NewMap(
				render.Pair{Key: "date", Value: render.NewString("2026-09-02T00:00:00Z")},
				render.Pair{Key: "text", Value: render.NewText("first\nsecond")}),
		},
		{
			name: "an update that wrote the text alone",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("DEV-1", "7-1", nil, nil, new("x"), nil, nil, nil,
					new("duration,type(name),date"))
			},
			answer: workItemAnswer{duration: workItemMinutes("45"), workType: secondWorkItemType, date: nextDayMidnight,
				text: `"x"`},
			printed: render.NewMap(
				render.Pair{Key: "duration", Value: render.NewString("PT45M")},
				render.Pair{Key: "type", Value: render.NewMap(render.Pair{Key: "name", Value: render.NewString("Second")})},
				render.Pair{Key: "date", Value: render.NewString("2026-09-02T00:00:00Z")}),
		},
		{
			name: "an attribute taken away that the answer does not carry",
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("DEV-1", "7-1", nil, nil, nil, nil, nil, []string{"Mode"}, new("id"))
			},
			printed: render.NewMap(render.Pair{Key: "id", Value: render.NewString("7-1")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, workItemProjectSettings(), tc.answer)

			node, fault := callOn(t, server)(tc.write())

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestCreateWorkItemResolvesATypeOfTheProjectByName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		types string
		named string
		kept  string
		sent  string
	}{
		{
			name:  "in lower case",
			types: firstWorkItemType + "," + secondWorkItemType,
			named: "second",
			kept:  secondWorkItemType,
			sent:  `{"duration":{"minutes":90},"type":{"id":"8-2"}}`,
		},
		{
			name:  "in upper case",
			types: firstWorkItemType + "," + secondWorkItemType,
			named: "FIRST",
			kept:  firstWorkItemType,
			sent:  `{"duration":{"minutes":90},"type":{"id":"8-1"}}`,
		},
		{
			name:  "the spelling of one of two types that differ in letter case alone",
			types: lowerTwinType + "," + upperTwinType,
			named: "TWIN",
			kept:  upperTwinType,
			sent:  `{"duration":{"minutes":90},"type":{"id":"8-3"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, workItemSettings(tc.types, ""), workItemAnswer{workType: tc.kept})

			_, fault := callOn(t, server)(youtrack.CreateWorkItem("DEV-1", "PT1H30M", nil, nil, new(tc.named), nil,
				new("id")))

			require.Nil(t, fault)
			assert.Equal(t, tc.sent, server.Last(t).Body)
		})
	}
}

func TestWorkItemWriteRefusesANameTheProjectDoesNotHave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		settings string
		write    func() (youtrack.Call, *diag.Fault)
		unknown  []*render.Node
	}{
		{
			name:     "a type a letter short",
			settings: workItemProjectSettings(),
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("DEV-1", "PT1H", nil, nil, new("Secnd"), nil, nil)
			},
			unknown: []*render.Node{render.NewMap(render.Pair{Key: "type", Value: render.NewString("Secnd")},
				workItemNearest("Second"))},
		},
		{
			name:     "a type near none of the project",
			settings: workItemProjectSettings(),
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("DEV-1", "PT1H", nil, nil, new("zzzzzzzz"), nil, nil)
			},
			unknown: []*render.Node{render.NewMap(render.Pair{Key: "type", Value: render.NewString("zzzzzzzz")},
				workItemNearest("First", "Second"))},
		},
		{
			name:     "a type two types answer to in another letter case",
			settings: workItemSettings(lowerTwinType+","+upperTwinType, ""),
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("DEV-1", "7-1", nil, nil, nil, new("twin"), nil, nil, nil)
			},
			unknown: []*render.Node{render.NewMap(render.Pair{Key: "type", Value: render.NewString("twin")},
				workItemNearest("TWIN", "Twin"))},
		},
		{
			name:     "an attribute the project has none of",
			settings: workItemProjectSettings(),
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("DEV-1", "PT1H", nil, nil, nil, []string{"Mood=Pair"}, nil)
			},
			unknown: []*render.Node{render.NewMap(render.Pair{Key: "attribute", Value: render.NewString("Mood")},
				workItemNearest("Mode"))},
		},
		{
			name:     "a value the attribute does not take",
			settings: workItemProjectSettings(),
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateWorkItem("DEV-1", "PT1H", nil, nil, nil, []string{"Mode=Trio"}, nil)
			},
			unknown: []*render.Node{render.NewMap(
				render.Pair{Key: "attribute", Value: render.NewString("Mode")},
				render.Pair{Key: "value", Value: render.NewString("Trio")},
				workItemNearest("Pair", "Solo"))},
		},
		{
			name:     "a value set and an attribute taken away, both unknown",
			settings: workItemProjectSettings(),
			write: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateWorkItem("DEV-1", "7-1", nil, nil, nil, nil, []string{"Mode=Sol"}, []string{"Mood"}, nil)
			},
			unknown: []*render.Node{
				render.NewMap(
					render.Pair{Key: "attribute", Value: render.NewString("Mode")},
					render.Pair{Key: "value", Value: render.NewString("Sol")},
					workItemNearest("Solo")),
				render.NewMap(render.Pair{Key: "attribute", Value: render.NewString("Mood")}, workItemNearest("Mode")),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, tc.settings, workItemAnswer{})

			_, fault := callOn(t, server)(tc.write())

			assert.Equal(t, diag.Fault{
				Code: diag.UnknownName,
				Details: []render.Pair{
					lastRequest(t, server),
					{Key: "project", Value: render.NewString("DEV")},
					{Key: "unknown", Value: render.NewList(tc.unknown...)},
				},
			}, refusal(t, fault))
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
		})
	}
}

func TestWorkItemWriteRefusesSettingsOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		read       string
		attributes []string
	}{
		{name: "an issue under the readable id of an article", read: `{"$type":"Issue","idReadable":"DEV-A-1",` +
			`"project":{"$type":"Project","shortName":"DEV","plugins":null}}`},
		{name: "a project that is no object", read: workItemProjectOf("null")},
		{
			name: "a project with no text for a short name",
			read: workItemProjectOf(`{"$type":"Project","shortName":null,"plugins":null}`),
		},
		{name: "no plugins", read: workItemProjectOf(`{"$type":"Project","shortName":"DEV","plugins":null}`)},
		{name: "no settings of time tracking", read: workItemTimeTracking("null")},
		{name: "no types of work", read: workItemTimeTracking(`{"workItemTypes":null,"attributes":[]}`)},
		{name: "a type that is null", read: workItemTimeTracking(`{"workItemTypes":[null],"attributes":[]}`)},
		{
			name: "a type with no text for an id",
			read: workItemTimeTracking(`{"workItemTypes":[{"id":8,"name":"First"}],"attributes":[]}`),
		},
		{name: "a type with no name", read: workItemTimeTracking(`{"workItemTypes":[{"id":"8-1","name":null}],"attributes":[]}`)},
		{
			name:       "no attributes",
			read:       workItemTimeTracking(`{"workItemTypes":[],"attributes":null}`),
			attributes: []string{"Mode=Solo"},
		},
		{
			name:       "an attribute with no id",
			read:       workItemTimeTracking(`{"workItemTypes":[],"attributes":[{"id":null,"name":"Mode","values":[]}]}`),
			attributes: []string{"Mode=Solo"},
		},
		{
			name:       "an attribute with no values",
			read:       workItemTimeTracking(`{"workItemTypes":[],"attributes":[{"id":"9-1","name":"Mode","values":null}]}`),
			attributes: []string{"Mode=Solo"},
		},
		{
			name: "a value of an attribute with no name",
			read: workItemTimeTracking(`{"workItemTypes":[],"attributes":[{"id":"9-1","name":"Mode",` +
				`"values":[{"id":"9-2","name":null}]}]}`),
			attributes: []string{"Mode=Solo"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingWorkItem(t, tc.read, workItemAnswer{})

			_, fault := callOn(t, server)(youtrack.CreateWorkItem("DEV-1", "PT1H", nil, nil, new("First"),
				tc.attributes, nil))

			assert.Equal(t, unreadable(lastRequest(t, server), tc.read),
				refusal(t, fault))
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
		})
	}
}

func TestDeleteWorkItemRemovesTheWorkItemUnderTheIssueTheReadNamed(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("GET /api/issues/dev-1/timeTracking/workItems/7-1",
		fake.JSON(http.StatusOK, `{"$type":"IssueWorkItem","id":"7-1","issue":{"$type":"Issue","idReadable":"DEV-1"}}`))
	mux.HandleFunc("DELETE "+workItemOfTheIssue, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	server := fake.Serve(t, mux.ServeHTTP)

	node, fault := callOn(t, server)(youtrack.DeleteWorkItem("dev-1", "7-1"))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "id", Value: render.NewString("7-1")},
		render.Pair{Key: "issue", Value: render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")})},
	), node)
	assert.Equal(t, []string{"/api/issues/dev-1/timeTracking/workItems/7-1", workItemOfTheIssue}, server.Paths())
	assert.Equal(t, http.MethodDelete, server.Last(t).Method)
}

func TestDeleteWorkItemRemovesNothingByAReadOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read string
	}{
		{name: "an id that is no internal id", read: `{"$type":"IssueWorkItem","id":"..","issue":{"idReadable":"DEV-1"}}`},
		{name: "an id that is no text", read: `{"$type":"IssueWorkItem","id":7,"issue":{"idReadable":"DEV-1"}}`},
		{name: "no issue at all", read: `{"$type":"IssueWorkItem","id":"7-1","issue":null}`},
		{name: "the readable id of an article", read: `{"$type":"IssueWorkItem","id":"7-1","issue":{"idReadable":"DEV-A-1"}}`},
		{name: "a readable id that is a path", read: `{"$type":"IssueWorkItem","id":"7-1","issue":{"idReadable":".."}}`},
		{name: "a readable id that is no text", read: `{"$type":"IssueWorkItem","id":"7-1","issue":{"idReadable":1}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.read))

			_, fault := callOn(t, server)(youtrack.DeleteWorkItem("DEV-1", "7-1"))

			assert.Equal(t, unreadable(lastRequest(t, server), tc.read),
				refusal(t, fault))
			assert.Equal(t, []string{workItemOfTheIssue}, server.Paths())
		})
	}
}

func TestListWorkItemsPrintsAWorkItem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		received   string
		printed    *render.Node
	}{
		{
			name:       "a duration of hours and minutes",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("90") + `}`,
			printed:    render.NewString("PT1H30M"),
		},
		{
			name:       "a duration of a whole hour",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("60") + `}`,
			printed:    render.NewString("PT1H"),
		},
		{
			name:       "a duration of a minute",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("1") + `}`,
			printed:    render.NewString("PT1M"),
		},
		{
			name:       "a duration of a day of the clock",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("1440") + `}`,
			printed:    render.NewString("PT24H"),
		},
		{
			name:       "the largest duration the server keeps",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("2147483647") + `}`,
			printed:    render.NewString("PT35791394H7M"),
		},
		{
			name:       "a duration of no time",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("0") + `}`,
			printed:    render.NewString("PT0M"),
		},
		{name: "no duration", expression: "duration", received: `{"duration":null}`, printed: render.NewNull()},
		{
			name:       "the day, at the midnight UTC it is kept at",
			expression: "date",
			received:   `{"date":` + writtenDayMidnight + `}`,
			printed:    render.NewString("2026-09-01T00:00:00Z"),
		},
		{
			name:       "a text of two lines, on the line of its record",
			expression: "text",
			received:   `{"text":"first\nsecond"}`,
			printed:    render.NewString("first\nsecond"),
		},
		{
			name:       "the attributes, as the value each holds under its name",
			expression: "attributes",
			received: `{"attributes":[{"id":"9-1","name":"Mode","value":{"id":"9-3","name":"Pair"}},` +
				`{"id":"9-4","name":"Kind","value":null}]}`,
			printed: render.NewMap(render.FromData("Mode", render.NewString("Pair")), render.FromData("Kind", render.NewNull())),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, "["+tc.received+"]"))

			node, fault := callOn(t, server)(youtrack.ListWorkItems("DEV-1", new(tc.expression), youtrack.Page{Limit: 50}))

			require.Nil(t, fault)
			assert.Equal(t, workItemListing(render.NewMap(render.Pair{Key: tc.expression, Value: tc.printed})), node)
		})
	}
}

func TestListWorkItemsRefusesAWorkItemOfAnotherShape(t *testing.T) {
	t.Parallel()
	const asked = "?fields=duration(minutes),attributes(id,name,value(id,name))&$top=50"
	tests := []struct {
		name     string
		received string
	}{
		{name: "a duration of no minutes", received: `{"duration":{"minutes":null},"attributes":[]}`},
		{name: "a fraction of a minute", received: `{"duration":{"minutes":1.5},"attributes":[]}`},
		{name: "the minutes as text", received: `{"duration":{"minutes":"90"},"attributes":[]}`},
		{name: "attributes that are null", received: `{"duration":null,"attributes":null}`},
		{name: "an attribute with no name", received: `{"duration":null,"attributes":[{"id":"9-1","name":null,"value":null}]}`},
		{
			name: "two attributes of one name",
			received: `{"duration":null,"attributes":[{"id":"9-1","name":"Mode","value":null},` +
				`{"id":"9-4","name":"Mode","value":null}]}`,
		},
		{
			name:     "a value of an attribute with no name",
			received: `{"duration":null,"attributes":[{"id":"9-1","name":"Mode","value":{"id":"9-3","name":null}}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := "[" + tc.received + "]"
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, fault := callOn(t, server)(youtrack.ListWorkItems("DEV-1", new("duration,attributes"),
				youtrack.Page{Limit: 50}))

			assert.Equal(t, diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				{Key: "request", Value: render.NewString("GET " + server.URL + workItemsOfTheIssue + asked)},
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(body)},
			}}, refusal(t, fault))
		})
	}
}

func TestListWorkItemsAsksTheIssuesOfALinkSlotOfTheIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "[]"))

	_, fault := callOn(t, server)(youtrack.ListWorkItems("DEV-1", new("issue(links(issues(idReadable)))"),
		youtrack.Page{Limit: 50}))

	require.Nil(t, fault)
	assert.Equal(t, []string{"issue(links(issues(idReadable),direction,linkType(sourceToTarget,targetToSource)))"},
		server.Fields())
}
