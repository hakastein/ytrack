package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// What a record of the list carries unasked, which is what its help names and what goes out where the caller
// writes no expression of their own.
const workItemListFields = "id,duration,type(name),attributes,author(login),date,text"

// What goes out for the same expression: the minutes are filled in under the duration, which arrives with its
// type and nothing else where nobody asks for them.
const sentWorkItemFields = "id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date,text"

// The path the work items of one issue stand under, which is the only place ytrack asks about them.
func workItemsPath(id string) string {
	return "/api/issues/" + id + "/timeTracking/workItems"
}

// A work item carries no readable id of its own, so the id of a record is held to the form of an internal one
// rather than to the digits one instance happens to have numbered it with.
const internalIDForm = `^[0-9]+-[0-9]+$`

// The one attribute DEV gives its work items, as a work item arrives with it unset and set: every work item
// carries every attribute of its project.
const (
	sentNoAttribute    = `"attributes":[{"$type":"WorkItemAttribute","id":"309-0","name":"Формат работы","value":null}]`
	sentAgentAttribute = `"attributes":[{"name":"Формат работы","$type":"WorkItemAttribute","id":"309-0",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"506-1","name":"ИИагент"}}]`
)

// Records under the default expression, with $type and the keys in an order other than the one asked for: the
// server keeps an order of its own.
const (
	listedWorkItem = `{"author":{"login":"admin","$type":"User"},"text":"Разбор полигона","date":1788220800000,` +
		`"duration":{"minutes":90,"$type":"DurationValue"},` + sentNoAttribute + `,` +
		`"type":{"name":"Разработка","$type":"WorkItemType"},"id":"199-6","$type":"IssueWorkItem"}`
	listedSecondWorkItem = `{"text":"Второй заход","$type":"IssueWorkItem","id":"199-7","date":1788307200000,` +
		`"duration":{"$type":"DurationValue","minutes":30},` + sentAgentAttribute + `,` +
		`"type":{"name":"Кодревью","$type":"WorkItemType"},"author":{"login":"dev.member","$type":"User"}}`
)

const (
	printedWorkItemRow = `  - {id: "199-6", duration: "PT1H30M", type: {name: "Разработка"}, attributes: {"Формат работы": null}, ` +
		`author: {login: "admin"}, date: "2026-09-01T00:00:00Z", text: "Разбор полигона"}` + "\n"
	printedSecondRow = `  - {id: "199-7", duration: "PT30M", type: {name: "Кодревью"}, attributes: {"Формат работы": "ИИагент"}, ` +
		`author: {login: "dev.member"}, date: "2026-09-02T00:00:00Z", text: "Второй заход"}` + "\n"
)

// listingWorkItems is what time list sends for a page of limit work items that it goes on to count: the second
// pass asks for ids alone and for every one of them.
func listingWorkItems(limit string) []url.Values {
	return []url.Values{
		{"fields": {sentWorkItemFields}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
}

// countedWorkItems answers a request for work items with records and the request that counts them, $top=-1,
// with count.
func countedWorkItems(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			count(w, r)
			return
		}
		answer(http.StatusOK, records)(w, r)
	}
}

