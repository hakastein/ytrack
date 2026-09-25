package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func projectNames() []any {
	return []any{"$type", "archived", "createdBy", "customFields", "description", "fromEmail", "iconUrl", "id", "issues",
		"leader", "name", "replyToEmail", "shortName", "startingNumber", "team", "template"}
}

func userNames() []any {
	return []any{"$type", "avatarUrl", "banned", "email", "fullName", "guest", "id", "isAnonymized", "login", "name",
		"online", "profiles", "ringId", "savedQueries", "tags"}
}

func missingFieldDetails(address, fields, key string, entries ...[]detail) []detail {
	listed := []any{}
	for _, entry := range entries {
		listed = append(listed, entry)
	}
	return []detail{
		{"request", "GET " + address + "/api/admin/projects/DEV?fields=" + fields},
		{"fields", fields},
		{key, listed},
	}
}

func unknownEntry(field string, nearest ...any) []detail {
	return []detail{{"field", field}, {"nearest", append([]any{}, nearest...)}}
}

func missingEntry(field string, serverTypeOrNil any) []detail {
	return []detail{{"field", field}, {"type", serverTypeOrNil}}
}

func TestProjectShowRefusesAFieldMissingFromTheResponse(t *testing.T) {
	t.Parallel()
	const asked = "shortName,name,archived,leader(login)"
	tests := []struct {
		name    string
		fields  string
		body    string
		missing [][]detail
	}{
		{
			name:    "a field the named type declares",
			fields:  asked,
			body:    `{"leader":{"login":"admin","$type":"User"},"shortName":"DEV","archived":false,"$type":"Project"}`,
			missing: [][]detail{missingEntry("name", "Project")},
		},
		{
			name:    "a field of an object that names no type",
			fields:  asked,
			body:    `{"leader":{"login":"admin","$type":"User"},"shortName":"DEV","archived":false}`,
			missing: [][]detail{missingEntry("name", nil)},
		},
		{
			name:    "a field of an object of a type the specification does not have",
			fields:  asked,
			body:    `{"leader":{"login":"admin","$type":"User"},"shortName":"DEV","archived":false,"$type":"ArchivedProject"}`,
			missing: [][]detail{missingEntry("name", "ArchivedProject")},
		},
		{
			name:    "a nested field the named type declares",
			fields:  asked,
			body:    `{"leader":{"$type":"User"},"shortName":"DEV","name":"DEVELOPMENT","archived":false,"$type":"Project"}`,
			missing: [][]detail{missingEntry("leader(login)", "User")},
		},
		{
			name:    "fields asked of a string where the schema declares an object",
			fields:  asked,
			body:    `{"leader":"admin","shortName":"DEV","name":"DEVELOPMENT","archived":false,"$type":"Project"}`,
			missing: [][]detail{missingEntry("leader(login)", nil)},
		},
		{
			name:    "fields asked of a number where the schema declares an object",
			fields:  asked,
			body:    `{"leader":7,"shortName":"DEV","name":"DEVELOPMENT","archived":false,"$type":"Project"}`,
			missing: [][]detail{missingEntry("leader(login)", nil)},
		},
		{
			name:    "a field of an item of a list",
			fields:  "issues(idReadable)",
			body:    `{"issues":[{"idReadable":"DEV-1","$type":"Issue"},{"$type":"Issue"}],"$type":"Project"}`,
			missing: [][]detail{missingEntry("issues(idReadable)", "Issue")},
		},
		{
			name:    "fields asked of a string in a list of objects",
			fields:  "issues(idReadable)",
			body:    `{"issues":["DEV-1"],"$type":"Project"}`,
			missing: [][]detail{missingEntry("issues(idReadable)", nil)},
		},
		{
			name:    "a field absent from every item of a list once",
			fields:  "issues(idReadable,summary)",
			body:    `{"issues":[{"summary":"a","$type":"Issue"},{"summary":"b","$type":"Issue"}],"$type":"Project"}`,
			missing: [][]detail{missingEntry("issues(idReadable)", "Issue")},
		},
		{
			name:    "a field beside a name no schema declares",
			fields:  "shortName,bogus,name",
			body:    `{"shortName":"DEV","$type":"Project"}`,
			missing: [][]detail{missingEntry("name", "Project")},
		},
		{
			name:    "a field under a field of no schema, by the type named there",
			fields:  "customFields(field(name))",
			body:    `{"customFields":[{"field":{"name":"State","$type":"CustomField"},"$type":"StateProjectCustomField"},{"$type":"EnumProjectCustomField"}],"$type":"Project"}`,
			missing: [][]detail{missingEntry("customFields(field)", "EnumProjectCustomField")},
		},
		{
			name:   "every field of an object of a schema that may not stand at the root",
			fields: asked,
			body:   `{"login":"admin","$type":"User"}`,
			missing: [][]detail{
				missingEntry("shortName", "User"), missingEntry("name", "User"), missingEntry("archived", "User"), missingEntry("leader", "User"),
			},
		},
		{
			name:    "a field of an object of a schema that may not stand under its field",
			fields:  asked,
			body:    `{"leader":{"shortName":"X","$type":"Project"},"shortName":"DEV","name":"DEVELOPMENT","archived":false,"$type":"Project"}`,
			missing: [][]detail{missingEntry("leader(login)", "Project")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", tc.fields)

			want := faultDocument{code: "upstream_invalid", details: missingFieldDetails(server.url, tc.fields, "missing", tc.missing...)}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestProjectShowRefusesANameNoSchemaOfItsNodeDeclares(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fields  string
		body    string
		unknown [][]detail
	}{
		{
			name:    "the nearest name first",
			fields:  "tam",
			body:    `{"$type":"Project"}`,
			unknown: [][]detail{unknownEntry("tam", "team", "name")},
		},
		{
			name:    "letter case aside",
			fields:  "leader(LOGIN)",
			body:    `{"leader":{"login":"admin","$type":"User"},"$type":"Project"}`,
			unknown: [][]detail{unknownEntry("leader(LOGIN)", "login")},
		},
		{
			name:    "a name absent from every item of a list once",
			fields:  "issues(summery)",
			body:    `{"issues":[{"$type":"Issue"},{"$type":"Issue"}],"$type":"Project"}`,
			unknown: [][]detail{unknownEntry("issues(summery)", "summary")},
		},
		{
			name:    "under a field of no schema, by the types named there",
			fields:  "customFields(bundel)",
			body:    `{"customFields":[{"$type":"EnumProjectCustomField"},{"$type":"PeriodProjectCustomField"}],"$type":"Project"}`,
			unknown: [][]detail{unknownEntry("customFields(bundel)", "bundle")},
		},
		{
			name:    "at the root, a name another schema of the hierarchy of the answer declares",
			fields:  "owner",
			body:    `{"$type":"Project"}`,
			unknown: [][]detail{unknownEntry("owner", projectNames()...)},
		},
		{
			name:    "a name only the schema of an object that may not stand at its place declares",
			fields:  "leader(shortName)",
			body:    `{"leader":{"$type":"Project"},"$type":"Project"}`,
			unknown: [][]detail{unknownEntry("leader(shortName)", userNames()...)},
		},
		{
			name:    "under a field the schema above does not declare, by the type named there",
			fields:  "team(users(logn))",
			body:    `{"team":{"users":[{"login":"admin","$type":"User"}],"$type":"ProjectTeam"},"$type":"Project"}`,
			unknown: [][]detail{unknownEntry("team(users(logn))", "login")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", tc.fields)

			want := faultDocument{code: "unknown_name", details: missingFieldDetails(server.url, tc.fields, "unknown", tc.unknown...)}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestProjectShowLeavesOutAFieldTheNamedTypeDoesNotDeclare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
		body   string
		stdout string
	}{
		{
			name:   "names asked of an empty list",
			fields: "issues(bogus)",
			body:   `{"issues":[],"$type":"Project"}`,
			stdout: "issues: []\n",
		},
		{
			name:   "a field of another schema under a field of no schema",
			fields: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"Type","$type":"CustomField"},"bundle":{"name":"Type (DEV)","$type":"EnumBundle"},"$type":"EnumProjectCustomField"},` +
				`{"field":{"name":"Оценка","$type":"CustomField"},"$type":"PeriodProjectCustomField"}],"$type":"Project"}`,
			stdout: "customFields:\n  - {field: {name: \"Type\"}, bundle: {name: \"Type (DEV)\"}}\n  - {field: {name: \"Оценка\"}}\n",
		},
		{
			name:   "a field of no schema whose one type lacks a field its siblings declare",
			fields: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"Estimation","$type":"CustomField"},"$type":"PeriodProjectCustomField"},` +
				`{"field":{"name":"Spent time","$type":"CustomField"},"$type":"PeriodProjectCustomField"}],"$type":"Project"}`,
			stdout: "customFields:\n  - {field: {name: \"Estimation\"}}\n  - {field: {name: \"Spent time\"}}\n",
		},
		{
			name:   "a field of no schema whose first type lacks a field a type after it declares",
			fields: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"Estimation","$type":"CustomField"},"$type":"PeriodProjectCustomField"},` +
				`{"field":{"name":"Type","$type":"CustomField"},"bundle":{"name":"Type (DEV)","$type":"EnumBundle"},"$type":"EnumProjectCustomField"}],"$type":"Project"}`,
			stdout: "customFields:\n  - {field: {name: \"Estimation\"}}\n  - {field: {name: \"Type\"}, bundle: {name: \"Type (DEV)\"}}\n",
		},
		{
			name:   "a field of no schema that another schema of the hierarchy named there declares",
			fields: "customFields(owner)",
			body:   `{"customFields":[{"$type":"Project"}],"$type":"Project"}`,
			stdout: "customFields:\n  - {}\n",
		},
		{
			name:   "under a field no schema declares, a field of a type named after the object that lacks it",
			fields: "team(users(login))",
			body: `{"team":{"users":[{"name":"All Users","$type":"UserGroup"},{"login":"admin","$type":"User"}],"$type":"ProjectTeam"},` +
				`"$type":"Project"}`,
			stdout: "team:\n  users:\n    - {}\n    - {login: \"admin\"}\n",
		},
		{
			name:   "a field of an object of any schema where a schema declares a value of no schema",
			fields: "issues(customFields(value(login)))",
			body: `{"issues":[{"customFields":[{"value":{"minutes":90,"$type":"DurationValue"},"$type":"SimpleIssueCustomField"}],` +
				`"$type":"Issue"}],"$type":"Project"}`,
			stdout: "issues:\n  - {customFields: [{value: {}}]}\n",
		},
		{
			name:   "fields asked of a scalar where a schema declares a value of no schema",
			fields: "issues(customFields(value(name)))",
			body: `{"issues":[{"customFields":[{"value":7,"$type":"SimpleIssueCustomField"},{"value":{"name":"Open","$type":"StateBundleElement"},"$type":"StateIssueCustomField"},` +
				`{"value":{"minutes":90,"$type":"PeriodValue"},"$type":"PeriodIssueCustomField"}],"$type":"Issue"}],"$type":"Project"}`,
			stdout: "issues:\n  - {customFields: [{value: 7}, {value: {name: \"Open\"}}, {value: {}}]}\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", tc.fields)

			assert.Equal(t, outcome{stdout: tc.stdout}, got)
			assert.Len(t, server.requests(), 1)
		})
	}
}
