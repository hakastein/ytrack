package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const fieldcheckAsked = "shortName,name,archived,leader(login)"

func fieldcheckFault(server *fake.Server, code diag.Code, target, expression, key string, entries ...*render.Node) diag.Fault {
	return diag.Fault{Code: code, Details: []render.Pair{
		requestTo(http.MethodGet, server, target+"?fields="+expression),
		{Key: "fields", Value: render.NewString(expression)},
		{Key: key, Value: render.NewList(entries...)},
	}}
}

func fieldcheckMissing(field string, schema *render.Node) *render.Node {
	return render.NewMap(render.Pair{Key: "field", Value: render.NewString(field)}, render.Pair{Key: "type", Value: schema})
}

func fieldcheckProjectNames() []string {
	return []string{"$type", "archived", "createdBy", "customFields", "description", "fromEmail", "iconUrl", "id",
		"issues", "leader", "name", "replyToEmail", "shortName", "startingNumber", "team", "template"}
}

func fieldcheckUserNames() []string {
	return []string{"$type", "avatarUrl", "banned", "email", "fullName", "guest", "id", "isAnonymized", "login", "name",
		"online", "profiles", "ringId", "savedQueries", "tags"}
}

func fieldcheckEmpty() *render.Node {
	return render.NewMap([]render.Pair{}...)
}

func fieldcheckName(name string) *render.Node {
	return render.NewMap(render.Pair{Key: "name", Value: render.NewString(name)})
}

