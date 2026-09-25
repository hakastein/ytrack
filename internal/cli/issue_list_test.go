package cli_test

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	issuesPath = "/api/issues"
	countPath  = "/api/issuesGetter/count"
)

const (
	namedState = "State"
	namedType  = "Type"
)

const sentIssueListFields = "idReadable,summary," + translatedCustomFieldsFields + ",created"

type listedIssue struct {
	id                 string
	internal           string
	summary            string
	state              string
	createdEpochMillis string
}

func (i listedIssue) sent() string {
	return `{"summary":` + strconv.Quote(i.summary) + `,"$type":"Issue","id":` + strconv.Quote(i.internal) +
		`,"customFields":` + receivedFields(
		receivedField{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
		receivedField{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement(i.state)},
	) + `,"idReadable":` + strconv.Quote(i.id) + `,"created":` + i.createdEpochMillis + `}`
}

func listedDEV1() string {
	return listedIssue{id: "DEV-1", internal: "3-19", summary: "First",
		state: "Open", createdEpochMillis: "1788134400875"}.sent()
}

func listedDEV2() string {
	return listedIssue{id: "DEV-2", internal: "3-20", summary: "Second",
		state: "Closed", createdEpochMillis: "1788134401000"}.sent()
}

func listedDEV3() string {
	return listedIssue{id: "DEV-3", internal: "3-21", summary: "Third",
		state: "New", createdEpochMillis: "1788134402400"}.sent()
}

const (
	printedDEV1Row = `  - {idReadable: "DEV-1", summary: "First", ` +
		`customFields: {"State": "Open", "Type": "Task"}, created: "2026-08-31T00:00:00.875Z"}` + "\n"
	printedDEV2Row = `  - {idReadable: "DEV-2", summary: "Second", ` +
		`customFields: {"State": "Closed", "Type": "Task"}, created: "2026-08-31T00:00:01Z"}` + "\n"
	printedDEV3Row = `  - {idReadable: "DEV-3", summary: "Third", ` +
		`customFields: {"State": "New", "Type": "Task"}, created: "2026-08-31T00:00:02.4Z"}` + "\n"
)

func issueListRequest(address, escapedQuery, fields, top string) string {
	return "GET " + address + issuesPath + "?query=" + escapedQuery +
		"&customFields=" + namedState + "&customFields=" + namedType + "&fields=" + fields + "&$top=" + top
}

func countRequest(address string) string {
	return "POST " + address + countPath + "?fields=count"
}

func countHandler(count string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, `{"$type":"IssueCountResponse","count":`+count+`}`)
}

func countedIssues(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == countPath {
			count(w, r)
			return
		}
		fake.JSON(http.StatusOK, records)(w, r)
	}
}

func breakOff(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func sentTo(server *fake.Server, path string) int {
	sent := 0
	for _, p := range server.Paths() {
		if p == path {
			sent++
		}
	}
	return sent
}

func TestIssueListTakesItsSearchFromTheQueryFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no search at all", argv: []string{"issue", "list"}},
		{name: "the flag twice", argv: []string{"issue", "list", "--query", "a", "--query", "b"}},
		{name: "a search that is no UTF-8", argv: []string{"issue", "list", "--query", "\xff"}},
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

func TestIssueListRefusesTheFlagsOfAListItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{name: "a limit of zero", flags: []string{"--limit", "0"}},
		{name: "a limit past the largest int32", flags: []string{"--limit", "2147483648"}},
		{name: "the limit twice", flags: []string{"--limit", "1", "--limit", "2"}},
		{name: "the fields twice", flags: []string{"--fields", "idReadable", "--fields", "summary"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), slices.Concat([]string{"issue", "list", "--query", ""}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestIssueListRefusesCommentsAskedForInTheExpression(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "issue", "list", "--query", "", "--fields", "+comments")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestIssueListPrintsTheIssuesTheSearchFinds(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`]`)))

	got := runWith(t, server.Env(), "issue", "list", "--query", " project: DEV ", "--limit", "3")

	want := "total: 2\nreturned: 2\ntruncated: false\nissues:\n" + printedDEV1Row + printedDEV2Row
	assert.Equal(t, outcome{stdout: want}, got)
	requireMarkedUpFirst(t, server, " project: DEV ")
	assert.Equal(t, []string{fake.AssistPath, issuesPath}, server.Paths())
	assert.Equal(t, url.Values{
		"query":        {" project: DEV "},
		"customFields": {namedState, namedType},
		"fields":       {sentIssueListFields},
		"$top":         {"3"},
	}, server.Request(t, 1).URL.Query())
}

func TestIssueListPrintsAListWhateverTheCountOfRecords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		records string
		want    string
	}{
		{name: "no records at all", records: `[]`, want: "total: 0\nreturned: 0\ntruncated: false\nissues: []\n"},
		{
			name:    "one record",
			records: `[` + listedDEV1() + `]`,
			want:    "total: 1\nreturned: 1\ntruncated: false\nissues:\n" + printedDEV1Row,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, tc.records)))

			got := runWith(t, server.Env(), "issue", "list", "--query", "")

			assert.Equal(t, outcome{stdout: tc.want}, got)
			requireMarkedUpFirst(t, server, "")
		})
	}
}

