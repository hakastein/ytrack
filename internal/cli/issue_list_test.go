package cli_test

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const (
	issuesPath = "/api/issues"
	countPath  = "/api/issuesGetter/count"
)

const (
	issueListFields = "idReadable,summary,customFields(State,Type),created"
	namedState      = "State"
	namedType       = "Type"
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
	return listedIssue{id: "DEV-1", internal: "3-19", summary: "[bug] fix login",
		state: "In Progress", createdEpochMillis: "1789035410875"}.sent()
}

func listedDEV2() string {
	return listedIssue{id: "DEV-2", internal: "3-20", summary: "Вторая задача",
		state: "Отклонена", createdEpochMillis: "1789035411000"}.sent()
}

func listedDEV3() string {
	return listedIssue{id: "DEV-3", internal: "3-21", summary: "Третья задача",
		state: "Новая", createdEpochMillis: "1789035412400"}.sent()
}

const (
	printedDEV1Row = `  - {idReadable: "DEV-1", summary: "[bug] fix login", ` +
		`customFields: {"State": "In Progress", "Type": "Task"}, created: "2026-09-10T10:16:50.875Z"}` + "\n"
	printedDEV2Row = `  - {idReadable: "DEV-2", summary: "Вторая задача", ` +
		`customFields: {"State": "Отклонена", "Type": "Task"}, created: "2026-09-10T10:16:51Z"}` + "\n"
	printedDEV3Row = `  - {idReadable: "DEV-3", summary: "Третья задача", ` +
		`customFields: {"State": "Новая", "Type": "Task"}, created: "2026-09-10T10:16:52.4Z"}` + "\n"
)

func issueListRequest(address, escapedQuery, fields, top string) string {
	return "GET " + address + issuesPath + "?query=" + escapedQuery +
		"&customFields=" + namedState + "&customFields=" + namedType + "&fields=" + fields + "&$top=" + top
}

func countRequest(address string) string {
	return "POST " + address + countPath + "?fields=count"
}

type issueListing struct {
	Total     *int             `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated *bool            `yaml:"truncated"`
	Issues    []map[string]any `yaml:"issues"`
}

func requireIssueListing(t *testing.T, got outcome) issueListing {
	t.Helper()
	assert.Empty(t, got.stderr)
	return requireListingIgnoringStderr(t, got)
}

func requireListingIgnoringStderr(t *testing.T, got outcome) issueListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed issueListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Issues, printed.Returned)
	require.NotNil(t, printed.Total, "stdout: %s", got.stdout)
	require.NotNil(t, printed.Truncated, "stdout: %s", got.stdout)
	assert.Equal(t, *printed.Total > printed.Returned, *printed.Truncated)
	return printed
}

func countHandler(count string) http.HandlerFunc {
	return respondWith(http.StatusOK, `{"$type":"IssueCountResponse","count":`+count+`}`)
}

func countedIssues(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == countPath {
			count(w, r)
			return
		}
		respondWith(http.StatusOK, records)(w, r)
	}
}

func breakOff(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func sentTo(server *upstream, path string) int {
	sent := 0
	for _, p := range server.sentPaths() {
		if p == path {
			sent++
		}
	}
	return sent
}

type countCalls struct {
	mu   sync.Mutex
	sent []countCall
}

type countCall struct {
	method      string
	contentType string
	body        string
	err         error
}

func (c *countCalls) recordingHandler(count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		c.mu.Lock()
		c.sent = append(c.sent, countCall{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			body:        string(body),
			err:         err,
		})
		c.mu.Unlock()
		count(w, r)
	}
}

func (c *countCalls) calls() []countCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sent)
}

func TestIssueListTakesItsSearchFromTheQueryFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no search at all", argv: []string{"issue", "list"}},
		{name: "a word of its own", argv: []string{"issue", "list", "DEV-1"}},
		{name: "two words of their own", argv: []string{"issue", "list", "a", "b"}},
		{name: "a word of its own that starts with a dash", argv: []string{"issue", "list", "-тег"}},
		{name: "the flag twice", argv: []string{"issue", "list", "--query", "a", "--query", "b"}},
		{name: "a search that is no UTF-8", argv: []string{"issue", "list", "--query", "\xff"}},
		{name: "a search holding a byte that is no UTF-8", argv: []string{"issue", "list", "--query", "State: \xc3\x28"}},
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
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"issue", "list", "--query", ""}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueListRefusesWhatOnlyIssueShowPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "comments in place of the default", expression: "comments(text)"},
		{name: "comments added to the default", expression: "+comments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", tc.expression)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueListHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"issue", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, issueListFields)
}

func TestIssueListSendsASearchItReadsNoWordOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
	}{
		{name: "brackets that never close", query: "(((("},
		{name: "a word behind a hash", query: "#Новая"},
		{name: "a leading dash", query: "-тег"},
		{name: "a field with nothing after it", query: "has:"},
		{name: "a name with nothing after it", query: "State:"},
		{name: "a quote that never closes", query: `"unclosed`},
		{name: "a closing brace alone", query: "}"},
		{name: "characters a query escapes", query: "a&b=c?d#e%20+f"},
		{name: "words of a text search", query: "Задача в работе"},
		{name: "a tab and a line feed inside", query: "State:\tIn\nProgress"},
		{name: "a line separator inside", query: "a\xe2\x80\xa8b"},
		{name: "a character outside the basic plane", query: "\xf0\x9f\x98\x80"},
		{name: "four kilobytes of it", query: strings.Repeat("Задача ", 512)},
		{name: "an empty search", query: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, respondWith(http.StatusOK, `[`+listedDEV1()+`]`))

			got := runWith(t, server.env(), "issue", "list", "--query", tc.query)

			assert.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			requireMarkedUpFirst(t, server, tc.query)
			requests := server.requests()
			require.Len(t, requests, 2)
			search := requests[1]
			assert.Equal(t, issuesPath, search.URL.Path)
			assert.Equal(t, []string{tc.query}, search.URL.Query()["query"])
			assert.Equal(t, []string{"50"}, search.URL.Query()["$top"])
			assert.Equal(t, []string{sentIssueListFields}, search.URL.Query()["fields"])
			assert.Equal(t, []string{namedState, namedType}, search.URL.Query()["customFields"])
		})
	}
}

