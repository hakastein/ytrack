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

// What the read before a write asks of the issue: the readable id the write is addressed by, and the types of
// work of the project it is filed in, which is where a type of work comes from.
const sentWorkItemTypesFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name))))"

// What goes out for the write itself once a type was resolved: the id of it is asked for beside the name, since
// the id is what the check holds the answer to and the name is what a refusal shows.
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

// issueWithWorkItemTypes is the issue as the read before a write sees it: the readable id the write goes to and
// the types of work of its project, with the $type the server puts on every object of the answer.
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

// writingTimeOfAType is the server of a write that names a type of work: the read of the issue is answered with
// read and the write itself with write, so a scenario says what each of the two requests found.
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

// sentWorkItemType is the type the body of the write carried, read as JSON reads it.
func sentWorkItemType(t *testing.T, u *upstream) any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body), "the body that went out: %s", lastAsk(u))
	return body[typeKeyName]
}

// The key a type of work stands under, in the body of a write and in the answer alike.
const typeKeyName = "type"

// pathsSince is what the server was sent after the count a scenario took before the command it holds to them:
// a contract scenario files an issue of its own first, and those requests are not what it is about.
func pathsSince(u *upstream, before int) []string {
	return u.sentPaths()[before:]
}

// A name of no type at all is refused before the network. The read before the write would find out as much
// from the server, and that read is a request sent to learn that an empty string names nothing.
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

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// The name is matched against the types of the project without regard to letter case, and what goes out is
// the id: two requests, the read of the issue as the caller addressed it and the write to the readable id that
// read gave.
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

// A name that answers to no one type of the project is a refusal naming the project it was held against and
// the names nearest it, in one request: the write never goes out, and nothing of the name reaches the server.
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
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// Where two types answer to the name in the same letter case, the one written byte for byte takes it: the
// name a type is printed under stays the address, whatever else the project called another one.
func TestTimeCreateTakesTheTypeWrittenByteForByte(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t,
		respondWith(http.StatusOK, issueWithWorkItemTypes("DEV-1", "DEV", "Test", "TEST")),
		respondWith(http.StatusOK, answeredWorkItem{workType: `{"$type":"WorkItemType","id":"178-1","name":"TEST"}`}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--type", "TEST")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, map[string]any{"id": "178-1"}, sentWorkItemType(t, server))
}

// The settings a type is resolved against are what ytrack asked for on its own behalf, so an answer that
// carries none of them is the server saying something other than what was asked, not a name the caller can fix.
// Nothing is written in any of these.
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
			// A type of work is addressed by the id and matched by the name, so neither is a number the
			// resolving could go on with: an id read off a number would send {"type":{"id":""}}.
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

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// The type is held against the id that went out, and a work item that came back written against another one
// — or against none — is the server disagreeing with the write as much as a duration of another length is. The
// work item exists by then, so the refusal names it and the code says the write is not to be sent again.
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

// Nothing is read where the call names no type: one POST is the whole command, and the body says nothing
// about a type, which is what leaves YouTrack to write the work item against none.
func TestTimeCreateReadsNothingWhereNoTypeIsNamed(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
	assert.NotContains(t, sentWorkItem(t, server), typeKeyName)
}

// The instance holds seventeen types of work and DEV writes against fifteen of them: a name of one of the
// other two is refused by the settings of the project, in the one request that read them, and no work item is
// written. This is what says the set comes from the project and not from the catalogue of the instance.
func TestTimeCreateWritesNoTimeAgainstATypeOutsideTheProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "create", issue, "PT15M", "--type", "ИИРазработка")

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, "DEV", detailNamed(t, found, "project"))
	assert.Equal(t, []any{[]detail{{"type", "ИИРазработка"}, {"nearest", []any{"Разработка"}}}},
		detailNamed(t, found, "unknown"))
	assert.Equal(t, []string{"/api/issues/" + issue}, pathsSince(dev, before))

	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

// A type of the project, named in another letter case, is resolved against the settings the read brought
// back and written by id: two requests, and the work item comes back under the name the project prints.
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

// A work item written against no type comes back with none, and nothing was read to find that out.
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