func TestShowProjectRefusesAFieldMissingFromTheAnswer(t *testing.T) {
	t.Parallel()
	named := render.NewString
	tests := []struct {
		name       string
		expression string
		body       string
		missing    []*render.Node
	}{
		{
			name:       "a field the named type declares",
			expression: fieldcheckAsked,
			body:       `{"leader":{"login":"leader","$type":"User"},"shortName":"DEV","archived":false,"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("name", named("Project"))},
		},
		{
			name:       "a field of an object that names no type",
			expression: fieldcheckAsked,
			body:       `{"leader":{"login":"leader","$type":"User"},"shortName":"DEV","archived":false}`,
			missing:    []*render.Node{fieldcheckMissing("name", render.NewNull())},
		},
		{
			name:       "a field of an object of a type the specification does not have",
			expression: fieldcheckAsked,
			body:       `{"leader":{"login":"leader","$type":"User"},"shortName":"DEV","archived":false,"$type":"Unknown"}`,
			missing:    []*render.Node{fieldcheckMissing("name", named("Unknown"))},
		},
		{
			name:       "a nested field the named type declares",
			expression: fieldcheckAsked,
			body:       `{"leader":{"$type":"User"},"shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("leader(login)", named("User"))},
		},
		{
			name:       "fields asked of a string where the schema declares an object",
			expression: fieldcheckAsked,
			body:       `{"leader":"leader","shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("leader(login)", render.NewNull())},
		},
		{
			name:       "fields asked of a number where the schema declares an object",
			expression: fieldcheckAsked,
			body:       `{"leader":7,"shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("leader(login)", render.NewNull())},
		},
		{
			name:       "a field of an item of a list",
			expression: "issues(idReadable)",
			body:       `{"issues":[{"idReadable":"DEV-1","$type":"Issue"},{"$type":"Issue"}],"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("issues(idReadable)", named("Issue"))},
		},
		{
			name:       "fields asked of a string in a list of objects",
			expression: "issues(idReadable)",
			body:       `{"issues":["DEV-1"],"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("issues(idReadable)", render.NewNull())},
		},
		{
			name:       "a field absent from every item of a list, once",
			expression: "issues(idReadable,summary)",
			body:       `{"issues":[{"summary":"First","$type":"Issue"},{"summary":"Second","$type":"Issue"}],"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("issues(idReadable)", named("Issue"))},
		},
		{
			name:       "a field beside a name no schema declares",
			expression: "shortName,bogus,name",
			body:       `{"shortName":"DEV","$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("name", named("Project"))},
		},
		{
			name:       "a field under a field of no schema, by the type named there",
			expression: "customFields(field(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"$type":"StateProjectCustomField"},` +
				`{"$type":"EnumProjectCustomField"}],"$type":"Project"}`,
			missing: []*render.Node{fieldcheckMissing("customFields(field)", named("EnumProjectCustomField"))},
		},
		{
			name:       "every field of an object of a schema that may not stand at the root",
			expression: fieldcheckAsked,
			body:       `{"login":"leader","$type":"User"}`,
			missing: []*render.Node{
				fieldcheckMissing("shortName", named("User")), fieldcheckMissing("name", named("User")),
				fieldcheckMissing("archived", named("User")), fieldcheckMissing("leader", named("User")),
			},
		},
		{
			name:       "a field of an object of a schema that may not stand under its field",
			expression: fieldcheckAsked,
			body:       `{"leader":{"shortName":"X","$type":"Project"},"shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*render.Node{fieldcheckMissing("leader(login)", named("Project"))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, fault := expressionShown(t, server, tc.expression)

			want := fieldcheckFault(server, diag.UpstreamInvalid, "/api/admin/projects/DEV", tc.expression, "missing", tc.missing...)
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestShowProjectRefusesANameNoSchemaOfItsNodeDeclares(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		body       string
		unknown    *render.Node
	}{
		{
			name:       "the nearest name first",
			expression: "tam",
			body:       `{"$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "tam", "team", "name"),
		},
		{
			name:       "letter case aside",
			expression: "leader(LOGIN)",
			body:       `{"leader":{"login":"leader","$type":"User"},"$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "leader(LOGIN)", "login"),
		},
		{
			name:       "a name absent from every item of a list, once",
			expression: "issues(summery)",
			body:       `{"issues":[{"$type":"Issue"},{"$type":"Issue"}],"$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "issues(summery)", "summary"),
		},
		{
			name:       "under a field of no schema, by the types named there",
			expression: "customFields(bundel)",
			body:       `{"customFields":[{"$type":"EnumProjectCustomField"},{"$type":"PeriodProjectCustomField"}],"$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "customFields(bundel)", "bundle"),
		},
		{
			name:       "at the root, a name only another schema of the hierarchy of the answer declares",
			expression: "owner",
			body:       `{"$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "owner", fieldcheckProjectNames()...),
		},
		{
			name:       "a name only the schema of an object that may not stand at its place declares",
			expression: "leader(shortName)",
			body:       `{"leader":{"$type":"Project"},"$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "leader(shortName)", fieldcheckUserNames()...),
		},
		{
			name:       "under a field the schema above does not declare, by the type named there",
			expression: "team(users(logn))",
			body:       `{"team":{"users":[{"login":"leader","$type":"User"}],"$type":"ProjectTeam"},"$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "team(users(logn))", "login"),
		},
		{
			name:       "a name asked of a scalar",
			expression: "shortName(bogus)",
			body:       `{"shortName":"DEV","$type":"Project"}`,
			unknown:    issueReadNearest("nearest", "shortName(bogus)"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, fault := expressionShown(t, server, tc.expression)

			want := fieldcheckFault(server, diag.UnknownName, "/api/admin/projects/DEV", tc.expression, "unknown", tc.unknown)
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestShowUserRefusesANameNoSchemaOfItsNodeDeclares(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"login":"leader","$type":"User"}`))
	call, fault := youtrack.ShowUser("leader", "login,logn")
	require.Nil(t, fault)

	_, fault = call(t.Context(), client(t, server))

	want := fieldcheckFault(server, diag.UnknownName, "/api/users/leader", "login,logn", "unknown",
		issueReadNearest("nearest", "logn", "login"))
	assert.Equal(t, want, faultOf(t, fault))
}

func TestShowProjectLeavesOutAFieldTheNamedTypeDoesNotDeclare(t *testing.T) {
	t.Parallel()
	listed := func(key string, items ...*render.Node) *render.Node {
		return render.NewMap(render.Pair{Key: key, Value: render.NewList(items...)})
	}
	field := func(name string) render.Pair { return render.Pair{Key: "field", Value: fieldcheckName(name)} }
	bundle := render.Pair{Key: "bundle", Value: fieldcheckName("Bundle")}
	tests := []struct {
		name       string
		expression string
		body       string
		printed    *render.Node
	}{
		{
			name:       "names asked of an empty list",
			expression: "issues(bogus)",
			body:       `{"issues":[],"$type":"Project"}`,
			printed:    listed("issues", []*render.Node{}...),
		},
		{
			name:       "a name asked of a null",
			expression: "createdBy(bogus)",
			body:       `{"createdBy":null,"$type":"Project"}`,
			printed:    render.NewMap(render.Pair{Key: "createdBy", Value: render.NewNull()}),
		},
		{
			name:       "a field of another schema under a field of no schema",
			expression: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"bundle":{"name":"Bundle","$type":"EnumBundle"},` +
				`"$type":"EnumProjectCustomField"},{"field":{"name":"Second","$type":"CustomField"},"$type":"PeriodProjectCustomField"}],` +
				`"$type":"Project"}`,
			printed: listed("customFields", render.NewMap(field("First"), bundle), render.NewMap(field("Second"))),
		},
		{
			name:       "a field of no schema whose one type lacks a field its siblings declare",
			expression: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"$type":"PeriodProjectCustomField"},` +
				`{"field":{"name":"Second","$type":"CustomField"},"$type":"PeriodProjectCustomField"}],"$type":"Project"}`,
			printed: listed("customFields", render.NewMap(field("First")), render.NewMap(field("Second"))),
		},
		{
			name:       "a field of no schema whose first type lacks a field a type after it declares",
			expression: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"$type":"PeriodProjectCustomField"},` +
				`{"field":{"name":"Second","$type":"CustomField"},"bundle":{"name":"Bundle","$type":"EnumBundle"},` +
				`"$type":"EnumProjectCustomField"}],"$type":"Project"}`,
			printed: listed("customFields", render.NewMap(field("First")), render.NewMap(field("Second"), bundle)),
		},
		{
			name:       "a field of no schema that another schema of the hierarchy named there declares",
			expression: "customFields(owner)",
			body:       `{"customFields":[{"$type":"Project"}],"$type":"Project"}`,
			printed:    listed("customFields", fieldcheckEmpty()),
		},
		{
			name:       "under a field no schema declares, a field of a type named after the object that lacks it",
			expression: "team(users(login))",
			body: `{"team":{"users":[{"name":"Group","$type":"UserGroup"},{"login":"leader","$type":"User"}],` +
				`"$type":"ProjectTeam"},"$type":"Project"}`,
			printed: render.NewMap(render.Pair{Key: "team", Value: listed("users", fieldcheckEmpty(),
				render.NewMap(render.Pair{Key: "login", Value: render.NewString("leader")}))}),
		},
		{
			name:       "a field of an object of any schema where a schema declares a value of no schema",
			expression: "issues(customFields(value(login)))",
			body: `{"issues":[{"customFields":[{"value":{"minutes":90,"$type":"DurationValue"},` +
				`"$type":"SimpleIssueCustomField"}],"$type":"Issue"}],"$type":"Project"}`,
			printed: listed("issues", listed("customFields", render.NewMap(render.Pair{Key: "value", Value: fieldcheckEmpty()}))),
		},
		{
			name:       "fields asked of a scalar where a schema declares a value of no schema",
			expression: "issues(customFields(value(name)))",
			body: `{"issues":[{"customFields":[{"value":7,"$type":"SimpleIssueCustomField"},` +
				`{"value":{"name":"Open","$type":"StateBundleElement"},"$type":"StateIssueCustomField"},` +
				`{"value":{"minutes":90,"$type":"PeriodValue"},"$type":"PeriodIssueCustomField"}],"$type":"Issue"}],"$type":"Project"}`,
			printed: listed("issues", listed("customFields",
				render.NewMap(render.Pair{Key: "value", Value: render.NewNumber("7")}),
				render.NewMap(render.Pair{Key: "value", Value: fieldcheckName("Open")}),
				render.NewMap(render.Pair{Key: "value", Value: fieldcheckEmpty()}),
			)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			node, fault := expressionShown(t, server, tc.expression)

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
		})
	}
}