func TestIssueListSearchesForATextThatLooksLikeAFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flag  []string
		query string
	}{
		{name: "a leading dash, apart", flag: []string{"--query", "-тег"}, query: "-тег"},
		{name: "a leading dash, joined", flag: []string{"--query=-тег"}, query: "-тег"},
		{name: "the word help, apart", flag: []string{"--query", "--help"}, query: "--help"},
		{name: "the word help, joined", flag: []string{"--query=--help"}, query: "--help"},
		{name: "a separator after the flag", flag: []string{"--query", "-тег", "--"}, query: "-тег"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, respondWith(http.StatusOK, `[`+listedDEV1()+`]`))

			got := runWith(t, server.env(), slices.Concat([]string{"issue", "list"}, tc.flag)...)

			want := "total: 1\nreturned: 1\ntruncated: false\nissues:\n" + printedDEV1Row
			assert.Equal(t, outcome{stdout: want}, got)
			requireMarkedUpFirst(t, server, tc.query)
			requests := server.requests()
			require.Len(t, requests, 2)
			assert.Equal(t, []string{tc.query}, requests[1].URL.Query()["query"])
		})
	}
}

func TestIssueListPrintsARecordToALine(t *testing.T) {
	t.Parallel()
	server := searching(t, respondWith(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "project: DEV")

	want := "total: 2\nreturned: 2\ntruncated: false\nissues:\n" + printedDEV1Row + printedDEV2Row
	assert.Equal(t, outcome{stdout: want}, got)
	requireMarkedUpFirst(t, server, "project: DEV")
	assert.Equal(t, 1, sentTo(server, issuesPath))
	assert.Equal(t, 0, sentTo(server, countPath))
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
			server := searching(t, respondWith(http.StatusOK, tc.records))

			got := runWith(t, server.env(), "issue", "list", "--query", "")

			assert.Equal(t, outcome{stdout: tc.want}, got)
			requireMarkedUpFirst(t, server, "")
		})
	}
}