func TestIssueListRefusesMoreIssuesThanTheLimit(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`)))

	got := runWith(t, server.Env(), "issue", "list", "--query", "", "--limit", "2")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 2}, {"returned", 3}},
	}
	assert.Equal(t, want, requireFault(t, got))
	requireMarkedUpFirst(t, server, "")
	assert.Equal(t, 1, sentTo(server, issuesPath))
	assert.Equal(t, 0, sentTo(server, countPath))
}

func TestIssueListPassesOnWhatTheServerSaysOfASearchItRefuses(t *testing.T) {
	t.Parallel()
	const child = "Unexpected token: 'Opne' at position 12"
	const said = `{"error":"invalid_query","error_description":"Invalid query",` +
		`"error_children":[{"error":"invalid_query","error_description":"` + child + `"}]}`
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusBadRequest, said)))

	got := runWith(t, server.Env(), "issue", "list", "--query", "State: Opne")

	want := faultDocument{
		code: "rejected",
		details: []detail{
			{"request", issueListRequest(server.URL, "State%3A+Opne", sentIssueListFields, "50")},
			{"upstream_status", 400},
			{"upstream_error", "invalid_query"},
			{"upstream_message", "Invalid query"},
			{"upstream_body", said},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	requireMarkedUpFirst(t, server, "State: Opne")
	assert.Equal(t, 1, sentTo(server, issuesPath))
}

func TestIssueListCountsOnlyAPageThatFillsTheLimit(t *testing.T) {
	t.Parallel()
	threeIssues := `[` + listedDEV1() + `,` + listedDEV2() + `,` + listedDEV3() + `]`
	const rows = printedDEV1Row + printedDEV2Row + printedDEV3Row
	tests := []struct {
		name   string
		limit  string
		count  string
		want   string
		counts int
	}{
		{
			name: "more issues than the page holds", limit: "3", count: "7", counts: 1,
			want: "total: 7\nreturned: 3\ntruncated: true\nissues:\n" + rows,
		},
		{
			name: "as many issues as the page holds", limit: "3", count: "3", counts: 1,
			want: "total: 3\nreturned: 3\ntruncated: false\nissues:\n" + rows,
		},
		{
			name: "a page short of the limit", limit: "4", count: "7", counts: 0,
			want: "total: 3\nreturned: 3\ntruncated: false\nissues:\n" + rows,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, countedIssues(threeIssues, countHandler(tc.count))))

			got := runWith(t, server.Env(), "issue", "list", "--query", "", "--limit", tc.limit)

			assert.Equal(t, outcome{stdout: tc.want}, got)
			requireMarkedUpFirst(t, server, "")
			assert.Equal(t, 1, sentTo(server, issuesPath))
			assert.Equal(t, tc.counts, sentTo(server, countPath))
		})
	}
}

func TestIssueListRefusesWhenTheCountFails(t *testing.T) {
	t.Parallel()
	const said = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	tests := []struct {
		name  string
		count http.HandlerFunc
		want  faultDocument
	}{
		{
			name:  "a server that failed",
			count: fake.JSON(http.StatusInternalServerError, said),
			want: faultDocument{
				code: "upstream_failed",
				details: []detail{
					{"request", "POST %s?fields=count"},
					{"upstream_status", 500},
					{"upstream_error", "server_error"},
					{"upstream_message", "java.lang.NullPointerException"},
				},
			},
		},
		{
			name:  "a search the counter refuses",
			count: fake.JSON(http.StatusBadRequest, `{"error":"invalid_query","error_description":"Invalid query"}`),
			want: faultDocument{
				code: "rejected",
				details: []detail{
					{"request", "POST %s?fields=count"},
					{"upstream_status", 400},
					{"upstream_error", "invalid_query"},
					{"upstream_message", "Invalid query"},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, countedIssues(`[`+listedDEV1()+`]`, tc.count)))

			got := runWith(t, server.Env(), "issue", "list", "--query", "a", "--limit", "1")

			want := tc.want
			want.details[0].value = countRequest(server.URL)
			assert.Equal(t, want, requireFault(t, got))
			requireMarkedUpFirst(t, server, "a")
			assert.Equal(t, 1, sentTo(server, countPath))
		})
	}
}

func TestIssueListRefusesACountWhoseAnswerBreaksOff(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Searching(t, countedIssues(`[`+listedDEV1()+`]`, breakOff)))

	got := runWith(t, server.Env(), "issue", "list", "--query", "a", "--limit", "1")

	got.stderr = strings.ReplaceAll(got.stderr, server.URL, "<upstream>")
	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", countRequest("<upstream>")}}, found.details)
	requireMarkedUpFirst(t, server, "a")
	assert.Equal(t, 1, sentTo(server, countPath))
}

func TestIssueListRefusesACountBelowTheIssuesReceived(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Searching(t, countedIssues(`[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`, countHandler("2"))))

	got := runWith(t, server.Env(), "issue", "list", "--query", "", "--limit", "3")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 2}, {"returned", 3}},
	}
	assert.Equal(t, want, requireFault(t, got))
	requireMarkedUpFirst(t, server, "")
	assert.Equal(t, 1, sentTo(server, countPath))
}

func recordKeys(record *yaml.Node) []string {
	var keys []string
	for pair := range slices.Chunk(record.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}
