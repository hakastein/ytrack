package cli_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sentWorkItemTypesFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name))))"

const sentWorkItemWriteFieldsWithType = "id,duration(minutes),type(name,id),attributes(id,name,value(id,name)),author(login),date," +
	"issue(idReadable," + customFieldsFields + "),text"

func devWorkItemTypes() []string {
	return []string{
		"Разработка", "Тестирование", "Документирование", "Исследование", "Груминг", "Декомпозиция", "Кодревью",
		"ПланированиеРетро", "ТехОкружение", "Коммуникации", "Дизайн/Прототипирование", "Написание ТЗ",
		"Написание инструкции", "Проектирование", "Уточнение требований",
	}
}

func workItemTypeID(at int) string {
	return "178-" + strconv.Itoa(at)
}

func issueWithWorkItemTypes(readable, project string, types ...string) string {
	items := make([]string, 0, len(types))
	for at, name := range types {
		items = append(items, `{"$type":"WorkItemType","id":`+strconv.Quote(workItemTypeID(at))+
			`,"name":`+strconv.Quote(name)+`}`)
	}
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":` + strconv.Quote(project) +
		`,"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings"` +
		`,"workItemTypes":[` + strings.Join(items, ",") + `]}}}}`
}

func devIssueWithWorkItemTypes() string {
	return issueWithWorkItemTypes("DEV-1", "DEV", devWorkItemTypes()...)
}

func writingTimeOfAType(t *testing.T, read, write http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			read(w, r)
			return
		}
		write(w, r)
	})
}

func sentWorkItemType(t *testing.T, u *upstream) any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body), "the body that went out: %s", lastAsk(u))
	return body[typeKeyName]
}

const typeKeyName = "type"

func pathsSince(u *upstream, before int) []string {
	return u.sentPaths()[before:]
}

func TestTimeCreateRefusesATypeItCannotResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an empty name", argv: []string{"--type", ""}},
		{name: "the flag written twice", argv: []string{"--type", "Разработка", "--type", "Кодревью"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"time", "create", "DEV-1", "PT1H"}, tc.argv...)...)

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeCreateResolvesATypeOfTheProjectWithoutRegardToLetterCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		named string
		at    int
	}{
		{name: "in lower case", named: "разработка", at: 0},
		{name: "in upper case, with the slash of its name", named: "ДИЗАЙН/ПРОТОТИПИРОВАНИЕ", at: 10},
		{name: "an abbreviation in lower case", named: "написание тз", at: 11},
		{name: "two words run together, as the project writes them", named: "планированиеретро", at: 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTimeOfAType(t,
				respondWith(http.StatusOK, devIssueWithWorkItemTypes()),
				respondWith(http.StatusOK, answeredWorkItem{
					workType: `{"$type":"WorkItemType","id":"` + workItemTypeID(tc.at) + `","name":"` +
						devWorkItemTypes()[tc.at] + `"}`,
				}.json()))

			got := runWith(t, server.env(), "time", "create", "dev-1", "PT1H30M", "--type", tc.named)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
			assert.Equal(t, []string{
				"/api/issues/dev-1?fields=" + sentWorkItemTypesFields,
				workItemsPath("DEV-1") + "?fields=" + sentWorkItemWriteFieldsWithType,
			}, server.sentTargets())
			assert.Equal(t, map[string]any{"id": workItemTypeID(tc.at)}, sentWorkItemType(t, server))
			assert.Equal(t, devWorkItemTypes()[tc.at], nodeAt(t, requireMapping(t, "stdout", got.stdout), "type", "name").Value)
		})
	}
}

