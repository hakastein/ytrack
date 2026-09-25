package cli_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/cli"
)

const defaultProjectFields = "shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))"

const projectDEV = `{"name":"DEVELOPMENT","plugins":{"timeTrackingSettings":{"workItemTypes":[` +
	`{"name":"Разработка","$type":"WorkItemType"},{"name":"Дизайн/Прототипирование","$type":"WorkItemType"}],` +
	`"enabled":true,"$type":"ProjectTimeTrackingSettings"},"$type":"ProjectPlugins"},"$type":"Project","shortName":"DEV"}`

const printedDEV = `shortName: "DEV"
name: "DEVELOPMENT"
plugins:
  timeTrackingSettings:
    enabled: true
    workItemTypes:
      - {name: "Разработка"}
      - {name: "Дизайн/Прототипирование"}
`

func authFromEnv() detail {
	return detail{"auth_from", "environment"}
}

func authFromSettings() detail {
	return detail{"auth_from", "settings"}
}

func lookedIn(places ...any) detail {
	return detail{"looked_in", places}
}

func TestProjectShowPrintsTheTimeTrackingSettingsAsReceived(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		settings string
		want     string
	}{
		{
			name: "types under a switch that is off",
			settings: `{"enabled":false,"workItemTypes":[{"name":"Разработка","$type":"WorkItemType"},` +
				`{"name":"ИИРазработка","$type":"WorkItemType"}],"$type":"ProjectTimeTrackingSettings"}`,
			want: "    enabled: false\n    workItemTypes:\n      - {name: \"Разработка\"}\n      - {name: \"ИИРазработка\"}\n",
		},
		{
			name:     "a set with nothing in it",
			settings: `{"enabled":true,"workItemTypes":[],"$type":"ProjectTimeTrackingSettings"}`,
			want:     "    enabled: true\n    workItemTypes: []\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"shortName":"DEV","name":"DEVELOPMENT","plugins":{"timeTrackingSettings":` + tc.settings +
				`,"$type":"ProjectPlugins"},"$type":"Project"}`
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "project", "show", "DEV")

			want := "shortName: \"DEV\"\nname: \"DEVELOPMENT\"\nplugins:\n  timeTrackingSettings:\n" + tc.want
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

func TestNoCommandOfItsOwnReadsTheTypesOfWork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a command of its own", argv: []string{"worktype", "list"}},
		{name: "a command named as the schema is", argv: []string{"work-item-type", "list"}},
		{name: "a subcommand of the project command", argv: []string{"project", "types", "DEV"}},
		{name: "a subcommand naming them as the settings do", argv: []string{"project", "worktypes", "DEV"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestProjectShowPrintsTheFieldsAskedInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, projectDEV))

	got := runWith(t, server.env(), "project", "show", "DEV")

	assert.Equal(t, outcome{stdout: printedDEV}, got)
	requests := server.requests()
	require.Len(t, requests, 1)
	request := requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "/api/admin/projects/DEV", request.URL.Path)
	assert.Equal(t, url.Values{"fields": {defaultProjectFields}}, request.URL.Query())
	assert.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
	assert.Equal(t, "application/json", request.Header.Get("Accept"))
}

func TestProjectShowPrintsNullAndBooleansBare(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, `{"shortName":"DEV","name":"DEVELOPMENT","archived":true,"leader":null,"$type":"Project"}`))

	got := runWith(t, server.env(), "project", "show", "DEV", "--fields", "shortName,name,archived,leader(login)")

	const want = `shortName: "DEV"
name: "DEVELOPMENT"
archived: true
leader: null
`
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestProjectShowTakesWhitespaceAroundTheAnswer(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, " \t\r\n"+projectDEV+" \t\r\n"))

	got := runWith(t, server.env(), "project", "show", "DEV")

	assert.Equal(t, outcome{stdout: printedDEV}, got)
}

func TestProjectShowSendsACodeOfLettersDigitsAndUnderscores(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, projectDEV))

	got := runWith(t, server.env(), "project", "show", "Проект_²")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	requests := server.requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "/api/admin/projects/Проект_²", requests[0].URL.Path)
}

func TestProjectShowReachesTheAPIUnderThePathOfTheAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "a slash", path: "/", want: "/api/admin/projects/DEV"},
		{name: "a path", path: "/ctx", want: "/ctx/api/admin/projects/DEV"},
		{name: "a path ending in a slash", path: "/ctx/", want: "/ctx/api/admin/projects/DEV"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, projectDEV))

			got := runWith(t, []string{"YTRACK_URL=" + server.url + tc.path, "YTRACK_TOKEN=" + token}, "project", "show", "DEV")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, tc.want, requests[0].URL.Path)
		})
	}
}

