package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// DEV under the default expression of the list, $type added and the keys in an order other than asked: the server
// keeps an order of its own.
const listedDEV = `{"name":"DEVELOPMENT","$type":"Project","shortName":"DEV"}`

const printedListedDEV = `  - {shortName: "DEV", name: "DEVELOPMENT"}` + "\n"

// listDocument is the document project list prints, read back.
type listDocument struct {
	Total     int              `yaml:"total"`
	Returned  int              `yaml:"returned"`
	Truncated bool             `yaml:"truncated"`
	Projects  []map[string]any `yaml:"projects"`
}

func requireListing(t *testing.T, got outcome) listDocument {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed listDocument
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Projects, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

// A refusal names the request project list sends, with its fields= expression as it was written.
func listRequest(address, fields, top string) string {
	return "GET " + address + "/api/admin/projects?fields=" + fields + "&$top=" + top
}

// countedBy answers a request for projects with records, and the request that counts them, $top=-1, with count.
func countedBy(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			count(w, r)
			return
		}
		respondWith(http.StatusOK, records)(w, r)
	}
}

// countingQueries is what project list sends for a selection of limit projects that it goes on to count.
func countingQueries(limit string) []url.Values {
	return []url.Values{
		{"fields": {"shortName,name"}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
}

func TestProjectListTakesNoArgument(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "project", "list", "DEV")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
}

func TestProjectListRefusesALimitItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		limit string
	}{
		{name: "zero", limit: "0"},
		{name: "a negative number", limit: "-1"},
		{name: "past the largest int32", limit: "2147483648"},
		{name: "not a number", limit: "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "project", "list", "--limit", tc.limit)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
		})
	}
}

func TestProjectListRefusesALimitGivenTwice(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "project", "list", "--limit", "1", "--limit", "2")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
}

func TestProjectListRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "project", "list", "--fields", "a,,b")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
}

func TestProjectListAddsFieldsToTheDefaultOfTheList(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `[{"id":"0-1","shortName":"DEV","name":"DEVELOPMENT","$type":"Project"}]`))

	got := runWith(t, server.env(), "project", "list", "--fields", "+id")

	want := "total: 1\nreturned: 1\ntruncated: false\nprojects:\n" + `  - {shortName: "DEV", name: "DEVELOPMENT", id: "0-1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []url.Values{{"fields": {"shortName,name,id"}, "$top": {"50"}}}, server.sentQueries())
}

func TestProjectListHelpNamesTheDefaults(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"project", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "shortName,name")
}

func TestProjectListPrintsAnArchivedProjectAndCountsIt(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `[{"archived":false,"name":"DEVELOPMENT","shortName":"DEV","$type":"Project"},{"archived":true,"name":"Old","shortName":"OLD","$type":"Project"}]`))

	got := runWith(t, server.env(), "project", "list", "--fields", "+archived")

	want := "total: 2\nreturned: 2\ntruncated: false\nprojects:\n" +
		`  - {shortName: "DEV", name: "DEVELOPMENT", archived: false}` + "\n" +
		`  - {shortName: "OLD", name: "Old", archived: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	requests := server.requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "/api/admin/projects", requests[0].URL.Path)
	assert.Equal(t, url.Values{"fields": {"shortName,name,archived"}, "$top": {"50"}}, requests[0].URL.Query())
}

func TestProjectListSendsTheLimitAsTop(t *testing.T) {
	t.Parallel()
	for _, limit := range []string{"1", "2147483647"} {
		t.Run(limit, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, `[]`))

			got := runWith(t, server.env(), "project", "list", "--limit", limit)

			assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nprojects: []\n"}, got)
			assert.Equal(t, []url.Values{{"fields": {"shortName,name"}, "$top": {limit}}}, server.sentQueries())
		})
	}
}

func TestProjectListCountsTheProjectsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		count  string
		stdout string
	}{
		{
			name:   "more counted than arrived",
			count:  `[{"id":"0-0","$type":"Project"},{"id":"0-1","$type":"Project"},{"id":"0-2","$type":"Project"}]`,
			stdout: "total: 3\nreturned: 1\ntruncated: true\nprojects:\n" + printedListedDEV,
		},
		{
			name:   "as many counted as arrived",
			count:  `[{"id":"0-0","$type":"Project"}]`,
			stdout: "total: 1\nreturned: 1\ntruncated: false\nprojects:\n" + printedListedDEV,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, countedBy(`[`+listedDEV+`]`, respondWith(http.StatusOK, tc.count)))

			got := runWith(t, server.env(), "project", "list", "--limit", "1")

			assert.Equal(t, outcome{stdout: tc.stdout}, got)
			assert.Equal(t, countingQueries("1"), server.sentQueries())
		})
	}
}

func TestProjectListRefusesMoreProjectsThanTheLimit(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `[`+listedDEV+`,{"shortName":"OLD","name":"Old","$type":"Project"}]`))

	got := runWith(t, server.env(), "project", "list", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

func TestProjectListRefusesACountBelowTheProjectsReceived(t *testing.T) {
	t.Parallel()
	server := serve(t, countedBy(`[`+listedDEV+`]`, respondWith(http.StatusOK, `[]`)))

	got := runWith(t, server.env(), "project", "list", "--limit", "1")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 0}, {"returned", 1}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, countingQueries("1"), server.sentQueries())
}