func TestTimeCreateRefusesATypeTheProjectDoesNotWriteAgainst(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		types   []string
		named   string
		nearest []any
	}{
		{
			name:    "a misspelling",
			types:   devWorkItemTypes(),
			named:   "Разрабока",
			nearest: []any{"Разработка"},
		},
		{
			name:  "a name near none of them",
			types: devWorkItemTypes(),
			named: "zzzzzzzz",
			nearest: []any{"Груминг", "Декомпозиция", "Дизайн/Прототипирование", "Документирование",
				"Исследование", "Кодревью", "Коммуникации", "Написание ТЗ", "Написание инструкции",
				"ПланированиеРетро", "Проектирование", "Разработка", "Тестирование", "ТехОкружение",
				"Уточнение требований"},
		},
		{
			name:    "a name two types answer to in the same letter case",
			types:   []string{"Test", "TEST"},
			named:   "test",
			nearest: []any{"TEST", "Test"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTimeOfAType(t,
				respondWith(http.StatusOK, issueWithWorkItemTypes("DEV-1", "DEV", tc.types...)),
				respondWith(http.StatusOK, answeredWorkItem{}.json()))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--type", tc.named)

			want := faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", sentWorkItemTypesFields)},
					{"project", "DEV"},
					{"unknown", []any{[]detail{{"type", tc.named}, {"nearest", tc.nearest}}}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestTimeCreateTakesTheTypeWrittenByteForByte(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t,
		respondWith(http.StatusOK, issueWithWorkItemTypes("DEV-1", "DEV", "Test", "TEST")),
		respondWith(http.StatusOK, answeredWorkItem{workType: `{"$type":"WorkItemType","id":"178-1","name":"TEST"}`}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--type", "TEST")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, map[string]any{"id": "178-1"}, sentWorkItemType(t, server))
}

func TestTimeCreateWritesNothingWhereTheSettingsWereNotReceived(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		read   string
		code   string
		status int
	}{
		{
			name: "no plugins at all",
			read: `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV"}}`,
			code: "upstream_invalid",
		},
		{
			name: "plugins without the settings of time tracking",
			read: `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
				`"plugins":{"$type":"ProjectPlugins"}}}`,
			code: "upstream_invalid",
		},
		{
			name: "settings without the types of work",
			read: `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
				`"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings"}}}}`,
			code: "upstream_invalid",
		},
		{
			name: "a type whose id is no text",
			read: `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
				`"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings",` +
				`"workItemTypes":[{"$type":"WorkItemType","id":178,"name":"Разработка"}]}}}}`,
			code: "upstream_invalid",
		},
		{
			name: "a type whose name is no text",
			read: `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
				`"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings",` +
				`"workItemTypes":[{"$type":"WorkItemType","id":"178-0","name":null}]}}}}`,
			code: "upstream_invalid",
		},
		{
			name:   "an issue the instance has none of",
			read:   `{"error":"Not Found","error_description":"Entity with id DEV-1 not found"}`,
			code:   "not_found",
			status: http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status := tc.status
			if status == 0 {
				status = http.StatusOK
			}
			server := writingTimeOfAType(t, respondWith(status, tc.read), respondWith(http.StatusOK, answeredWorkItem{}.json()))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--type", "Разработка")

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestTimeCreateRefusesATypeTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		workType string
		received any
	}{
		{
			name:     "another type of the project",
			workType: `{"$type":"WorkItemType","id":"178-6","name":"Кодревью"}`,
			received: "Кодревью",
		},
		{
			name:     "no type at all",
			workType: "null",
			received: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTimeOfAType(t,
				respondWith(http.StatusOK, devIssueWithWorkItemTypes()),
				respondWith(http.StatusOK, answeredWorkItem{workType: tc.workType}.json()))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--type", "разработка")

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{"request", "issue", "id", "mismatch"}, detailKeys(found))
			assert.Equal(t, []any{[]detail{
				{"field", "type"},
				{"expected", "разработка"},
				{"actual", tc.received},
			}}, detailNamed(t, found, "mismatch"))
		})
	}
}

func TestTimeCreateReadsNothingWhereNoTypeIsNamed(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
	assert.NotContains(t, sentWorkItem(t, server), typeKeyName)
}

func TestTimeCreateWritesNoTimeAgainstATypeOutsideTheProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	const typeOfTheInstanceDEVDoesNotUse = "ИИРазработка"
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "create", issue, "PT15M", "--type", typeOfTheInstanceDEVDoesNotUse)

	found := requireFault(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, "DEV", detailNamed(t, found, "project"))
	assert.Equal(t, []any{[]detail{{"type", typeOfTheInstanceDEVDoesNotUse}, {"nearest", []any{"Разработка"}}}},
		detailNamed(t, found, "unknown"))
	assert.Equal(t, []string{"/api/issues/" + issue}, pathsSince(dev, before))

	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

func TestTimeCreateWritesTimeAgainstATypeOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "create", issue, "PT15M", "--type", "кодревью")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "Кодревью", nodeAt(t, requireMapping(t, "stdout", got.stdout), "type", "name").Value)
	assert.Equal(t, []string{"/api/issues/" + issue, workItemsPath(issue)}, pathsSince(dev, before))
}

func TestTimeCreateWritesTimeAgainstNoTypeOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "create", issue, "PT15M")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Nil(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", got.stdout), "type")))
	assert.Equal(t, []string{workItemsPath(issue)}, pathsSince(dev, before))
}
