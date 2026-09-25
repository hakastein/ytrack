package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const listedDEV = `{"name":"DEVELOPMENT","$type":"Project","shortName":"DEV"}`

const printedListedDEV = `  - {shortName: "DEV", name: "DEVELOPMENT"}` + "\n"

func listRequest(address, fields, top string) string {
	return "GET " + address + "/api/admin/projects?fields=" + fields + "&$top=" + top
}

func countedBy(records string, count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			count(w, r)
			return
		}
		fake.JSON(http.StatusOK, records)(w, r)
	}
}

func countingQueries(limit string) []url.Values {
	return []url.Values{
		{"fields": {"shortName,name"}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "project", "list", "--limit", tc.limit)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestProjectListRefusesALimitGivenTwice(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "project", "list", "--limit", "1", "--limit", "2")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
}

func TestProjectListRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "project", "list", "--fields", "a,,b")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
}

func TestProjectListAddsFieldsToTheDefaultOfTheList(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"id":"0-1","shortName":"DEV","name":"DEVELOPMENT","$type":"Project"}]`))

	got := runWith(t, server.Env(), "project", "list", "--fields", "+id")

	want := "total: 1\nreturned: 1\ntruncated: false\nprojects:\n" + `  - {shortName: "DEV", name: "DEVELOPMENT", id: "0-1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []url.Values{{"fields": {"shortName,name,id"}, "$top": {"50"}}}, server.Queries())
}

func TestProjectListSendsTheLimitAsTop(t *testing.T) {
	t.Parallel()
	for _, limit := range []string{"1", "2147483647"} {
		t.Run(limit, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

			got := runWith(t, server.Env(), "project", "list", "--limit", limit)

			assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nprojects: []\n"}, got)
			assert.Equal(t, []url.Values{{"fields": {"shortName,name"}, "$top": {limit}}}, server.Queries())
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
			server := fake.Serve(t, countedBy(`[`+listedDEV+`]`, fake.JSON(http.StatusOK, tc.count)))

			got := runWith(t, server.Env(), "project", "list", "--limit", "1")

			assert.Equal(t, outcome{stdout: tc.stdout}, got)
			assert.Equal(t, countingQueries("1"), server.Queries())
		})
	}
}

func TestProjectListRefusesMoreProjectsThanTheLimit(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+listedDEV+`,{"shortName":"OLD","name":"Old","$type":"Project"}]`))

	got := runWith(t, server.Env(), "project", "list", "--limit", "1")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 1}, {"returned", 2}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 1)
}

func TestProjectListRefusesACountBelowTheProjectsReceived(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, countedBy(`[`+listedDEV+`]`, fake.JSON(http.StatusOK, `[]`)))

	got := runWith(t, server.Env(), "project", "list", "--limit", "1")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 0}, {"returned", 1}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, countingQueries("1"), server.Queries())
}

func TestProjectListRefusesACountWhoseAnswerBreaksOff(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, countedBy(`[`+listedDEV+`]`, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if assert.NoError(t, err) {
			assert.NoError(t, conn.Close())
		}
	}))

	got := runWith(t, server.Env(), "project", "list", "--limit", "1")

	want := faultDocument{code: "upstream_failed", details: []detail{{"request", listRequest(server.URL, "id", "-1")}}}
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.Requests(), 2)
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
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			got := runWith(t, server.Env(), "project", "list")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", listRequest(server.URL, "shortName,name", "50")},
					{"upstream_status", 200},
					{"upstream_body", tc.body},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.Requests(), 1)
		})
	}
}

func TestProjectListRefusesAFieldAProjectDidNotBring(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		limit   string
		handler http.HandlerFunc
		fields  string
		top     string
		missing []any
	}{
		{
			name:    "a field of one project of several",
			limit:   "50",
			handler: fake.JSON(http.StatusOK, `[`+listedDEV+`,{"shortName":"OLD","$type":"Project"}]`),
			fields:  "shortName,name",
			top:     "50",
			missing: []any{missingEntry("name", "Project")},
		},
		{
			name:    "a field every project lacks",
			limit:   "50",
			handler: fake.JSON(http.StatusOK, `[{"shortName":"DEV","$type":"Project"},{"shortName":"OLD","$type":"Project"}]`),
			fields:  "shortName,name",
			top:     "50",
			missing: []any{missingEntry("name", "Project")},
		},
		{
			name:    "every field of a record of a schema that may not stand in the list",
			limit:   "50",
			handler: fake.JSON(http.StatusOK, `[`+listedDEV+`,{"login":"admin","$type":"User"}]`),
			fields:  "shortName,name",
			top:     "50",
			missing: []any{missingEntry("shortName", "User"), missingEntry("name", "User")},
		},
		{
			name:    "the id of a project counted",
			limit:   "1",
			handler: countedBy(`[`+listedDEV+`]`, fake.JSON(http.StatusOK, `[{"id":"0-0","$type":"Project"},{"$type":"Project"}]`)),
			fields:  "id",
			top:     "-1",
			missing: []any{missingEntry("id", "Project")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, tc.handler)

			got := runWith(t, server.Env(), "project", "list", "--limit", tc.limit)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", listRequest(server.URL, tc.fields, tc.top)},
					{"fields", tc.fields},
					{"missing", tc.missing},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
}
