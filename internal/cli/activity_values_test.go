package cli_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func sentFieldChange(valueType, added, removed string) string {
	field := `{"$type":"CustomFilterField","name":"Поле","customField":{"$type":"CustomField","name":"Field",` +
		`"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(valueType) + `}}}`
	return sentActivity{
		kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
		field: field, added: added, removed: removed,
	}.sent()
}

func sentJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func TestActivityPrintsTheValuesOfAChangeAlwaysAsAList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		activity string
		want     string
	}{
		{
			name: "a resolution, which puts one moment there and takes null away",
			activity: sentActivity{
				kind: "IssueResolvedActivityItem", category: "IssueResolvedCategory", timestamp: middle,
				added: `1789035411782`, removed: `null`,
			}.sent(),
			want: `added: ["2026-09-10T10:16:51.782Z"], removed: []`,
		},
		{
			name: "a summary, which holds one text at either end",
			activity: sentActivity{
				kind: "SimpleValueActivityItem", category: "SummaryCategory", timestamp: middle,
				added: `"[bug] fix login"`, removed: `"fix login"`,
			}.sent(),
			want: `added: ["[bug] fix login"], removed: ["fix login"]`,
		},
		{
			name: "a link, which holds a list of one issue and an empty list",
			activity: sentActivity{
				kind: "LinksActivityItem", category: "LinksCategory", timestamp: middle,
				field: `{"$type":"LinkTypeFilterField","name":"Зависит от"}`, added: sentLinkedIssue,
			}.sent(),
			want: `added: [{id: "3-21", idReadable: "DEV-3"}], removed: []`,
		},
		{
			name: "a filing, which puts nothing anywhere",
			activity: sentActivity{
				kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory", timestamp: middle,
			}.sent(),
			want: `added: [], removed: []`,
		},
		{
			name: "a filing whose issue arrived on its own rather than in a list",
			activity: sentActivity{
				kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory", timestamp: middle,
				added: `{"$type":"Issue","id":"3-25","idReadable":"DEV-7"}`, removed: `null`,
			}.sent(),
			want: `added: [{id: "3-25", idReadable: "DEV-7"}], removed: []`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, respondWith(http.StatusOK, `[`+tc.activity+`]`))

			got := runWith(t, server.env(), "activity", "list", activityIssue,
				"--fields", "added(id,idReadable),removed(id,idReadable)")

			assert.Equal(t, outcome{stdout: oneRecord(tc.want)}, got)
		})
	}
}

func TestActivityPrintsTheValuesOfACustomFieldByTheTypeOfTheField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		added     string
		want      string
	}{
		{name: "a state", valueType: "state", added: `[{"$type":"StateBundleElement","id":"156-17","name":"Pending"}]`,
			want: `[{id: "156-17", name: "Pending"}]`},
		{
			name: "an enum of two values", valueType: "enum",
			added: `[{"$type":"EnumBundleElement","id":"157-1","name":"Ядро"},{"$type":"EnumBundleElement","id":"157-2","name":"API"}]`,
			want:  `[{id: "157-1", name: "Ядро"}, {id: "157-2", name: "API"}]`,
		},
		{name: "a group", valueType: "group", added: `[{"$type":"UserGroup","id":"4-2","name":"Участники полигона"}]`,
			want: `[{id: "4-2", name: "Участники полигона"}]`},
		{
			name: "a user, whose full name the server sends beside the login", valueType: "user",
			added: `[{"$type":"User","id":"2-231","login":"Сидорова.Анна","name":"Анна Сидорова"}]`,
			want:  `[{id: "2-231", login: "Сидорова.Анна", name: "Анна Сидорова"}]`,
		},
		{name: "a period", valueType: "period", added: `6755`, want: `["PT112H35M"]`},
		{name: "a period of no minutes at all", valueType: "period", added: `0`, want: `["PT0M"]`},
		{name: "a date", valueType: "date", added: `1789041600000`, want: `["2026-09-10"]`},
		{name: "a moment", valueType: "date and time", added: `0`, want: `["1970-01-01T00:00:00Z"]`},
		{name: "a whole number", valueType: "integer", added: `10`, want: `[10]`},
		{name: "a fraction", valueType: "float", added: `1.5`, want: `[1.5]`},
		{name: "a line of text", valueType: "string", added: `"на полигоне"`, want: `["на полигоне"]`},
		{name: "prose", valueType: "text", added: `"first\nsecond"`, want: `["first\nsecond"]`},
		{name: "a field emptied outright", valueType: "state", added: `null`, want: `[]`},
		{name: "a field nothing was put into", valueType: "state", added: `[]`, want: `[]`},
		{name: "a scalar field emptied outright", valueType: "period", added: `null`, want: `[]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, respondWith(http.StatusOK, `[`+sentFieldChange(tc.valueType, tc.added, "")+`]`))

			got := runWith(t, server.env(), "activity", "list", activityIssue, "--fields", "+added")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Contains(t, got.stdout, "added: "+tc.want+", removed: []}")
			assert.NotContains(t, got.stdout, "$type")
		})
	}
}