func TestProjectShowRefusesAnAddressItCannotUse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
	}{
		{name: "no scheme", address: "localhost:8091"},
		{name: "another scheme", address: "ftp://h"},
		{name: "no host", address: "http:///ctx"},
		{name: "a broken escape", address: "http://h/%zz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := []string{"YTRACK_URL=" + tc.address, "YTRACK_TOKEN=" + token}

			got := runWith(t, env, "project", "show", "DEV")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoToken(t, got, token)
		})
	}
}

func TestProjectShowRefusesAnAddressWithAQueryOrAFragment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tail string
	}{
		{name: "a query", tail: "/?q=1"},
		{name: "a fragment", tail: "/#top"},
		{name: "an empty query", tail: "/?"},
		{name: "an empty fragment", tail: "/#"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			address := server.url + tc.tail

			got := runWith(t, []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + token}, "project", "show", "DEV")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoToken(t, got, token)
		})
	}
}

func TestProjectShowRefusesWithoutAToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		token   []string
	}{
		{name: "no token", address: "http://127.0.0.1:%s"},
		{name: "an empty token", address: "http://127.0.0.1:%s", token: []string{"YTRACK_TOKEN="}},
		{name: "an address with a password", address: "http://svc:secret@127.0.0.1:%s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			listening, err := url.Parse(server.url)
			require.NoError(t, err)
			env := append([]string{"YTRACK_URL=" + fmt.Sprintf(tc.address, listening.Port())}, tc.token...)

			got := runWith(t, env, "project", "show", "DEV")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.NotContains(t, got.stderr, "secret")
			assertNoToken(t, got, token)
			assert.Empty(t, server.requests())
		})
	}
}

func TestProjectRefusesACallForTheFaultCheckedFirst(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		env  []string
		want faultDocument
	}{
		{
			name: "a code of another form, fields that do not parse, no address and no token",
			argv: []string{"project", "show", "a/b", "--fields", "a,,b"},
			want: faultDocument{code: "bad_usage"},
		},
		{
			name: "a limit it cannot send, fields that do not parse, no address and no token",
			argv: []string{"project", "list", "--limit", "0", "--fields", "a,,b"},
			want: faultDocument{code: "bad_usage"},
		},
		{
			name: "fields that do not parse, no address and no token",
			argv: []string{"project", "show", "DEV", "--fields", "a,,b"},
			want: faultDocument{code: "bad_usage"},
		},
		{
			name: "no address and no token",
			argv: []string{"project", "show", "DEV"},
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}},
		},
		{
			name: "an empty address and an empty token",
			argv: []string{"project", "show", "DEV"},
			env:  []string{"YTRACK_URL=", "YTRACK_TOKEN="},
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := runWith(t, tc.env, tc.argv...)

			assert.Equal(t, tc.want, requireFault(t, got))
			assertNoToken(t, got, token)
		})
	}
}

func TestProjectShowRefusesATokenItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		token string
	}{
		{name: "the carriage return of a Windows line ending", token: token + "\r"},
		{name: "a line feed", token: token + "\n"},
		{name: "a delete", token: "\x7f" + token},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, []string{"YTRACK_URL=" + server.url, "YTRACK_TOKEN=" + tc.token}, "project", "show", "DEV")

			want := faultDocument{code: "bad_usage"}
			assert.Equal(t, want, requireFault(t, got))
			assert.NotContains(t, got.stderr, token)
		})
	}
}

func TestProjectShowRefusesWithoutThePasswordOfTheAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		code    string
	}{
		{
			name:    "a refusal by the status of the answer",
			handler: respondWith(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id DEV not found"}`),
			code:    "not_found",
		},
		{
			name:    "a refusal of the judgment of names",
			handler: respondWith(http.StatusOK, `{"$type":"Project"}`),
			code:    "upstream_invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, tc.handler)
			address, err := url.Parse(server.url)
			require.NoError(t, err)
			address.User = url.UserPassword("svc", "secret")

			got := runWith(t, []string{"YTRACK_URL=" + address.String(), "YTRACK_TOKEN=" + token}, "project", "show", "DEV")

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			require.NotEmpty(t, found.details)
			assert.Equal(t, detail{"request", showRequest("http://svc:xxxxx@"+address.Host, "DEV")}, found.details[0])
			assert.NotContains(t, got.stderr, "secret")
		})
	}
}

func TestProjectShowRefusesWhenStdoutFails(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, projectDEV))
	var stderr strings.Builder

	code := cli.Run(t.Context(), []string{"project", "show", "DEV"}, server.env(), nil, nil, failingWriter{}, &stderr)

	got := outcome{code: code, stderr: stderr.String()}
	assert.Equal(t, faultDocument{code: "upstream_failed"}, requireFault(t, got))
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("stdout is closed")
}
