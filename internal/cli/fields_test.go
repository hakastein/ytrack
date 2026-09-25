package cli_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestProjectShowRefusesFieldsThatDoNotParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
	}{
		{name: "nothing", fields: ""},
		{name: "a plus alone", fields: "+"},
		{name: "a comma at the end", fields: "a,"},
		{name: "a comma at the start", fields: ",a"},
		{name: "two commas", fields: "a,,b"},
		{name: "empty parentheses", fields: "a()"},
		{name: "an unclosed parenthesis", fields: "a(b"},
		{name: "an unopened parenthesis", fields: "a)"},
		{name: "a space between names", fields: "a b"},
		{name: "a plus inside", fields: "+a+b"},
		{name: "a name in Cyrillic", fields: "поле"},
		{name: "a tab is one column", fields: "a,\t\t)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", tc.fields)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestProjectShowRefusesANameItCannotPrintAsAKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
	}{
		{name: "a word read as a bool", fields: "on"},
		{name: "a word read as a bool, letter case aside", fields: "leader(login,No)"},
		{name: "a word read as null, added to the default", fields: "+null"},
		{name: "a leading digit", fields: "1abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", tc.fields)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestProjectShowRefusesFieldsGivenTwice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
	}{
		{
			name:  "two expressions",
			flags: []string{"--fields", "name", "--fields", "shortName"},
		},
		{
			name:  "one expression twice",
			flags: []string{"--fields=name", "--fields=name"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"project", "show", "DEV"}, tc.flags)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestProjectShowSendsEachFieldOnceInOneForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
		sent   string
	}{
		{name: "spaces and tabs around names and punctuation", fields: " \tname\t, leader ( login ) ", sent: "name,leader(login)"},
		{name: "a name given again keeps its first place", fields: "name,shortName,name", sent: "name,shortName"},
		{name: "a name given bare before its fields", fields: "leader,leader(login)", sent: "leader(login)"},
		{name: "fields merged at depth", fields: "team(users(login)),name,team(users(fullName),name)", sent: "team(users(login,fullName),name),name"},
		{name: "a new name added to the default", fields: "+description", sent: defaultProjectFields + ",description"},
		{name: "a name of the default added to it", fields: "+name,description", sent: defaultProjectFields + ",description"},
		{name: "spaces around the plus", fields: " + description", sent: defaultProjectFields + ",description"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, `{"shortName":"DEV","name":"DEVELOPMENT","archived":false,"description":null,`+
				`"leader":{"login":"admin","fullName":"Administrator"},"team":{"name":"DEV Team","users":[{"login":"admin","fullName":"Administrator"}]},`+
				`"plugins":{"timeTrackingSettings":{"enabled":true,"workItemTypes":[]}}}`))

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", tc.fields)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{tc.sent}, server.sentFields())
		})
	}
}

func TestProjectShowPrintsTheTypeWhenAskedFor(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, projectDEV))

	got := runWith(t, server.env(), "project", "show", "DEV", "--fields", "$type,shortName")

	assert.Equal(t, outcome{stdout: "$type: \"Project\"\nshortName: \"DEV\"\n"}, got)
}

func TestProjectShowPrintsScalarsAsReceived(t *testing.T) {
	t.Parallel()
	const pastFloat64Precision = "9007199254740993"
	server := serve(t, respondWith(http.StatusOK, `{"name":"[bug] fix login","archived":true,"startingNumber":`+
		pastFloat64Precision+`,"issues":[],"leader":null,"$type":"Project"}`))

	got := runWith(t, server.env(), "project", "show", "DEV", "--fields", "name,archived,startingNumber,issues(idReadable),leader(login)")

	const want = `name: "[bug] fix login"
archived: true
startingNumber: ` + pastFloat64Precision + `
issues: []
leader: null
`
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestProjectShowPrintsAListOneFlowItemALine(t *testing.T) {
	t.Parallel()
	const body = `{"shortName": "DEV", "team": {"name": "DEV Team", "users": [` +
		`{"login": "admin", "banned": false, "online": 1, "profile": {}, "groups": [{"name": "All Users"}, {"name": "DEV \"Team\""}], "tags": []}, ` +
		`{"login": "dev.member", "banned": true, "online": null, "profile": null, "groups": [], "tags": ["a", "b"]}]}, ` +
		`"watchers": [], "codes": [1, null, "DEV", true, [], {}]}`
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "project", "show", "DEV", "--fields", "shortName,team(name,users(login,banned,online,profile,groups(name),tags)),watchers,codes")

	const want = `shortName: "DEV"
team:
  name: "DEV Team"
  users:
    - {login: "admin", banned: false, online: 1, profile: {}, groups: [{name: "All Users"}, {name: "DEV \"Team\""}], tags: []}
    - {login: "dev.member", banned: true, online: null, profile: null, groups: [], tags: ["a", "b"]}
watchers: []
codes:
  - 1
  - null
  - "DEV"
  - true
  - []
  - {}
`
	assert.Equal(t, outcome{stdout: want}, got)
	var printed, received any
	require.NoError(t, yaml.Unmarshal([]byte(got.stdout), &printed))
	require.NoError(t, yaml.Unmarshal([]byte(body), &received))
	assert.Equal(t, received, printed)
}
