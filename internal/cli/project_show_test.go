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
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/cli"
)

// The fields= of project show where the caller wrote no --fields of their own.
const defaultProjectFields = "shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))"

// DEV under the default expression, $type added and the keys in an order other than asked: the server keeps an
// order of its own. The types of work stand under the settings of the project's time tracking, a place the
// specification declares nothing of and the server answers from all the same.
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

// DEV of the polygon under the default: the fifteen types of work its time tracking is set up with, in the
// order the server keeps them in.
const printedDevProject = `shortName: "DEV"
name: "DEVELOPMENT"
plugins:
  timeTrackingSettings:
    enabled: true
    workItemTypes:
      - {name: "Разработка"}
      - {name: "Тестирование"}
      - {name: "Документирование"}
      - {name: "Исследование"}
      - {name: "Груминг"}
      - {name: "Декомпозиция"}
      - {name: "Кодревью"}
      - {name: "ПланированиеРетро"}
      - {name: "ТехОкружение"}
      - {name: "Коммуникации"}
      - {name: "Дизайн/Прототипирование"}
      - {name: "Написание ТЗ"}
      - {name: "Написание инструкции"}
      - {name: "Проектирование"}
      - {name: "Уточнение требований"}
`

// Where a refusal says the login came from: the whole pair, never which variable or which record of it.
func authFromEnv() detail {
	return detail{"auth_from", "environment"}
}

func authFromSettings() detail {
	return detail{"auth_from", "settings"}
}

func lookedIn(places ...any) detail {
	return detail{"looked_in", places}
}

// The default reaches the polygon in one request and prints DEV with the types of work its time tracking
// is set up with.
func TestProjectShowPrintsDEVOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV")

	assert.Equal(t, outcome{stdout: printedDevProject}, got)
	assert.Equal(t, []string{defaultProjectFields}, dev.sentFields())
	assert.Len(t, dev.requests(), 1)
}

