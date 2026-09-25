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

const workItemListFields = "id,duration,type(name),attributes,author(login),date,text"

const sentWorkItemFields = "id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date,text"

func workItemsPath(id string) string {
	return "/api/issues/" + id + "/timeTracking/workItems"
}

const internalIDForm = `^[0-9]+-[0-9]+$`

const (
	sentNoAttribute    = `"attributes":[{"$type":"WorkItemAttribute","id":"309-0","name":"Формат работы","value":null}]`
	sentAgentAttribute = `"attributes":[{"name":"Формат работы","$type":"WorkItemAttribute","id":"309-0",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"506-1","name":"ИИагент"}}]`
)

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

func listingWorkItems(limit string) []url.Values {
	return []url.Values{
		{"fields": {sentWorkItemFields}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
}

func countedWorkItems(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			count(w, r)
			return
		}
		respondWith(http.StatusOK, records)(w, r)
	}
}

type workItemListing struct {
	Total     int              `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated bool             `yaml:"truncated"`
	WorkItems []map[string]any `yaml:"workItems"`
}

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

func TestTimeListRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the command with no subcommand", argv: []string{"time"}},
		{name: "a subcommand the command has none of", argv: []string{"time", "bogus"}},
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeListHelpNamesItsDefaults(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"time", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, workItemListFields)
}

func TestTimeCommandNamesItsSubcommands(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"time", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"create", "delete", "list", "update"}, availableCommands(t, got.stdout))
}

func TestTimeCommandIsOfferedByTheWordsOfEveryOtherCommand(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	answered := requireCompleted(t, runWith(t, server.env(), "__complete", "tim"))

	require.Len(t, answered.suggestions, 1)
	name, text, found := strings.Cut(answered.suggestions[0], "\t")
	assert.Equal(t, "time", name)
	assert.True(t, found, "the group came back with nothing beside it")
	assert.NotEmpty(t, text)
}

func TestTimeListAsksTheWorkItemsOfTheIssueInOneRequest(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

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

func workItemRecords(t *testing.T, got outcome) []*yaml.Node {
	t.Helper()
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "workItems").Content
}

func TestTimeListPrintsTheTextOfAWorkItemOnOneLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
		want     string
	}{
		{name: "a line feed", received: `"a\nb"`, want: `"a\nb"`},
		{name: "a carriage return", received: `"a\rb"`, want: `"a\rb"`},
		{name: "a line separator", received: "\"a\xe2\x80\xa8b\"", want: `"a\Lb"`},
		{name: "nothing at all", received: `""`, want: `""`},
		{name: "no text", received: `null`, want: `null`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `[{"$type":"IssueWorkItem","id":"199-6","author":null,"text":` + tc.received + `}]`
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,author(login),text")

			want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
				`  - {id: "199-6", author: null, text: ` + tc.want + "}\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

func TestTimeListKeepsEveryByteOfTheTextOfAWorkItem(t *testing.T) {
	t.Parallel()
	const written = "первая\nвторая\rтретья\xe2\x80\xa8четвёртая"
	body := "[{\"$type\":\"IssueWorkItem\",\"id\":\"199-6\",\"text\":\"первая\\nвторая\\rтретья\xe2\x80\xa8четвёртая\"}]"
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,text")

	printed := requireWorkItemListing(t, got)
	require.Len(t, printed.WorkItems, 1)
	assert.Equal(t, written, printed.WorkItems[0]["text"])
	assert.Equal(t, 1, strings.Count(strings.TrimSuffix(got.stdout, "\n"), "\n  - "))
}

func TestTimeListCountsTheWorkItemsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	const found = `[{"id":"199-6","$type":"IssueWorkItem"},{"id":"199-7","$type":"IssueWorkItem"},` +
		`{"id":"199-8","$type":"IssueWorkItem"}]`
	server := serve(t, countedWorkItems("["+listedWorkItem+","+listedSecondWorkItem+"]", respondWith(http.StatusOK, found)))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--limit", "2")

	want := "total: 3\nreturned: 2\ntruncated: true\nworkItems:\n" + printedWorkItemRow + printedSecondRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, listingWorkItems("2"), server.sentQueries())
	assert.Equal(t, []string{workItemsPath("DEV-1"), workItemsPath("DEV-1")}, server.sentPaths())
}

func TestTimeListCountsNothingWhenThePageIsShortOfTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, "["+listedWorkItem+"]"))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--limit", "2")

	want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" + printedWorkItemRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, server.requests(), 1)
}

func TestTimeListRefusesWhenTheCountDoesNotMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		count http.HandlerFunc
		want  faultDocument
	}{
		{
			name:  "a count of none over a page of one",
			count: respondWith(http.StatusOK, "[]"),
			want: faultDocument{
				code:    "upstream_failed",
				details: []detail{{"total", 0}, {"returned", 1}},
			},
		},
		{
			name:  "a server that failed the count",
			count: respondWith(http.StatusInternalServerError, `{"error":"server_error","error_description":"java.lang.NullPointerException"}`),
			want: faultDocument{
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
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 2)
		})
	}
}

func TestTimeListRefusesMoreWorkItemsThanTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 1)
}

func TestTimeListPrintsTheSameDocumentHoweverManyWereReceived(t *testing.T) {
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
				found := requireFault(t, got)
				assert.Equal(t, "upstream_invalid", found.code)
				return
			}
			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

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
	assert.Equal(t, "2026-09-02T00:00:00Z", record["date"],
		"the member wrote it at 15:00 UTC on 1 September, and their profile is Asia/Vladivostok")
	assert.Equal(t, "Разбор истории правок", record["text"])
	assert.Equal(t, []string{workItemsPath("DEV-7")}, dev.sentPaths())
}

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

func TestTimeListPrintsNoWorkItemOfDEV2OfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "time", "list", "DEV-2")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nworkItems: []\n"}, got)
	assert.Len(t, dev.requests(), 1)
}

func TestTimeListRefusesAnIssueTheDevInstanceDoesNotShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		id    string
		token func(*testing.T) string
	}{
		{
			name:  "an issue the dev instance has none of",
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

			found := requireFault(t, got)
			assert.Equal(t, "not_found", found.code)
			assert.Equal(t, "Entity with id "+tc.id+" not found", detailNamed(t, found, "upstream_message"))
			assert.Equal(t, []string{workItemsPath(tc.id)}, dev.sentPaths())
		})
	}
}

func TestTimeListPrintsTheWorkItemOfDEV1ToTheMemberOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "time", "list", "DEV-1")

	printed := requireWorkItemListing(t, got)
	require.Len(t, printed.WorkItems, 1)
	assert.Equal(t, "Разбор полигона", printed.WorkItems[0]["text"])
	assert.Equal(t, []string{sentWorkItemFields}, dev.sentFields())
}

func TestTimeRefusesTheNamesUnderABlockOfTheIssue(t *testing.T) {
	t.Parallel()
	subcommands := []struct {
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
	for _, subcommand := range subcommands {
		for _, tc := range written {
			t.Run(subcommand.name+", "+tc.name, func(t *testing.T) {
				t.Parallel()
				server := serveNothing(t)

				got := runWith(t, server.env(), slices.Concat(subcommand.argv, []string{"--fields", tc.expression})...)

				assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
				assert.Empty(t, server.requests())
			})
		}
	}
}

func TestTimeListAsksTheIssuesOfALinkSlotOfTheIssue(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, "[]"))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "issue(links(issues(idReadable)))")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"issue(links(issues(idReadable),direction,linkType(sourceToTarget,targetToSource)))"},
		server.sentFields())
}

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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}