func TestIssueListRefusesMoreIssuesThanTheLimit(t *testing.T) {
	t.Parallel()
	server := searching(t, respondWith(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--limit", "2")

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
	server := searching(t, respondWith(http.StatusBadRequest, said))

	got := runWith(t, server.env(), "issue", "list", "--query", "State: Opne")

	want := faultDocument{
		code: "rejected",
		details: []detail{
			{"request", issueListRequest(server.url, "State%3A+Opne", sentIssueListFields, "50")},
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
			server := searching(t, countedIssues(threeIssues, countHandler(tc.count)))

			got := runWith(t, server.env(), "issue", "list", "--query", "", "--limit", tc.limit)

			assert.Equal(t, outcome{stdout: tc.want}, got)
			requireMarkedUpFirst(t, server, "")
			assert.Equal(t, 1, sentTo(server, issuesPath))
			assert.Equal(t, tc.counts, sentTo(server, countPath))
		})
	}
}

func TestIssueListAsksTheCounterTheSearchItAskedThePageFor(t *testing.T) {
	t.Parallel()
	const query = `project: DEV "exact phrase" \ "`
	calls := &countCalls{}
	server := searching(t, countedIssues(`[`+listedDEV1()+`]`, calls.recordingHandler(countHandler("7"))))

	got := runWith(t, server.env(), "issue", "list", "--query", query, "--limit", "1")

	want := "total: 7\nreturned: 1\ntruncated: true\nissues:\n" + printedDEV1Row
	assert.Equal(t, outcome{stdout: want}, got)
	sent := []countCall{{
		method:      http.MethodPost,
		contentType: "application/json",
		body:        `{"query":"project: DEV \"exact phrase\" \\ \""}`,
	}}
	assert.Equal(t, sent, calls.calls())
	requireMarkedUpFirst(t, server, query)
	counted := countedAt(server)
	require.Len(t, counted, 1)
	assert.Equal(t, url.Values{"fields": {"count"}}, server.sentQueries()[counted[0]])
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
			count: respondWith(http.StatusInternalServerError, said),
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
			count: respondWith(http.StatusBadRequest, `{"error":"invalid_query","error_description":"Invalid query"}`),
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
			server := searching(t, countedIssues(`[`+listedDEV1()+`]`, tc.count))

			got := runWith(t, server.env(), "issue", "list", "--query", "a", "--limit", "1")

			want := tc.want
			want.details[0].value = countRequest(server.url)
			assert.Equal(t, want, requireFault(t, got))
			requireMarkedUpFirst(t, server, "a")
			assert.Equal(t, 1, sentTo(server, countPath))
		})
	}
}

func TestIssueListRefusesACountWhoseAnswerBreaksOff(t *testing.T) {
	t.Parallel()
	server := searching(t, countedIssues(`[`+listedDEV1()+`]`, breakOff))

	got := runWith(t, server.env(), "issue", "list", "--query", "a", "--limit", "1")

	got.stderr = strings.ReplaceAll(got.stderr, server.url, "<upstream>")
	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", countRequest("<upstream>")}}, found.details)
	requireMarkedUpFirst(t, server, "a")
	assert.Equal(t, 1, sentTo(server, countPath))
}

func TestIssueListRefusesACountBelowTheIssuesReceived(t *testing.T) {
	t.Parallel()
	server := searching(t, countedIssues(`[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`, countHandler("2")))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--limit", "3")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 2}, {"returned", 3}},
	}
	assert.Equal(t, want, requireFault(t, got))
	requireMarkedUpFirst(t, server, "")
	assert.Equal(t, 1, sentTo(server, countPath))
}