func TestActivityPrintsAValueByTheNamesAskedOfIt(t *testing.T) {
	t.Parallel()
	comment := sentActivity{
		kind: "CommentActivityItem", category: "CommentsCategory", timestamp: middle,
		added: `[{"$type":"IssueComment","id":"7-1","text":"Первый комментарий","author":{"$type":"User","login":"admin"}}]`,
	}.sent()
	tests := []struct {
		name   string
		fields string
		want   string
	}{
		{name: "the default, which names a comment by its id alone", fields: "+added", want: `added: [{id: "7-1"}], removed: []}`},
		{name: "the text added to the default", fields: "+added(text)",
			want: `added: [{id: "7-1", text: "Первый комментарий"}], removed: []}`},
		{name: "names of the caller's own", fields: "added(text,author(login))",
			want: `{added: [{text: "Первый комментарий", author: {login: "admin"}}]}`},
		{name: "the value asked for with no names under it", fields: "added", want: `{added: [{}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, respondWith(http.StatusOK, `[`+comment+`]`))

			got := runWith(t, server.env(), "activity", "list", activityIssue, "--fields", tc.fields)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Contains(t, got.stdout, tc.want)
		})
	}
}

func TestActivityLeavesOutOfAValueANameOnlyAnotherTypeDeclares(t *testing.T) {
	t.Parallel()
	assigned := sentActivity{
		kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: newest,
		field: `{"$type":"CustomFilterField","name":"Исполнитель","customField":{"$type":"CustomField",` +
			`"name":"Assignee","fieldType":{"$type":"FieldType","valueType":"user"}}}`,
		added: `[{"$type":"User","login":"admin"}]`,
	}.sent()
	commented := sentActivity{
		kind: "CommentActivityItem", category: "CommentsCategory", timestamp: oldest,
		added: `[{"$type":"IssueComment"}]`,
	}.sent()
	server := activityServer(t, respondWith(http.StatusOK, `[`+assigned+`,`+sentLinkActivity(middle)+`,`+commented+`]`))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--fields", "category,added(login,idReadable)")

	want := "total: 3\nreturned: 3\ntruncated: false\nactivities:\n" +
		`  - {category: "CustomFieldCategory", added: [{login: "admin"}]}` + "\n" +
		`  - {category: "LinksCategory", added: [{idReadable: "DEV-3"}]}` + "\n" +
		`  - {category: "CommentsCategory", added: [{}]}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestActivityRefusesANameNoTypeOfAValueDeclares(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fields  string
		unknown []any
	}{
		{name: "a login misspelled", fields: "added(logn)", unknown: []any{unknownEntry("added(logn)", "login")}},
		{
			name:    "a misspelling added to the default",
			fields:  "+removed(idReadabel)",
			unknown: []any{unknownEntry("removed(idReadabel)", "idReadable")},
		},
		{
			name:    "one misspelling at each end",
			fields:  "added(verson),removed(urlz)",
			unknown: []any{unknownEntry("added(verson)", "version"), unknownEntry("removed(urlz)", "url", "urls")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "activity", "list", activityIssue, "--fields", tc.fields)

			want := faultDocument{
				code:    "unknown_name",
				details: []detail{{"fields", tc.fields}, {"unknown", tc.unknown}},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestActivityTakesANameAnyTypeOfAValueDeclares(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, `[`+sentLinkActivity(middle)+`]`))

	got := runWith(t, server.env(), "activity", "list", activityIssue,
		"--fields", "added(idReadable,localizedName,version),removed(text)")

	assert.Equal(t, outcome{stdout: oneRecord(`added: [{idReadable: "DEV-3"}], removed: []`)}, got)
}

func TestActivityPrintsTheDurationOfAWorkItemAsAPeriod(t *testing.T) {
	t.Parallel()
	changed := sentActivity{
		kind: "WorkItemDurationActivityItem", category: "WorkItemCategory", timestamp: middle,
		field: `{"$type":"WorkItemFilterField","name":"работа"}`,
		added: `{"$type":"DurationValue","id":"120","minutes":120}`, removed: `{"$type":"DurationValue","id":"90","minutes":90}`,
	}.sent()
	server := activityServer(t, respondWith(http.StatusOK, `[`+changed+`]`))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--fields", "added,removed")

	assert.Equal(t, outcome{stdout: oneRecord(`added: ["PT2H"], removed: ["PT1H30M"]`)}, got)
	assert.Contains(t, activitySent(t, server).Get("fields"), "added(minutes)")
}

func TestActivityPrintsTheTextOfAnEditOnTheOneLineOfTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range textCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			edit := sentActivity{
				kind: "TextMarkupActivityItem", category: "DescriptionCategory", timestamp: middle,
				added: sentJSON(t, tc.text), removed: "null",
			}.sent()
			server := activityServer(t, respondWith(http.StatusOK, `[`+edit+`]`))

			got := runWith(t, server.env(), "activity", "list", activityIssue, "--fields", "added,removed")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			record := requireMapping(t, "stdout", got.stdout)
			added := nodeAt(t, record, "activities", "added")
			require.Len(t, added.Content, 1)
			assert.Equal(t, tc.text, added.Content[0].Value)
			assert.Equal(t, yaml.DoubleQuotedStyle, added.Content[0].Style)
			assert.Empty(t, nodeAt(t, record, "activities", "removed").Content)
			assert.Equal(t, 1, strings.Count(got.stdout, "\n  - "))
		})
	}
}

