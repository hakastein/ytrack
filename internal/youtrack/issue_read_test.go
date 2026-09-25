package youtrack_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const issueReadPath = "/api/issues/DEV-1"

func issueReadShown(t *testing.T, server *fake.Server, expression string, comments youtrack.Comments) (*render.Node, *diag.Fault) {
	t.Helper()
	call, fault := youtrack.ShowIssue("DEV-1", &expression, comments)
	require.Nil(t, fault)
	return call(t.Context(), client(t, server))
}

func TestShowIssueRefusesAnExpressionBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "comments in place of the default", expression: "comments(text)"},
		{name: "comments added to the default", expression: "+comments"},
		{name: "comments under the issues of a link", expression: "links(issues(comments(id)))"},
		{name: "the id of a link slot", expression: "links(id)"},
		{name: "the end the issue stands at", expression: "links(direction)"},
		{name: "the type of a link", expression: "links(linkType(name))"},
		{name: "the id of the parent slot", expression: "parent(id)"},
		{name: "the trimmed issues of the subtasks slot", expression: "subtasks(trimmedIssues(idReadable))"},
		{name: "a slot under the issues of a link", expression: "links(issues(parent(id)))"},
		{name: "a name asked of a custom field", expression: "customFields(First(name))"},
		{name: "the members of the custom field block", expression: "customFields(name,value(name))"},
		{name: "a custom field named under the issues of a link", expression: "links(issues(customFields(First)))"},
		{name: "a name in quotes at the root", expression: `"First"`},
		{name: "a name in quotes under a link", expression: `links(issues("First"))`},
		{name: "a name in quotes of nothing", expression: `customFields("")`},
		{name: "a quote left open", expression: `customFields("First`},
		{name: "a backslash before anything but a quote or a backslash", expression: `customFields("a\b")`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, fault := youtrack.ShowIssue("DEV-1", &tc.expression, youtrack.AllComments())

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}

func TestListIssuesRefusesACallBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		query      string
		expression string
	}{
		{name: "a search that is no UTF-8", query: "\xff", expression: "idReadable"},
		{name: "a search holding a byte that is no UTF-8", query: "field: \xc3\x28", expression: "idReadable"},
		{name: "comments in place of the default", expression: "comments(text)"},
		{name: "comments added to the default", expression: "+comments"},
		{name: "a custom field named under the issues of a link", expression: "links(issues(customFields(First)))"},
		{name: "a part of a link other than its issues", expression: "+links(direction)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, fault := youtrack.ListIssues(tc.query, &tc.expression, youtrack.Page{Limit: 50}, func(*diag.Warning) {})

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}

func TestShowIssuePrintsTheFieldsInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","summary":"First"}`))

	node, fault := issueReadShown(t, server, "summary,idReadable", youtrack.Comments{})

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "summary", Value: render.NewString("First")},
		render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
	), node)
}

func TestShowIssuePrintsTheDescriptionAsText(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","description":"First\nSecond"}`))

	node, fault := issueReadShown(t, server, "description", youtrack.Comments{})

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(render.Pair{Key: "description", Value: render.NewText("First\nSecond")}), node)
}

func TestListIssuesPrintsTheTextOfARecordAsAString(t *testing.T) {
	t.Parallel()
	notes := issueReadCustomField("Notes", "null", "text", false, `{"$type":"TextFieldValue","text":"First\nSecond"}`)
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK,
		`[{"$type":"Issue","description":"First\nSecond","customFields":[`+notes+`]}]`)))

	node, _, fault := searchListing(t, server, "field: value", "description,customFields", 50)

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "total", Value: render.NewNumber("1")},
		render.Pair{Key: "returned", Value: render.NewNumber("1")},
		render.Pair{Key: "truncated", Value: render.NewBool(false)},
		render.Pair{Key: "issues", Value: render.NewList(render.NewMap(
			render.Pair{Key: "description", Value: render.NewString("First\nSecond")},
			render.Pair{Key: "customFields", Value: render.NewMap(render.FromData("Notes", render.NewString("First\nSecond")))},
		))},
	), node)
}

func TestShowIssuePrintsAMomentInUTC(t *testing.T) {
	t.Parallel()
	_, offset := time.Now().Zone()
	require.NotZero(t, offset, "the process runs in UTC, where a moment in its zone reads as one in UTC")
	tests := []struct {
		name     string
		received string
		printed  string
	}{
		{name: "the epoch itself", received: "0", printed: "1970-01-01T00:00:00Z"},
		{name: "a whole second", received: "1788134400000", printed: "2026-08-31T00:00:00Z"},
		{name: "a tenth of a second", received: "1788134400100", printed: "2026-08-31T00:00:00.1Z"},
		{name: "every millisecond", received: "1788134400123", printed: "2026-08-31T00:00:00.123Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","created":`+tc.received+`}`))

			node, fault := issueReadShown(t, server, "created", youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(render.Pair{Key: "created", Value: render.NewString(tc.printed)}), node)
		})
	}
}

func TestShowIssuePrintsOnlyATimeAsAMoment(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","created":0,"resolved":null,"numberInProject":1,`+
		`"attachments":[{"$type":"IssueAttachment","created":0,"size":75}]}`))

	node, fault := issueReadShown(t, server, "created,resolved,numberInProject,attachments(created,size)", youtrack.Comments{})

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "created", Value: render.NewString("1970-01-01T00:00:00Z")},
		render.Pair{Key: "resolved", Value: render.NewNull()},
		render.Pair{Key: "numberInProject", Value: render.NewNumber("1")},
		render.Pair{Key: "attachments", Value: render.NewList(render.NewMap(
			render.Pair{Key: "created", Value: render.NewString("1970-01-01T00:00:00Z")},
			render.Pair{Key: "size", Value: render.NewNumber("75")},
		))},
	), node)
}

func TestShowIssueRefusesAMomentThatIsNoWholeNumberOfMilliseconds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "a number inside a string", body: `{"$type":"Issue","created":"1788134400000"}`},
		{name: "a fraction of a millisecond", body: `{"$type":"Issue","created":1.5}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, fault := issueReadShown(t, server, "created", youtrack.Comments{})

			want := unreadable(requestTo(http.MethodGet, server, issueReadPath+"?fields=created"), tc.body)
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}