// The types of work are settings of a project's time tracking and nothing among its custom fields:
// the set DEV of the polygon carries holds neither the name of a field, nor the name a project gives one, nor
// the name of a value of any bundle, letter case aside. Two types the instance-wide catalogue has and DEV is
// not set up with are absent from the set as well, so the set is the project's own.
func TestProjectShowKeepsTheTypesOfWorkOutOfTheCustomFieldsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields",
		"plugins(timeTrackingSettings(workItemTypes(name))),customFields(field(name,localizedName),bundle(values(name)))")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	var printed struct {
		Plugins struct {
			TimeTrackingSettings struct {
				WorkItemTypes []struct {
					Name string `yaml:"name"`
				} `yaml:"workItemTypes"`
			} `yaml:"timeTrackingSettings"`
		} `yaml:"plugins"`
		CustomFields []struct {
			Field struct {
				Name string `yaml:"name"`
				// The name the project gives the field, which a caller may write instead of the name itself.
				LocalizedName string `yaml:"localizedName"`
			} `yaml:"field"`
			Bundle struct {
				Values []struct {
					Name string `yaml:"name"`
				} `yaml:"values"`
			} `yaml:"bundle"`
		} `yaml:"customFields"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(got.stdout), &printed))
	var types []string
	for _, workItemType := range printed.Plugins.TimeTrackingSettings.WorkItemTypes {
		types = append(types, workItemType.Name)
	}
	assert.Equal(t, []string{
		"Разработка", "Тестирование", "Документирование", "Исследование", "Груминг", "Декомпозиция", "Кодревью",
		"ПланированиеРетро", "ТехОкружение", "Коммуникации", "Дизайн/Прототипирование", "Написание ТЗ",
		"Написание инструкции", "Проектирование", "Уточнение требований",
	}, types)
	assert.NotContains(t, types, "ИИРазработка")
	assert.NotContains(t, types, "Реализация")
	var names, values []string
	for _, item := range printed.CustomFields {
		names = append(names, item.Field.Name)
		if item.Field.LocalizedName != "" {
			names = append(names, item.Field.LocalizedName)
		}
		for _, value := range item.Bundle.Values {
			values = append(values, value.Name)
		}
	}
	assert.Len(t, printed.CustomFields, 28)
	// The names the types are held against, not the fields: six of the twenty-eight carry a localized name of
	// the project as well, and it is a name a caller may write. Counted here so that a set that shrinks — a
	// localizedName out of the expression, or one a token is not sent — cannot leave the scenario green.
	assert.Len(t, names, 34)
	assert.Len(t, values, 92)
	taken := map[string]bool{}
	for _, name := range append(names, values...) {
		taken[strings.ToLower(name)] = true
	}
	var shared []string
	for _, name := range types {
		if taken[strings.ToLower(name)] {
			shared = append(shared, name)
		}
	}
	assert.Empty(t, shared)
	assert.Len(t, dev.requests(), 1)
}

// The switch stands in the default because a project that has time tracking off carries its set of types
// all the same: DOCS of the polygon is off and lists sixteen.
func TestProjectShowPrintsTheProjectOfTheDevInstanceThatHasTimeTrackingOff(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DOCS")

	const want = `shortName: "DOCS"
name: "DOCS"
plugins:
  timeTrackingSettings:
    enabled: false
    workItemTypes:
      - {name: "Разработка"}
      - {name: "Тестирование"}
      - {name: "Документирование"}
      - {name: "Исследование"}
      - {name: "Груминг"}
      - {name: "Декомпозиция"}
      - {name: "Кодревью"}
      - {name: "ПланированиеРетро"}
      - {name: "ТехОкружение"}
      - {name: "Коммуникации"}
      - {name: "Дизайн/Прототипирование"}
      - {name: "Написание ТЗ"}
      - {name: "Написание инструкции"}
      - {name: "Проектирование"}
      - {name: "Уточнение требований"}
      - {name: "ИИРазработка"}
`
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Len(t, dev.requests(), 1)
}

// The switch is printed as it arrived rather than standing in for the set: a project that has time
// tracking off still lists its types, and a set the server sends empty is printed empty.
func TestProjectShowPrintsTheTimeTrackingSettingsAsTheyArrived(t *testing.T) {
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
			server := serve(t, answer(http.StatusOK, body))

			got := runWith(t, server.env(), "project", "show", "DEV")

			want := "shortName: \"DEV\"\nname: \"DEVELOPMENT\"\nplugins:\n  timeTrackingSettings:\n" + tc.want
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

// The types of work are data of a project and nothing ytrack gives a noun or a verb of its own: a caller
// who looks for one is refused where any other unknown command is, before the network.
func TestNoCommandOfItsOwnReadsTheTypesOfWork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a group of its own", argv: []string{"worktype", "list"}},
		{name: "a group named as the schema is", argv: []string{"work-item-type", "list"}},
		{name: "a verb of the project group", argv: []string{"project", "types", "DEV"}},
		{name: "a verb naming them as the settings do", argv: []string{"project", "worktypes", "DEV"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The group offers the two verbs it did before, and the types of work came with neither a third nor a
// flag: they are printed by the show that was already there.
func TestProjectHelpNamesTwoVerbs(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"project", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"list", "show"}, availableCommands(t, got.stdout))
}

func TestProjectShowPrintsTheFieldsTheMemberAsksFor(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "project", "show", "DEV", "--fields", "shortName,name")

	assert.Equal(t, outcome{stdout: "shortName: \"DEV\"\nname: \"DEVELOPMENT\"\n"}, got)
	assert.Len(t, dev.requests(), 1)
}

// The settings of a project's time tracking reach a member of it with no rights in its administration, so the
// default is one document whoever reads the project.
func TestProjectShowPrintsTheDefaultToTheMember(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "project", "show", "DEV")

	assert.Equal(t, outcome{stdout: printedDevProject}, got)
	assert.Len(t, dev.requests(), 1)
}

func TestProjectShowRefusesAFieldHiddenFromTheMember(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}, "project", "show", "DEV", "--fields", "+archived")

	want := refusal{
		code:    "upstream_lied",
		details: judgmentDetails(dev.url, defaultProjectFields+",archived", "missing", missingEntry("archived", "Project")),
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, dev.requests(), 1)
}

func TestProjectShowPrintsTheFieldsAskedInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, projectDEV))

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
	server := serve(t, answer(http.StatusOK, `{"shortName":"DEV","name":"DEVELOPMENT","archived":true,"leader":null,"$type":"Project"}`))

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
	server := serve(t, answer(http.StatusOK, " \t\r\n"+projectDEV+" \t\r\n"))

	got := runWith(t, server.env(), "project", "show", "DEV")

	assert.Equal(t, outcome{stdout: printedDEV}, got)
}

func TestProjectShowSendsACodeOfLettersDigitsAndUnderscores(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, projectDEV))

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
			server := serve(t, answer(http.StatusOK, projectDEV))

			got := runWith(t, []string{"YTRACK_URL=" + server.url + tc.path, "YTRACK_TOKEN=" + token}, "project", "show", "DEV")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, tc.want, requests[0].URL.Path)
		})
	}
}

func TestProjectShowTakesExactlyOneCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no code", argv: []string{"project", "show"}},
		{name: "two codes", argv: []string{"project", "show", "DEV", "DEMO"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assertNoToken(t, got, token)
		})
	}
}

// Were it not refused, each address would be sent, so it names a server that fails the test on any request.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assertNoToken(t, got, token)
		})
	}
}

// An address without a token is half a login, and a refusal that prints no address prints no password with it.
func TestProjectShowRefusesWithoutAToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// A format of the port the server listens on.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
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
		want refusal
	}{
		{
			name: "a code of another form, fields that do not parse, no address and no token",
			argv: []string{"project", "show", "a/b", "--fields", "a,,b"},
			want: refusal{code: "bad_usage"},
		},
		{
			name: "a limit it cannot send, fields that do not parse, no address and no token",
			argv: []string{"project", "list", "--limit", "0", "--fields", "a,,b"},
			want: refusal{code: "bad_usage"},
		},
		{
			name: "fields that do not parse, no address and no token",
			argv: []string{"project", "show", "DEV", "--fields", "a,,b"},
			want: refusal{code: "bad_usage"},
		},
		{
			name: "no address and no token",
			argv: []string{"project", "show", "DEV"},
			want: refusal{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}},
		},
		{
			name: "an empty address and an empty token",
			argv: []string{"project", "show", "DEV"},
			env:  []string{"YTRACK_URL=", "YTRACK_TOKEN="},
			want: refusal{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := runWith(t, tc.env, tc.argv...)

			assert.Equal(t, tc.want, requireRefusal(t, got))
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

			want := refusal{code: "bad_usage"}
			assert.Equal(t, want, requireRefusal(t, got))
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
			handler: answer(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id DEV not found"}`),
			code:    "not_found",
		},
		{
			name:    "a refusal of the judgment of names",
			handler: answer(http.StatusOK, `{"$type":"Project"}`),
			code:    "upstream_lied",
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

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			require.NotEmpty(t, found.details)
			assert.Equal(t, detail{"request", showRequest("http://svc:xxxxx@"+address.Host, "DEV")}, found.details[0])
			assert.NotContains(t, got.stderr, "secret")
		})
	}
}

func TestProjectShowRefusesWhenStdoutFails(t *testing.T) {
	t.Parallel()
	server := serve(t, answer(http.StatusOK, projectDEV))
	var stderr strings.Builder

	code := cli.Run(t.Context(), []string{"project", "show", "DEV"}, server.env(), nil, nil, failingWriter{}, &stderr)

	got := outcome{code: code, stderr: stderr.String()}
	assert.Equal(t, refusal{code: "upstream_failed"}, requireRefusal(t, got))
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("stdout is closed")
}
