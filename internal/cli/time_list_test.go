package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const sentWorkItemFields = "id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date,text"

func workItemsPath(id string) string {
	return "/api/issues/" + id + "/timeTracking/workItems"
}

const (
	listedWorkItem = `{"author":{"login":"author","$type":"User"},"text":"First text","date":1788220800000,` +
		`"duration":{"minutes":90,"$type":"DurationValue"},` +
		`"attributes":[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode","value":null}],` +
		`"type":{"name":"First","$type":"WorkItemType"},"id":"199-6","$type":"IssueWorkItem"}`
	listedSecondWorkItem = `{"text":"Second text","$type":"IssueWorkItem","id":"199-7","date":1788307200000,` +
		`"duration":{"$type":"DurationValue","minutes":30},` +
		`"attributes":[{"name":"Mode","$type":"WorkItemAttribute","id":"9-1",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}}],` +
		`"type":{"name":"Second","$type":"WorkItemType"},"author":{"login":"second.author","$type":"User"}}`
)

const (
	printedWorkItemRow = `  - {id: "199-6", duration: "PT1H30M", type: {name: "First"}, attributes: {"Mode": null}, ` +
		`author: {login: "author"}, date: "2026-09-01T00:00:00Z", text: "First text"}` + "\n"
	printedSecondRow = `  - {id: "199-7", duration: "PT30M", type: {name: "Second"}, attributes: {"Mode": "Pair"}, ` +
		`author: {login: "second.author"}, date: "2026-09-02T00:00:00Z", text: "Second text"}` + "\n"
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
		fake.JSON(http.StatusOK, records)(w, r)
	}
}

func TestTimeListRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a limit of zero", argv: []string{"time", "list", "DEV-1", "--limit", "0"}},
		{name: "the limit twice", argv: []string{"time", "list", "DEV-1", "--limit", "1", "--limit", "2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTimeListAsksTheWorkItemsOfTheIssueInOneRequest(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

	got := runWith(t, server.Env(), "time", "list", "DEV-1")

	want := "total: 2\nreturned: 2\ntruncated: false\nworkItems:\n" + printedWorkItemRow + printedSecondRow
	assert.Equal(t, outcome{stdout: want}, got)
	requests := server.Requests()
	require.Len(t, requests, 1)
	assert.Equal(t, http.MethodGet, requests[0].Method)
	assert.Equal(t, workItemsPath("DEV-1"), requests[0].URL.Path)
	assert.Equal(t, url.Values{"fields": {sentWorkItemFields}, "$top": {"50"}}, requests[0].URL.Query())
}

func TestTimeListCountsTheWorkItemsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	const found = `[{"id":"199-6","$type":"IssueWorkItem"},{"id":"199-7","$type":"IssueWorkItem"},` +
		`{"id":"199-8","$type":"IssueWorkItem"}]`
	server := fake.Serve(t, countedWorkItems("["+listedWorkItem+","+listedSecondWorkItem+"]", fake.JSON(http.StatusOK, found)))

	got := runWith(t, server.Env(), "time", "list", "DEV-1", "--limit", "2")

	want := "total: 3\nreturned: 2\ntruncated: true\nworkItems:\n" + printedWorkItemRow + printedSecondRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, listingWorkItems("2"), server.Queries())
	assert.Equal(t, []string{workItemsPath("DEV-1"), workItemsPath("DEV-1")}, server.Paths())
}

func TestTimeListCountsNothingWhenThePageIsShortOfTheLimit(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedWorkItem+"]"))

	got := runWith(t, server.Env(), "time", "list", "DEV-1", "--limit", "2")

	want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" + printedWorkItemRow
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, server.Requests(), 1)
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
			count: fake.JSON(http.StatusOK, "[]"),
			want: faultDocument{
				code:    "upstream_failed",
				details: []detail{{"total", 0}, {"returned", 1}},
			},
		},
		{
			name:  "a server that failed the count",
			count: fake.JSON(http.StatusInternalServerError, `{"error":"server_error","error_description":"java.lang.NullPointerException"}`),
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
			server := fake.Serve(t, countedWorkItems("["+listedWorkItem+"]", tc.count))

			got := runWith(t, server.Env(), "time", "list", "DEV-1", "--limit", "1")

			want := tc.want
			for i, printed := range want.details {
				if printed.key == "request" {
					want.details[i].value = "GET " + server.URL + workItemsPath("DEV-1") + "?fields=id&$top=-1"
				}
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.Requests(), 2)
		})
	}
}

func TestTimeListRefusesMoreWorkItemsThanTheLimit(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "["+listedWorkItem+","+listedSecondWorkItem+"]"))

	got := runWith(t, server.Env(), "time", "list", "DEV-1", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 1)
}

func TestTimeListRefusesAnAnswerOfAnotherShape(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, listedWorkItem))

	got := runWith(t, server.Env(), "time", "list", "DEV-1")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "GET " + server.URL + workItemsPath("DEV-1") + "?fields=" + sentWorkItemFields + "&$top=50"},
			{"upstream_status", 200},
			{"upstream_body", listedWorkItem},
		},
	}, requireFault(t, got))
}

func TestTimeListRefusesAnExpressionItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "a name under the duration", flags: []string{"--fields", "duration(minutes)"}},
		{name: "the expression twice", flags: []string{"--fields", "id", "--fields", "text"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), slices.Concat([]string{"time", "list", "DEV-1"}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}