func TestIssueListRefusesACountThatIsNoNumberOfIssues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		count http.HandlerFunc
	}{
		{name: "a negative number other than -1", count: countHandler("-2")},
		{name: "a fraction", count: countHandler("1.5")},
		{name: "a number in quotes", count: countHandler(`"3"`)},
		{name: "nothing at all", count: countHandler("null")},
		{name: "no count in the answer", count: respondWith(http.StatusOK, `{"$type":"IssueCountResponse","id":"count"}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, countedIssues(`[`+listedDEV1()+`]`, tc.count))

			got := runWith(t, server.env(), "issue", "list", "--query", "a", "--limit", "1")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			requireMarkedUpFirst(t, server, "a")
			assert.Equal(t, 1, sentTo(server, countPath))
		})
	}
}

const devInstanceIssues = "issue id: DEV-1, DEV-2, DEV-3 sort by: {issue id} asc"

func records(t *testing.T, got outcome) []*yaml.Node {
	t.Helper()
	return nodeAt(t, requireMapping(t, "stdout", got.stdout), "issues").Content
}

func recordKeys(record *yaml.Node) []string {
	var keys []string
	for pair := range slices.Chunk(record.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}

func requireIssuesOfTheDevInstance(t *testing.T, got outcome, ids ...string) {
	t.Helper()
	printed := requireIssueListing(t, got)
	assert.Equal(t, len(ids), *printed.Total)
	assert.False(t, *printed.Truncated)
	found := records(t, got)
	require.Len(t, found, len(ids))
	for i, record := range found {
		assert.Equal(t, []string{"idReadable", "summary", "customFields", "created"}, recordKeys(record))
		assert.Equal(t, ids[i], nodeAt(t, record, "idReadable").Value)
		summary := nodeAt(t, record, "summary")
		assert.Equal(t, yaml.DoubleQuotedStyle, summary.Style)
		assert.NotEmpty(t, summary.Value)
		fields := nodeAt(t, record, "customFields")
		assert.Equal(t, []string{namedState, namedType}, recordKeys(fields))
		for pair := range slices.Chunk(fields.Content, 2) {
			assert.Equal(t, yaml.DoubleQuotedStyle, pair[0].Style, "the key %q stands bare", pair[0].Value)
			assert.NotEmpty(t, pair[1].Value)
		}
		assert.Regexp(t, instantForm, nodeAt(t, record, "created").Value)
	}
}

func TestIssueListPrintsTheIssuesOfTheDevInstanceTheSearchNames(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", devInstanceIssues)

	requireIssuesOfTheDevInstance(t, got, "DEV-1", "DEV-2", "DEV-3")
	requireMarkedUpFirst(t, dev, devInstanceIssues)
	assert.Equal(t, []url.Values{{"query": {devInstanceIssues}, "customFields": {namedState, namedType}, "fields": {sentIssueListFields}, "$top": {"50"}}}, dev.sentQueries()[1:])
}

func TestIssueListDoesNotCountTheIssuesOfTheDevInstanceThatFitTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", devInstanceIssues, "--limit", "4")

	requireIssuesOfTheDevInstance(t, got, "DEV-1", "DEV-2", "DEV-3")
	requireMarkedUpFirst(t, dev, devInstanceIssues)
	assert.Equal(t, []url.Values{{"query": {devInstanceIssues}, "customFields": {namedState, namedType}, "fields": {sentIssueListFields}, "$top": {"4"}}}, dev.sentQueries()[1:])
}

func TestIssueListPassesOnTheDevInstanceRefusingAValueOfAField(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	const query = "issue id: DEV-1 State: In Progress"

	got := runWith(t, dev.env(), "issue", "list", "--query", query)

	documents := documentsOf(t, got.stderr)
	require.Len(t, documents, 2, "stderr: %q", got.stderr)
	assert.Equal(t, warningOf(query, "Progress"), requireWarning(t, documents[0]))
	assert.Equal(t, 1, strings.Count(got.stderr, separator), "stderr: %q", got.stderr)
	got.stderr = got.stderr[strings.Index(got.stderr, separator)+len(separator):]
	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, detail{"upstream_status", 400}, found.details[1])
	assert.Equal(t, detail{"upstream_error", "invalid_query"}, found.details[2])
	body, isText := found.details[len(found.details)-1].value.(string)
	require.True(t, isText, "upstream_body: %v", found.details[len(found.details)-1])
	assert.Equal(t, "upstream_body", found.details[len(found.details)-1].key)
	assert.Contains(t, body, "«In»")
	requireMarkedUpFirst(t, dev, query)
	assert.Equal(t, 1, sentTo(dev, issuesPath))
}

func TestIssueListFindsNoIssueOfTheDevInstanceForTheLimitedToken(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "issue", "list", "--query", "")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nissues: []\n"}, got)
	requireMarkedUpFirst(t, dev, "")
	assert.Equal(t, 1, sentTo(dev, issuesPath))
}