// workItemListing is the document time list prints, read back.
type workItemListing struct {
	Total     int              `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated bool             `yaml:"truncated"`
	WorkItems []map[string]any `yaml:"workItems"`
}

// requireWorkItemListing reads back what time list printed and holds the counts to the records, which is as far
// as a test of the polygon can hold them.
func requireWorkItemListing(t *testing.T, got outcome) workItemListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed workItemListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.WorkItems, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

// The issue is one argument and the limit one number: everything else a call may be written as is refused
// before the network.
func TestTimeListRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the group with no verb", argv: []string{"time"}},
		{name: "a verb the group has none of", argv: []string{"time", "bogus"}},
		{name: "no issue at all", argv: []string{"time", "list"}},
		{name: "two issues", argv: []string{"time", "list", "DEV-1", "DEV-2"}},
		{name: "a limit of zero", argv: []string{"time", "list", "DEV-1", "--limit", "0"}},
		{name: "the limit twice", argv: []string{"time", "list", "DEV-1", "--limit", "1", "--limit", "2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The help names what goes out unasked and how many records are printed unasked, so a caller reads both
// defaults off the command rather than off the answer.
func TestTimeListHelpNamesItsDefaults(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"time", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, workItemListFields)
}

// The group offers the verbs it was built with and no other.
func TestTimeGroupNamesItsVerbs(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"time", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"create", "delete", "list", "update"}, availableCommands(t, got.stdout))
}

func TestTimeGroupIsOfferedByTheWordsOfEveryOtherGroup(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	answered := requireCompleted(t, runWith(t, server.env(), "__complete", "tim"))

	require.Len(t, answered.suggestions, 1)
	name, text, found := strings.Cut(answered.suggestions[0], "\t")
	assert.Equal(t, "time", name)
	assert.True(t, found, "the group came back with nothing beside it")
	assert.NotEmpty(t, text)
}

// One request carries the limit as $top and the default as fields=, and the records are printed in the
// order they arrived, each on the line of its own.
func TestTimeListAsksTheWorkItemsOfTheIssueInOneRequest(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

	got := runWith(t, server.env(), "time", "list", "DEV-1")

	want := "total: 2\nreturned: 2\ntruncated: false\nworkItems:\n" + printedWorkItemRow + printedSecondRow
	assert.Equal(t, outcome{stdout: want}, got)
	requests := server.requests()
	require.Len(t, requests, 1)
	assert.Equal(t, http.MethodGet, requests[0].Method)
	assert.Equal(t, workItemsPath("DEV-1"), requests[0].URL.Path)
	assert.Equal(t, url.Values{"fields": {sentWorkItemFields}, "$top": {"50"}}, requests[0].URL.Query())
	for _, record := range workItemRecords(t, got) {
		assert.Equal(t, []string{"id", "duration", "type", "attributes", "author", "date", "text"}, recordKeys(record))
		assert.Equal(t, yaml.FlowStyle, record.Style, "the record stands on more than one line")
	}
}

// workItemRecords is the records of a printed selection as they were printed, so a scenario can hold the keys
// to their order and a record to the one line it stands on.
func workItemRecords(t *testing.T, got outcome) []*yaml.Node {
	t.Helper()
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "workItems").Content
}

// A record stands on one line, so its text is a double-quoted string there: every byte of it is kept, the
// line endings and the separators a literal block could not carry among them.
func TestTimeListPrintsTheTextOfAWorkItemOnOneLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrived string
		want    string
	}{
		{name: "a line feed", arrived: `"a\nb"`, want: `"a\nb"`},
		{name: "a carriage return", arrived: `"a\rb"`, want: `"a\rb"`},
		{name: "a line separator", arrived: "\"a\xe2\x80\xa8b\"", want: `"a\Lb"`},
		{name: "nothing at all", arrived: `""`, want: `""`},
		{name: "no text", arrived: `null`, want: `null`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `[{"$type":"IssueWorkItem","id":"199-6","author":null,"text":` + tc.arrived + `}]`
			server := serve(t, answer(http.StatusOK, body))

			got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,author(login),text")

			want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
				`  - {id: "199-6", author: null, text: ` + tc.want + "}\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

// The separators a literal block cannot carry reach the document as the bytes they arrived as, whatever
// YAML writes them as: the text is read back rather than looked at.
func TestTimeListKeepsEveryByteOfTheTextOfAWorkItem(t *testing.T) {
	t.Parallel()
	const written = "первая\nвторая\rтретья\xe2\x80\xa8четвёртая"
	body := "[{\"$type\":\"IssueWorkItem\",\"id\":\"199-6\",\"text\":\"первая\\nвторая\\rтретья\xe2\x80\xa8четвёртая\"}]"
	server := serve(t, answer(http.StatusOK, body))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,text")

	printed := requireWorkItemListing(t, got)
	require.Len(t, printed.WorkItems, 1)
	assert.Equal(t, written, printed.WorkItems[0]["text"])
	assert.Equal(t, 1, strings.Count(strings.TrimSuffix(got.stdout, "\n"), "\n  - "))
}

// A page that fills the limit proves nothing about the whole, so it is counted by a second pass over ids
// alone, and that pass asks for every work item the issue holds.
func TestTimeListCountsTheWorkItemsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	const found = `[{"id":"199-6","$type":"IssueWorkItem"},{"id":"199-7","$type":"IssueWorkItem"},` +
		`{"id":"199-8","$type":"IssueWorkItem"}]`
	server := serve(t, countedWorkItems("["+listedWorkItem+","+listedSecondWorkItem+"]", answer(http.StatusOK, found)))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--limit", "2")

	want := "total: 3\nreturned: 2\ntruncated: true\nworkItems:\n" + printedWorkItemRow + printedSecondRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, listingWorkItems("2"), server.sentQueries())
	assert.Equal(t, []string{workItemsPath("DEV-1"), workItemsPath("DEV-1")}, server.sentPaths())
}

// A page shorter than the limit is the whole of what the issue holds, and nothing is asked twice.
func TestTimeListCountsNothingWhenThePageIsShortOfTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, "["+listedWorkItem+"]"))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--limit", "2")

	want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" + printedWorkItemRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, server.requests(), 1)
}

// A count that fails, or that comes back below what already arrived, takes the command with it: half a
// document would say the rest were not cut off.
func TestTimeListRefusesWhenTheCountDoesNotHold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		count http.HandlerFunc
		want  refusal
	}{
		{
			name:  "a count of none over a page of one",
			count: answer(http.StatusOK, "[]"),
			want: refusal{
				code:    "upstream_failed",
				details: []detail{{"total", 0}, {"returned", 1}},
			},
		},
		{
			name:  "a server that failed the count",
			count: answer(http.StatusInternalServerError, `{"error":"server_error","error_description":"java.lang.NullPointerException"}`),
			want: refusal{
				code: "upstream_failed",
				details: []detail{
					{"request", ""},
					{"upstream_status", 500},
					{"upstream_error", "server_error"},
					{"upstream_message", "java.lang.NullPointerException"},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, countedWorkItems("["+listedWorkItem+"]", tc.count))

			got := runWith(t, server.env(), "time", "list", "DEV-1", "--limit", "1")

			want := tc.want
			for i, printed := range want.details {
				if printed.key == "request" {
					want.details[i].value = "GET " + server.url + workItemsPath("DEV-1") + "?fields=id&$top=-1"
				}
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Len(t, server.requests(), 2)
		})
	}
}

// A page longer than the limit means $top went out wrong or the server ignored it, and the count that
// would follow it would be of something else, so the refusal comes before it.
func TestTimeListRefusesMoreWorkItemsThanTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--limit", "1")

	want := refusal{
		code:    "upstream_lied",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

// An issue that holds no work item prints the same keys as one that does, with an empty list under the
// plural; a body that is no array of objects at all is a refusal rather than an empty list.
func TestTimeListPrintsTheSameDocumentHoweverManyArrived(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		body        string
		want        string
		refused     bool
	}{
		{
			name:        "none at all",
			contentType: "application/json",
			body:        "[]",
			want:        "total: 0\nreturned: 0\ntruncated: false\nworkItems: []\n",
		},
		{
			name:        "no body under a 200",
			contentType: "application/json",
			refused:     true,
		},
		{
			name:        "the web page under a 200",
			contentType: "text/html",
			body:        "<!doctype html>\n<html><body>Log in</body></html>",
			refused:     true,
		},
		{
			name:        "one object where an array was promised",
			contentType: "application/json",
			body:        listedWorkItem,
			refused:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			})

			got := runWith(t, server.env(), "time", "list", "DEV-1")

			if tc.refused {
				found := requireRefusal(t, got)
				assert.Equal(t, "upstream_lied", found.code)
				return
			}
			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

// The one work item of DEV-1 on the polygon, read in one request: the id is held to the form of an internal
// id, since the numbers of an instance are its own.
func TestTimeListPrintsTheWorkItemOfDEV1OfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "time", "list", "DEV-1")

	printed := requireWorkItemListing(t, got)
	assert.Equal(t, 1, printed.Total)
	assert.False(t, printed.Truncated)
	require.Len(t, printed.WorkItems, 1)
	record := printed.WorkItems[0]
	assert.Regexp(t, internalIDForm, record["id"])
	assert.Equal(t, "PT1H30M", record["duration"])
	assert.Equal(t, map[string]any{"name": "Разработка"}, record["type"])
	assert.Equal(t, "2026-09-01T00:00:00Z", record["date"])
	assert.Equal(t, map[string]any{"login": "admin"}, record["author"])
	assert.Equal(t, "Разбор полигона", record["text"])
	require.Len(t, workItemRecords(t, got), 1)
	assert.Equal(t, []string{"id", "duration", "type", "attributes", "author", "date", "text"},
		recordKeys(workItemRecords(t, got)[0]))
	assert.Equal(t, []string{workItemsPath("DEV-1")}, dev.sentPaths())
}

// The one work item of DEV-7, the fixture where the moment the server keeps differs from the moment it was
// written at: 15:00 UTC of 1 September, filed by a token whose profile is Asia/Vladivostok, stands under 2
// September. This is what the refusal of a time of day rests on — YouTrack files a moment under the
// calendar day of the writer's own zone, which the caller has no way to know.
func TestTimeListPrintsTheWorkItemOfDEV7OfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "time", "list", "DEV-7")

	printed := requireWorkItemListing(t, got)
	assert.Equal(t, 1, printed.Total)
	require.Len(t, printed.WorkItems, 1)
	record := printed.WorkItems[0]
	assert.Regexp(t, internalIDForm, record["id"])
	assert.Equal(t, "PT30M", record["duration"])
	assert.Equal(t, map[string]any{"name": "Разработка"}, record["type"])
	assert.Equal(t, map[string]any{"login": "dev.member"}, record["author"])
	assert.Equal(t, "2026-09-02T00:00:00Z", record["date"])
	assert.Equal(t, "Разбор истории правок", record["text"])
	assert.Equal(t, []string{workItemsPath("DEV-7")}, dev.sentPaths())
}

// A page that fills the limit is counted against the polygon too, and the counting pass asks for ids alone.
func TestTimeListCountsTheWorkItemsOfDEV1OfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "time", "list", "DEV-1", "--limit", "1")

	printed := requireWorkItemListing(t, got)
	assert.Equal(t, 1, printed.Total)
	assert.Equal(t, 1, printed.Returned)
	assert.False(t, printed.Truncated)
	assert.Equal(t, listingWorkItems("1"), dev.sentQueries())
}

// An issue of the polygon that nobody wrote time against prints an empty list rather than a refusal.
func TestTimeListPrintsNoWorkItemOfDEV2OfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "time", "list", "DEV-2")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nworkItems: []\n"}, got)
	assert.Len(t, dev.requests(), 1)
}

// An issue the polygon has none of and one this token may not see are the same 404 of the server, in one
// request: the work items of an issue are asked for under the issue, so its absence is the server's to answer.
func TestTimeListRefusesAnIssueTheDevInstanceDoesNotShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		id    string
		token func(*testing.T) string
	}{
		{
			name:  "an issue the polygon has none of",
			id:    "DEV-99999",
			token: func(t *testing.T) string { t.Helper(); return devTokens(t).admin },
		},
		{
			name:  "an issue hidden from the limited token",
			id:    "DEV-1",
			token: func(t *testing.T) string { t.Helper(); return devTokens(t).limited },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + tc.token(t)}, "time", "list", tc.id)

			found := requireRefusal(t, got)
			assert.Equal(t, "not_found", found.code)
			assert.Equal(t, "Entity with id "+tc.id+" not found", detailNamed(t, found, "upstream_message"))
			assert.Equal(t, []string{workItemsPath(tc.id)}, dev.sentPaths())
		})
	}
}

// A member of the project reads the work items of its issues by the default, so no key of it costs a
// reader their document.
func TestTimeListPrintsTheWorkItemOfDEV1ToTheMemberOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "time", "list", "DEV-1")

	printed := requireWorkItemListing(t, got)
	require.Len(t, printed.WorkItems, 1)
	assert.Equal(t, "Разбор полигона", printed.WorkItems[0]["text"])
	assert.Equal(t, []string{sentWorkItemFields}, dev.sentFields())
}

// The blocks of the issue a work item hangs from are composed by ytrack rather than answered as they were
// asked for, so every name an expression may not put under one is refused here exactly as it is at ytrack issue
// show — and at every verb that prints a work item, since one expression serves all three.
func TestTimeRefusesTheNamesUnderABlockOfTheIssue(t *testing.T) {
	t.Parallel()
	verbs := []struct {
		name string
		argv []string
	}{
		{name: "a listing", argv: []string{"time", "list", "DEV-1"}},
		{name: "a creation", argv: []string{"time", "create", "DEV-1", "PT1H"}},
		{name: "an update", argv: []string{"time", "update", "DEV-1", "199-6", "--text", "x"}},
	}
	written := []struct {
		name       string
		expression string
	}{
		{name: "a part of a link slot", expression: "issue(links(direction))"},
		{name: "a part of the parent slot", expression: "issue(parent(id))"},
		{name: "a part of the subtasks slot", expression: "issue(subtasks(linkType(sourceToTarget)))"},
		{name: "a custom field of the issue named", expression: "issue(customFields(State))"},
	}
	for _, verb := range verbs {
		for _, tc := range written {
			t.Run(verb.name+", "+tc.name, func(t *testing.T) {
				t.Parallel()
				server := serveNothing(t)

				got := runWith(t, server.env(), slices.Concat(verb.argv, []string{"--fields", tc.expression})...)

				assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
				assert.Empty(t, server.requests())
			})
		}
	}
}

// What a link slot does hold is the issues at its other end, and an expression that asks for them alone
// goes out with the parts the phrase is read from filled in beside them.
func TestTimeListAsksTheIssuesOfALinkSlotOfTheIssue(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, "[]"))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "issue(links(issues(idReadable)))")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"issue(links(issues(idReadable),direction,linkType(sourceToTarget,targetToSource)))"},
		server.sentFields())
}

// Neither flag of the command is written twice, and an expression that does not parse is refused where
// every other one is: before the network.
func TestTimeListRefusesAnExpressionItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "the expression twice", flags: []string{"--fields", "id", "--fields", "text"}},
		{name: "an expression that does not parse", flags: []string{"--fields", "a,,b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"time", "list", "DEV-1"}, tc.flags)...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}
