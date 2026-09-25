package cli_test

import (
	"net/http"
	"slices"
	"strings"
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

func TestProjectShowHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"project", "show", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, defaultProjectFields)
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

func TestProjectShowPrintsTheFieldsOfTheDevInstanceInTheOrderAsked(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields", " name , shortName ")

	assert.Equal(t, outcome{stdout: "name: \"DEVELOPMENT\"\nshortName: \"DEV\"\n"}, got)
	assert.Equal(t, []string{"name,shortName"}, dev.sentFields())
}

func TestProjectShowPrintsTheEmptyValuesOfTheDevInstanceApart(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields", "shortName,fromEmail,replyToEmail,createdBy(login),leader")

	const want = `shortName: "DEV"
fromEmail: ""
replyToEmail: null
createdBy: null
leader: {}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, dev.requests(), 1)
}

func TestProjectShowAddsFieldsToTheDefaultOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields", "+leader(login,fullName)")

	assert.Empty(t, got.stderr)
	assert.Equal(t, 0, got.code)
	assert.Equal(t, []string{defaultProjectFields + ",leader(login,fullName)"}, dev.sentFields())
	assert.Regexp(t, `\nleader:\n  login: "admin"\n  fullName: "[^"\n]*"\n$`, got.stdout)
	assert.True(t, strings.HasPrefix(got.stdout, printedDevProject), "stdout: %s", got.stdout)
}

func TestProjectShowSendsAFieldGivenTwiceInOneFormToTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields", "leader(login),leader")

	assert.Equal(t, outcome{stdout: "leader:\n  login: \"admin\"\n"}, got)
	assert.Equal(t, []string{"leader(login)"}, dev.sentFields())
}

func TestProjectShowRefusesAFieldTheDevInstanceFailsOn(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields", "startingNumber")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	require.Len(t, found.details, 4, "details: %v", found.details)
	want := []detail{
		{"request", "GET " + dev.url + "/api/admin/projects/DEV?fields=startingNumber"},
		{"upstream_status", 500},
		{"upstream_error", "server_error"},
	}
	assert.Equal(t, want, found.details[:3])
	assert.Equal(t, "upstream_message", found.details[3].key)
	assert.Contains(t, found.details[3].value, `Cannot invoke "java.lang.Number.longValue()"`)
	assert.Len(t, dev.requests(), 1)
}

func TestProjectShowPrintsAListOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields", "team(users(login))")

	assert.Equal(t, outcome{stdout: "team:\n  users:\n    - {login: \"admin\"}\n"}, got)
	assert.Len(t, dev.requests(), 1)
}
