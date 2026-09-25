package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const expressionProject = `{"$type":"Project","shortName":"DEV","name":"First","description":null,` +
	`"leader":{"$type":"User","login":"leader","fullName":"Leader"},` +
	`"team":{"$type":"ProjectTeam","name":"Team","users":[{"$type":"User","login":"leader","fullName":"Leader"}]},` +
	`"plugins":{"timeTrackingSettings":{"enabled":true,"workItemTypes":[]}}}`

func expressionShown(t *testing.T, server *fake.Server, expression string) (*render.Node, *diag.Fault) {
	t.Helper()
	call, fault := youtrack.ShowProject("DEV", expression)
	require.Nil(t, fault)
	return call(t.Context(), client(t, server))
}

func TestShowProjectRefusesAnExpressionItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "nothing", expression: ""},
		{name: "a plus alone", expression: "+"},
		{name: "a comma at the end", expression: "a,"},
		{name: "a comma at the start", expression: ",a"},
		{name: "two commas", expression: "a,,b"},
		{name: "empty parentheses", expression: "a()"},
		{name: "an unclosed parenthesis", expression: "a(b"},
		{name: "an unopened parenthesis", expression: "a)"},
		{name: "a space between names", expression: "a b"},
		{name: "a plus inside", expression: "+a+b"},
		{name: "a name outside ASCII", expression: "поле"},
		{name: "tabs before a closing parenthesis", expression: "a,\t\t)"},
		{name: "a name in quotes where no custom field is named", expression: `"name"`},
		{name: "a word read as a bool", expression: "on"},
		{name: "a word read as a bool, letter case aside", expression: "leader(login,No)"},
		{name: "a word read as null, added to the default", expression: "+null"},
		{name: "a leading digit", expression: "1abc"},
		{name: "the content of a file", expression: "issues(attachments(base64Content))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, fault := youtrack.ShowProject("DEV", tc.expression)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}

func TestShowIssueRefusesTheContentOfAFileOutsideQuotes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "under the attachments", expression: "attachments(base64Content)"},
		{name: "as a custom field", expression: "customFields(base64Content)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, fault := youtrack.ShowIssue("DEV-1", &tc.expression, youtrack.Comments{})

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}

func TestShowIssueTakesAQuotedBase64ContentForACustomField(t *testing.T) {
	t.Parallel()

	_, fault := youtrack.ShowIssue("DEV-1", new(`customFields("base64Content")`), youtrack.Comments{})

	assert.Nil(t, fault)
}

func TestShowProjectSendsEachFieldOnceInOneForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		sent       string
	}{
		{name: "spaces and tabs around names and punctuation", expression: " \tname\t, leader ( login ) ", sent: "name,leader(login)"},
		{name: "a name given again keeps its first place", expression: "name,shortName,name", sent: "name,shortName"},
		{name: "a name given bare before its fields", expression: "leader,leader(login)", sent: "leader(login)"},
		{
			name:       "fields merged at depth",
			expression: "team(users(login)),name,team(users(fullName),name)",
			sent:       "team(users(login,fullName),name),name",
		},
		{name: "a new name added to the default", expression: "+description", sent: youtrack.ProjectShowFields + ",description"},
		{name: "a name of the default added to it", expression: "+name,description", sent: youtrack.ProjectShowFields + ",description"},
		{name: "spaces around the plus", expression: " + description", sent: youtrack.ProjectShowFields + ",description"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, expressionProject))

			_, _ = expressionShown(t, server, tc.expression)

			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestShowProjectPrintsWhatArrivedAsItArrived(t *testing.T) {
	t.Parallel()
	const pastFloat64Precision = "9007199254740993"
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","name":"First","archived":true,`+
		`"startingNumber":`+pastFloat64Precision+`,"issues":[],"leader":null}`))

	node, fault := expressionShown(t, server, "$type,name,archived,startingNumber,issues(idReadable),leader(login)")

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "$type", Value: render.NewString("Project")},
		render.Pair{Key: "name", Value: render.NewString("First")},
		render.Pair{Key: "archived", Value: render.NewBool(true)},
		render.Pair{Key: "startingNumber", Value: render.NewNumber(pastFloat64Precision)},
		render.Pair{Key: "issues", Value: render.NewList([]*render.Node{}...)},
		render.Pair{Key: "leader", Value: render.NewNull()},
	), node)
}
