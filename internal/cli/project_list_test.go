package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const listedDEV = `{"name":"DEVELOPMENT","$type":"Project","shortName":"DEV"}`

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

func TestProjectListRefusesAnAnswerOfAnotherShape(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, listedDEV))

	got := runWith(t, server.Env(), "project", "list", "--fields", "shortName")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", listRequest(server.URL, "shortName", "50")},
			{"upstream_status", 200},
			{"upstream_body", listedDEV},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
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

			got := runWith(t, server.Env(), "project", "list", "--limit", tc.limit, "--fields", "shortName,name")

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