func TestProjectListRefusesACountWhoseAnswerBreaksOff(t *testing.T) {
	t.Parallel()
	server := serve(t, countedBy(`[`+listedDEV+`]`, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if assert.NoError(t, err) {
			assert.NoError(t, conn.Close())
		}
	}))

	got := runWith(t, server.env(), "project", "list", "--limit", "1")

	want := faultDocument{code: "upstream_failed", details: []detail{{"request", listRequest(server.url, "id", "-1")}}}
	assert.Equal(t, want, requireRefusal(t, got))
	// net/http repeats on its own a request whose reused connection breaks.
	assert.Len(t, server.requests(), 2)
}

func TestProjectListRefusesAnAnswerOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "an object", body: listedDEV},
		{name: "a list holding a string", body: `["DEV"]`},
		{name: "a list holding null", body: `[null]`},
		{name: "a list holding a list", body: `[` + listedDEV + `,[]]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "project", "list")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", listRequest(server.url, "shortName,name", "50")},
					{"upstream_status", 200},
					{"upstream_body", tc.body},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestProjectListRefusesAFieldAProjectDidNotBring(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		limit   string
		handler http.HandlerFunc
		// Of the request the refusal names.
		fields  string
		top     string
		missing []any
	}{
		{
			name:    "a field of one project of several",
			limit:   "50",
			handler: respondWith(http.StatusOK, `[`+listedDEV+`,{"shortName":"OLD","$type":"Project"}]`),
			fields:  "shortName,name",
			top:     "50",
			missing: []any{missingEntry("name", "Project")},
		},
		{
			name:    "a field every project lacks",
			limit:   "50",
			handler: respondWith(http.StatusOK, `[{"shortName":"DEV","$type":"Project"},{"shortName":"OLD","$type":"Project"}]`),
			fields:  "shortName,name",
			top:     "50",
			missing: []any{missingEntry("name", "Project")},
		},
		{
			name:    "every field of a record of a schema that may not stand in the list",
			limit:   "50",
			handler: respondWith(http.StatusOK, `[`+listedDEV+`,{"login":"admin","$type":"User"}]`),
			fields:  "shortName,name",
			top:     "50",
			missing: []any{missingEntry("shortName", "User"), missingEntry("name", "User")},
		},
		{
			name:    "the id of a project counted",
			limit:   "1",
			handler: countedBy(`[`+listedDEV+`]`, respondWith(http.StatusOK, `[{"id":"0-0","$type":"Project"},{"$type":"Project"}]`)),
			fields:  "id",
			top:     "-1",
			missing: []any{missingEntry("id", "Project")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, tc.handler)

			got := runWith(t, server.env(), "project", "list", "--limit", tc.limit)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", listRequest(server.url, tc.fields, tc.top)},
					{"fields", tc.fields},
					{"missing", tc.missing},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
		})
	}
}

func TestProjectListPrintsTheProjectsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "list")

	printed := requireListing(t, got)
	assert.Contains(t, strings.SplitAfter(got.stdout, "\n"), printedListedDEV)
	var codes []any
	for _, project := range printed.Projects {
		codes = append(codes, project["shortName"])
	}
	assert.Contains(t, codes, "DEMO")
	assert.Equal(t, []url.Values{{"fields": {"shortName,name"}, "$top": {"50"}}}, dev.sentQueries())
}

func TestProjectListCountsTheProjectsOfTheDevInstanceBeyondTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "list", "--limit", "1")

	printed := requireListing(t, got)
	assert.Equal(t, 1, printed.Returned)
	assert.True(t, printed.Truncated)
	assert.GreaterOrEqual(t, printed.Total, 2)
	assert.Equal(t, countingQueries("1"), dev.sentQueries())
}

func TestProjectListCountsTheProjectsOfTheDevInstanceThatFillTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "list", "--limit", "2")

	requireListing(t, got)
	assert.Equal(t, countingQueries("2"), dev.sentQueries())
}

func TestProjectListPrintsNoProjectHiddenFromTheLimitedUser(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "project", "list")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nprojects: []\n"}, got)
	assert.Len(t, dev.requests(), 1)
}

func TestProjectListPrintsTheFieldsTheMemberAsksFor(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "project", "list", "--fields", "shortName,name")

	const want = `total: 3
returned: 3
truncated: false
projects:
  - {shortName: "DEV", name: "DEVELOPMENT"}
  - {shortName: "DOCS", name: "DOCS"}
  - {shortName: "DEMO", name: "Демопроект"}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, dev.requests(), 1)
}

func TestProjectListPrintsTheDefaultToTheMember(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "project", "list")

	const want = `total: 3
returned: 3
truncated: false
projects:
  - {shortName: "DEV", name: "DEVELOPMENT"}
  - {shortName: "DOCS", name: "DOCS"}
  - {shortName: "DEMO", name: "Демопроект"}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, dev.requests(), 1)
}

func TestProjectListRefusesANameTheSchemasOfTheDevInstanceDoNotDeclare(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "list", "--fields", "shortName,bogus")

	// Every project lacks the name, and the name is listed once.
	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", listRequest(dev.url, "shortName,bogus", "50")},
			{"fields", "shortName,bogus"},
			{"unknown", []any{unknownEntry("bogus", projectNames()...)}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}
