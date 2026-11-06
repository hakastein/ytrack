package cli_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const limitedToken = "perm-ytrack-test-limited"

func withCommands(record string, commands ...string) string {
	listed, _ := json.Marshal(commands)
	return strings.TrimSuffix(record, "}") + `,"commands":` + string(listed) + "}"
}

// homeOfALogin holds the global login given and four scripts of the user: hello, greet, authors and team hello.
func homeOfALogin(t *testing.T, record string) string {
	t.Helper()
	home := scriptsHome(t, map[string]string{"hello.js": greeting, "greet.js": greeting, "authors.js": greeting, "team/hello.js": greeting})
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ytrack", "auth.json"), []byte(recordFile(record)), 0o600))
	return home
}

func limitedTo(t *testing.T, server *fake.Server, commands ...string) []string {
	t.Helper()
	return []string{"HOME=" + homeOfALogin(t, withCommands(unscopedRecord(server.URL, limitedToken), commands...))}
}

func TestLimitedLoginHasNoCommandOutsideItsList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a builtin command beside a listed one", argv: []string{"issue", "list"}},
		{name: "a builtin command under another word", argv: []string{"article", "show", "DEV-A-1"}},
		{name: "a script of the user", argv: []string{"greet"}},
		{name: "the completion command", argv: []string{"completion", "bash"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, limitedTo(t, fake.ServeNothing(t), "issue show", "hello"), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestLimitedLoginOffersOnlyTheListedCommandsAndAuth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		words []string
		names []string
	}{
		{name: "the root", words: []string{""}, names: []string{"auth", "hello", "issue"}},
		{name: "a command listed by a path under it", words: []string{"issue", ""}, names: []string{"show"}},
		{name: "auth", words: []string{"auth", ""}, names: []string{"login", "logout", "status"}},
		{name: "a word typed as far as auth", words: []string{"auth"}, names: []string{"auth"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := completingWith(t, limitedTo(t, fake.ServeNothing(t), "issue show", "hello"), tc.words...)

			assert.Equal(t, tc.names, got.names())
		})
	}
}

func TestLimitedLoginHelpListsNoScriptOutsideTheList(t *testing.T) {
	t.Parallel()
	got := runWith(t, limitedTo(t, fake.ServeNothing(t), "hello"), "--help")

	assert.Equal(t, 0, got.code)
	assert.Contains(t, got.stdout, "hello")
	assert.NotContains(t, got.stdout, "greet")
}

func TestLimitedLoginRunsAListedCommand(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","comments":[]}`))

	got := runWith(t, limitedTo(t, server, "issue show"), "issue", "show", "DEV-1", "--fields", "idReadable")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, bearing(limitedToken), server.Last(t).Header.Get("Authorization"))
}

func TestLimitedLoginRunsAListedScript(t *testing.T) {
	t.Parallel()
	got := runWith(t, limitedTo(t, fake.ServeNothing(t), "issue show", "hello"), "hello")

	assert.Equal(t, outcome{stdout: greeted}, got)
}

func TestAuthWorksUnderALimitedLogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		commands []string
	}{
		{name: "a list without auth", commands: []string{"issue show"}},
		{name: "a list naming no command", commands: []string{"issue close"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveUserOfTheToken(t, map[string]string{limitedToken: recordUser})

			got := runWith(t, limitedTo(t, server, tc.commands...), "auth", "status")

			assert.Equal(t, 0, got.code)
		})
	}
}

func TestLoginThatListsNoCommandsLimitsNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  func(t *testing.T) []string
	}{
		{name: "a login from the environment beside a limited record", env: func(t *testing.T) []string {
			t.Helper()
			nothing := fake.ServeNothing(t)
			return append(envOf(nothing), limitedTo(t, nothing, "hello")...)
		}},
		{name: "a saved login without commands", env: func(t *testing.T) []string {
			t.Helper()
			return []string{"HOME=" + homeOfALogin(t, unscopedRecord(fake.ServeNothing(t).URL, limitedToken))}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, tc.env(t), "greet")

			assert.Equal(t, outcome{stdout: greeted}, got)
		})
	}
}

func TestLimitedLoginNamingNoCommandIsRefusedBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a listed command", argv: []string{"issue", "show", "DEV-1"}},
		{name: "help", argv: []string{"--help"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, limitedTo(t, fake.ServeNothing(t), "issue show", "issue close"), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestLimitedLoginListingACommandWithSubcommandsIsRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		listed  string
		calling []string
	}{
		{name: "a builtin command", listed: "issue", calling: []string{"issue", "show", "DEV-1"}},
		{name: "a directory of scripts", listed: "team", calling: []string{"team", "hello"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, limitedTo(t, fake.ServeNothing(t), "hello", tc.listed), tc.calling...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestAuthLogoutKeepsTheCommandsOfTheRecordsLeft(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)
	limited := withCommands(scopedRecord(here(t), server.URL, hereToken), "issue show")
	home, path := homeWith(t, recordFile(limited, unscopedRecord(server.URL, everywhereToken)))

	got := runWith(t, []string{"HOME=" + home}, "auth", "logout", "--global")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, savedFile(limited), fileBytes(t, path))
}