func TestActivityRefusesValuesTheFieldOfTheChangeDoesNotReferTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		activity string
	}{
		{
			name:     "an object where the type of the field holds a bare value",
			activity: sentFieldChange("period", `[{"$type":"DurationValue","id":"90","minutes":90}]`, ""),
		},
		{
			name:     "a bare value where the type of the field holds values with names of their own",
			activity: sentFieldChange("state", `"Pending"`, ""),
		},
		{
			name:     "a value of a type outside the twenty ytrack models",
			activity: sentFieldChange("favourite colour", `[{"$type":"EnumBundleElement","id":"157-3","name":"зелёный"}]`, ""),
		},
		{
			name: "a change of a custom field standing for a filter that is no custom field",
			activity: sentActivity{
				kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
			}.sent(),
		},
		{
			name: "a change of a custom field standing for no filter at all",
			activity: sentActivity{
				kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
				field: `"Состояние"`,
			}.sent(),
		},
		{
			name: "a custom field arriving with no type of value",
			activity: sentActivity{
				kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
				field: `{"$type":"CustomFilterField","name":"Поле","customField":{"$type":"CustomField","name":"Field"}}`,
			}.sent(),
		},
		{
			name:     "a fraction where the number of minutes of a duration stands",
			activity: sentFieldChange("period", `1.5`, ""),
		},
		{
			name:     "a moment that is no count of milliseconds",
			activity: sentFieldChange("date and time", `"2026-09-10"`, ""),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, respondWith(http.StatusOK, `[`+tc.activity+`]`))

			got := runWith(t, server.env(), "activity", "list", activityIssue)

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}

