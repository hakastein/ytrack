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

// Requests are counted by endpoint, so a request a later change adds to a selection leaves the
// counts of these scenarios as they are.
const (
	issuesPath = "/api/issues"
	countPath  = "/api/issuesGetter/count"
)

// What issue list prints by default, which is what its help names, and the two custom fields of it as query
// parameters of their own, which is how the answer is cut down to them.
const (
	issueListFields = "idReadable,summary,customFields(State,Type),created"
	namedState      = "State"
	namedType       = "Type"
)

// The default with the block of custom fields filled in, which is what goes out as fields=.
const sentIssueListFields = "idReadable,summary," + translatedCustomFieldsFields + ",created"

// A record of a selection as the server sends it under the default expression: $type on every object, an id
// nobody asked for, and the keys in an order other than the one asked for, the custom fields included — the
// server answers Type before State, and the record prints them in the order the expression named them.
type listedIssue struct {
	id       string
	internal string
	summary  string
	state    string
	// Milliseconds since the epoch, as JSON holds them.
	created string
}

func (i listedIssue) sent() string {
	return `{"summary":` + strconv.Quote(i.summary) + `,"$type":"Issue","id":` + strconv.Quote(i.internal) +
		`,"customFields":` + receivedFields(
		receivedField{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
		receivedField{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement(i.state)},
	) + `,"idReadable":` + strconv.Quote(i.id) + `,"created":` + i.created + `}`
}

// The three records the scenarios of a selection are answered with. A created without milliseconds and one
// ending in a zero are there because a moment is printed with as much of a second as it holds.
func listedDEV1() string {
	return listedIssue{id: "DEV-1", internal: "3-19", summary: "[bug] fix login",
		state: "In Progress", created: "1789035410875"}.sent()
}

func listedDEV2() string {
	return listedIssue{id: "DEV-2", internal: "3-20", summary: "Вторая задача",
		state: "Отклонена", created: "1789035411000"}.sent()
}

func listedDEV3() string {
	return listedIssue{id: "DEV-3", internal: "3-21", summary: "Третья задача",
		state: "Новая", created: "1789035412400"}.sent()
}

const (
	printedDEV1Row = `  - {idReadable: "DEV-1", summary: "[bug] fix login", ` +
		`customFields: {"State": "In Progress", "Type": "Task"}, created: "2026-09-10T10:16:50.875Z"}` + "\n"
	printedDEV2Row = `  - {idReadable: "DEV-2", summary: "Вторая задача", ` +
		`customFields: {"State": "Отклонена", "Type": "Task"}, created: "2026-09-10T10:16:51Z"}` + "\n"
	printedDEV3Row = `  - {idReadable: "DEV-3", summary: "Третья задача", ` +
		`customFields: {"State": "Новая", "Type": "Task"}, created: "2026-09-10T10:16:52.4Z"}` + "\n"
)

// A refusal names the request issue list sends: the search stands escaped, the way it went out, while the
// fields= expression reads as written. The two names of the default follow the search, a parameter each.
func issueListRequest(address, query, fields, top string) string {
	return "GET " + address + issuesPath + "?query=" + query +
		"&customFields=" + namedState + "&customFields=" + namedType + "&fields=" + fields + "&$top=" + top
}

// The request that counts, which carries its search in a body rather than in the query of the URL.
func countRequest(address string) string {
	return "POST " + address + countPath + "?fields=count"
}

// issueListing is the document issue list prints, read back. total and truncated are pointers because a total
// the server would not say is printed null, and then so is whether anything was cut off.
type issueListing struct {
	Total     *int             `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated *bool            `yaml:"truncated"`
	Issues    []map[string]any `yaml:"issues"`
}

func requireIssueListing(t *testing.T, got outcome) issueListing {
	t.Helper()
	assert.Empty(t, got.stderr)
	return requireListingPrinted(t, got)
}

// requireListingPrinted is the document of a selection whatever stderr holds: a search the server looks for as
// text is warned about and is printed all the same.
func requireListingPrinted(t *testing.T, got outcome) issueListing {
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

// countHandler answers the counter with a body of the shape it answers with, whatever count reads as.
func countHandler(count string) http.HandlerFunc {
	return respondWith(http.StatusOK, `{"$type":"IssueCountResponse","count":`+count+`}`)
}

// countedIssues answers a selection with records and the request that counts them with count.
func countedIssues(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == countPath {
			count(w, r)
			return
		}
		respondWith(http.StatusOK, records)(w, r)
	}
}

// breakOff is an answer that never comes: the connection goes away once the request has been read whole.
func breakOff(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

// sentTo is how many requests of the server's log went to one endpoint.
func sentTo(server *upstream, path string) int {
	sent := 0
	for _, p := range server.sentPaths() {
		if p == path {
			sent++
		}
	}
	return sent
}

// countCalls keeps what each request to the counter carried: the log of the server keeps the request and not
// the body, which the handler is the last to see.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The flags of a list are the ones every list takes; what is checked here is that this command is wired to
// them, not the checks themselves.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// A selection prints a line an issue at a time, which a comment does not fit on, so comments are refused before
// any request wherever an issue stands in the expression, and the refusal names the command that prints them.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The help names the default field expression, which is data the help must keep in sync with the command's
// actual behaviour, rather than words this scenario would have to update if the wording around it changed.
func TestIssueListHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"issue", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, issueListFields)
}

// The search is the caller's and reaches the server as it stands: ytrack reads no word of it, so a text every
// local parser of the query language would refuse is sent and answered like any other. The markup that goes
// out ahead of it is asked about those same words, byte for byte.
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
			selection := requests[1]
			assert.Equal(t, issuesPath, selection.URL.Path)
			assert.Equal(t, []string{tc.query}, selection.URL.Query()["query"])
			assert.Equal(t, []string{"50"}, selection.URL.Query()["$top"])
			assert.Equal(t, []string{sentIssueListFields}, selection.URL.Query()["fields"])
			assert.Equal(t, []string{namedState, namedType}, selection.URL.Query()["customFields"])
		})
	}
}

// pflag reads the value of a flag as text wherever it starts, so a search needs no separator of its own, and a
// search that spells a flag of cobra's own is still a search.
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
		// -- is still read; it is no longer the way a leading dash gets through, and the help names it no more.
		{name: "a separator after the flag", flag: []string{"--query", "-тег", "--"}, query: "-тег"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, respondWith(http.StatusOK, `[`+listedDEV1()+`]`))

			got := runWith(t, server.env(), slices.Concat([]string{"issue", "list"}, tc.flag)...)

			// Printed rather than the help of the command, which cobra prints for --help anywhere else.
			want := "total: 1\nreturned: 1\ntruncated: false\nissues:\n" + printedDEV1Row
			assert.Equal(t, outcome{stdout: want}, got)
			requireMarkedUpFirst(t, server, tc.query)
			requests := server.requests()
			require.Len(t, requests, 2)
			assert.Equal(t, []string{tc.query}, requests[1].URL.Query()["query"])
		})
	}
}

// A record carries the keys asked for, in the order asked for, whatever order the server sent them in, and
// each one is a line of its own.
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

// The shape of the document is a function of the command and not of how many records arrived: one record is a
// list of one, and none is an empty list.
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

// A page longer than the limit means $top went out wrong or the server ignored it, and a count of it would be
// of something else.
func TestIssueListRefusesMoreIssuesThanTheLimit(t *testing.T) {
	t.Parallel()
	server := searching(t, respondWith(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--limit", "2")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 2}, {"returned", 3}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	requireMarkedUpFirst(t, server, "")
	assert.Equal(t, 1, sentTo(server, issuesPath))
	assert.Equal(t, 0, sentTo(server, countPath))
}

// A search the server will not run is the caller's to fix, and what it tripped over is the server's to say, so
// the answer passes on word for word.
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
	assert.Equal(t, want, requireRefusal(t, got))
	requireMarkedUpFirst(t, server, "State: Opne")
	assert.Equal(t, 1, sentTo(server, issuesPath))
}

// A page that fills the limit proves nothing about what is beyond it, so the server is asked how many the
// search finds; a page short of the limit is the whole of it and is counted by itself.
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

// The counter is asked the search in a body, since a POST is how the server takes it; the search it counts is
// the one the page came from, word for word.
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
	// The markup, the selection and the count, in that order.
	counts := server.sentQueries()[2]
	assert.Equal(t, url.Values{"fields": {"count"}}, counts)
}

// A count that fails takes the command with it: a document without it would have to say whether the rest were
// cut off, and nothing answered that.
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
			assert.Equal(t, want, requireRefusal(t, got))
			requireMarkedUpFirst(t, server, "a")
			assert.Equal(t, 1, sentTo(server, countPath))
		})
	}
}

// A count that never arrived is a read whose answer was lost: the instance is what it was, so the command
// refuses with the code of a failure rather than of a write it is unsure of.
func TestIssueListRefusesACountWhoseAnswerBreaksOff(t *testing.T) {
	t.Parallel()
	server := searching(t, countedIssues(`[`+listedDEV1()+`]`, breakOff))

	got := runWith(t, server.env(), "issue", "list", "--query", "a", "--limit", "1")

	got.stderr = strings.ReplaceAll(got.stderr, server.url, "<upstream>")
	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", countRequest("<upstream>")}}, found.details)
	requireMarkedUpFirst(t, server, "a")
	assert.Equal(t, 1, sentTo(server, countPath))
}

// A count below the records that arrived is the selection changing between the two requests, and the document
// has no way to say so: a total under returned would print truncated: false over a page that was cut.
func TestIssueListRefusesACountBelowTheIssuesReceived(t *testing.T) {
	t.Parallel()
	server := searching(t, countedIssues(`[`+listedDEV1()+`,`+listedDEV2()+`,`+listedDEV3()+`]`, countHandler("2")))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--limit", "3")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 2}, {"returned", 3}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	requireMarkedUpFirst(t, server, "")
	assert.Equal(t, 1, sentTo(server, countPath))
}

// Only a whole number of issues and the -1 of a count that is not ready are answers of the counter; anything
// else is the server saying something the protocol of its own has no place for.
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

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			requireMarkedUpFirst(t, server, "a")
			assert.Equal(t, 1, sentTo(server, countPath))
		})
	}
}

// The selection the contract scenarios run names its fixtures, so neither the issues a neighbouring test
// creates nor how many of them there are reaches the document; sort by settles the order the server keeps.
const devInstanceIssues = "issue id: DEV-1, DEV-2, DEV-3 sort by: {issue id} asc"

// records is the records of a printed selection as they were printed, so a scenario can hold the keys to their
// order and a value to the style it was written in.
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

// A page short of the limit is the whole of what the search finds, so the counter is not asked at all.
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
	found := requireRefusal(t, got)
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
