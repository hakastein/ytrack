package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestShowCommandsPrintTheirDefaultFieldsInTheOrderTheyAsk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		answer  string
		argv    []string
		printed string
		target  string
	}{
		{
			name:    "article show",
			answer:  articleWithAChild(),
			argv:    []string{"article", "show", "DEV-A-1"},
			printed: printedArticleWithAChild,
			target:  "/api/articles/DEV-A-1?fields=" + sentArticleFields,
		},
		{
			name:    "project show",
			answer:  projectDEV,
			argv:    []string{"project", "show", "DEV"},
			printed: printedDEV,
			target:  "/api/admin/projects/DEV?fields=shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))",
		},
		{
			name:    "user show",
			answer:  shownUser,
			argv:    []string{"user", "show", "first"},
			printed: printedUser,
			target:  "/api/users/first?fields=login,fullName,email,banned",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, outcome{stdout: tc.printed}, got)
			assert.Equal(t, []string{http.MethodGet}, server.Methods())
			assert.Equal(t, []string{tc.target}, server.Targets(t))
		})
	}
}

func TestProjectShowSendsTheTokenAndAsksForJSON(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectDEV))

	runWith(t, server.Env(), "project", "show", "DEV")

	assert.Equal(t, "Bearer "+fake.Token, server.Request(t, 0).Header.Get("Authorization"))
	assert.Equal(t, "application/json", server.Request(t, 0).Header.Get("Accept"))
}

func TestListsRefuseAnAnswerThatIsNoList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		answer string
		argv   []string
		sent   func(address string) string
	}{
		{
			name:   "project list",
			answer: listedDEV,
			argv:   []string{"project", "list", "--fields", "shortName"},
			sent:   func(address string) string { return listRequest(address, "shortName", "50") },
		},
		{
			name:   "time list",
			answer: listedWorkItem,
			argv:   []string{"time", "list", "DEV-1", "--fields", "id"},
			sent: func(address string) string {
				return "GET " + address + workItemsPath("DEV-1") + "?fields=id&$top=50"
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))

			got := runWith(t, server.Env(), tc.argv...)

			want := faultDocument{code: "upstream_invalid", details: []detail{
				{"request", tc.sent(server.URL)},
				{"upstream_status", 200},
				{"upstream_body", tc.answer},
			}}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
}