const capturedFieldActivities = `[` +
	`{"removed":[{"name":"Готово к передаче","id":"150-65","$type":"StateBundleElement"}],"added":[{"name":"Реализовано","id":"150-80","$type":"StateBundleElement"}],"field":{"name":"Статус анализа","customField":{"name":"Статус анализа","fieldType":{"valueType":"state","$type":"FieldType"},"$type":"CustomField"},"$type":"CustomFilterField"},"id":"0-0.14-120311","timestamp":1766647197137,"author":{"login":"Сидорова.Анна","$type":"User"},"category":{"id":"CustomFieldCategory","$type":"ActivityCategory"},"$type":"CustomFieldActivityItem"},` +
	`{"removed":null,"added":1764116587829,"field":{"name":"Дата закрытия","customField":{"name":"Дата закрытия","fieldType":{"valueType":"date and time","$type":"FieldType"},"$type":"CustomField"},"$type":"CustomFilterField"},"id":"0-0.14-65180","timestamp":1764116587937,"author":{"login":"Сидорова.Анна","$type":"User"},"category":{"id":"CustomFieldCategory","$type":"ActivityCategory"},"$type":"CustomFieldActivityItem"},` +
	`{"removed":340,"added":440,"field":{"name":"Затраченное время","customField":{"name":"Затраченное время","fieldType":{"valueType":"period","$type":"FieldType"},"$type":"CustomField"},"$type":"CustomFilterField"},"id":"0-0.14-41597","timestamp":1763041922425,"author":{"login":"Петров.Пётр","$type":"User"},"category":{"id":"CustomFieldCategory","$type":"ActivityCategory"},"$type":"CustomFieldActivityItem"},` +
	`{"removed":[{"login":"Петров.Пётр","name":"Петров Пётр","id":"2-268","$type":"User"}],"added":[{"login":"Сидорова.Анна","name":"Сидорова Анна","id":"2-231","$type":"User"}],"field":{"name":"Исполнитель","customField":{"name":"Исполнитель","fieldType":{"valueType":"user","$type":"FieldType"},"$type":"CustomField"},"$type":"CustomFilterField"},"id":"0-0.14-34874","timestamp":1762324104384,"author":{"login":"Сидорова.Анна","$type":"User"},"category":{"id":"CustomFieldCategory","$type":"ActivityCategory"},"$type":"CustomFieldActivityItem"}` +
	`]`

func TestActivityPrintsTheChangesOfCustomFieldsOfALiveInstance(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, capturedFieldActivities))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--category", "CustomFieldCategory")

	want := "total: 4\nreturned: 4\ntruncated: false\nactivities:\n" +
		`  - {timestamp: "2025-12-25T07:19:57.137Z", author: {login: "Сидорова.Анна"}, category: "CustomFieldCategory", ` +
		`field: "Статус анализа", added: [{id: "150-80", name: "Реализовано"}], ` +
		`removed: [{id: "150-65", name: "Готово к передаче"}]}` + "\n" +
		`  - {timestamp: "2025-11-26T00:23:07.937Z", author: {login: "Сидорова.Анна"}, category: "CustomFieldCategory", ` +
		`field: "Дата закрытия", added: ["2025-11-26T00:23:07.829Z"], removed: []}` + "\n" +
		`  - {timestamp: "2025-11-13T13:52:02.425Z", author: {login: "Петров.Пётр"}, category: "CustomFieldCategory", ` +
		`field: "Затраченное время", added: ["PT7H20M"], removed: ["PT5H40M"]}` + "\n" +
		`  - {timestamp: "2025-11-05T06:28:24.384Z", author: {login: "Сидорова.Анна"}, category: "CustomFieldCategory", ` +
		`field: "Исполнитель", added: [{id: "2-231", login: "Сидорова.Анна", name: "Сидорова Анна"}], ` +
		`removed: [{id: "2-268", login: "Петров.Пётр", name: "Петров Пётр"}]}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"CustomFieldCategory"}, activitySent(t, server)["categories"])
}
