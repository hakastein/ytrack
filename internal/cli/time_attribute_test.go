package cli_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sentWorkItemSettingsFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name)," +
	"attributes(id,name,values(id,name)))))"

func devIssueWithAttributes() string {
	withTypes := devIssueWithWorkItemTypes()
	attributes := `,"attributes":[{"$type":"WorkItemProjectAttribute","id":"309-0","name":"Формат работы","values":[` +
		`{"$type":"WorkItemAttributeValue","id":"506-0","name":"Сам"},` +
		`{"$type":"WorkItemAttributeValue","id":"506-1","name":"ИИагент"}]}]`
	return strings.TrimSuffix(withTypes, "}}}}") + attributes + "}}}}"
}

func sentAttributes(t *testing.T, u *upstream) any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body), "the body that went out: %s", lastAsk(u))
	return body["attributes"]
}

func TestTimeCreateWritesAnAttributeByTheIDsOfTheProject(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t, respondWith(http.StatusOK, devIssueWithAttributes()),
		respondWith(http.StatusOK, answeredWorkItem{attributes: sentAgentAttribute}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--attribute", "формат работы=ииагент",
		"--fields", "id,attributes")

	assert.Equal(t, outcome{stdout: "id: \"199-7\"\nattributes:\n  \"Формат работы\": \"ИИагент\"\n"}, got)
	assert.Equal(t, []any{map[string]any{"id": "309-0", "value": map[string]any{"id": "506-1"}}}, sentAttributes(t, server))
	queries := server.sentQueries()
	require.Len(t, queries, 2)
	assert.Equal(t, sentWorkItemSettingsFields, queries[0].Get("fields"))
}

func TestTimeCreateReadsTheTypeAndTheAttributeInOneRequest(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t, respondWith(http.StatusOK, devIssueWithAttributes()),
		respondWith(http.StatusOK, answeredWorkItem{workType: `{"$type":"WorkItemType","id":"178-0","name":"Разработка"}`,
			attributes: sentAgentAttribute}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--type", "Разработка",
		"--attribute", "Формат работы=ИИагент")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, methodsOf(server))
}

func TestTimeUpdateRefusesAnAttributeTheProjectHasNot(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t, respondWith(http.StatusOK, devIssueWithAttributes()),
		func(http.ResponseWriter, *http.Request) { t.Error("a work item was written") })

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--attribute", "Формат работы=ИИ",
		"--clear", "Формат")

	found := requireFault(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, "DEV", detailNamed(t, found, "project"))
	assert.Equal(t, []any{
		[]detail{{"attribute", "Формат работы"}, {"value", "ИИ"}, {"nearest", []any{"ИИагент", "Сам"}}},
		[]detail{{"attribute", "Формат"}, {"nearest", []any{"Формат работы"}}},
	}, detailNamed(t, found, "unknown"))
	assert.Equal(t, []string{http.MethodGet}, methodsOf(server))
}

func TestTimeUpdateTakesAnAttributeAway(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t, respondWith(http.StatusOK, devIssueWithAttributes()),
		respondWith(http.StatusOK, answeredWorkItem{}.json()))

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-7", "--clear", "Формат работы")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []any{map[string]any{"id": "309-0", "value": nil}}, sentAttributes(t, server))
}

func TestTimeUpdateRefusesAnAttributeSetAndRemoved(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-7", "--attribute", "Формат работы=Сам",
		"--clear", "ФОРМАТ РАБОТЫ")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestTimeCreateRefusesAnAttributeTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t, respondWith(http.StatusOK, devIssueWithAttributes()),
		respondWith(http.StatusOK, answeredWorkItem{attributes: sentNoAttribute}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--attribute", "Формат работы=ИИагент")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, []any{[]detail{{"field", "Формат работы"}, {"expected", "ИИагент"}, {"actual", nil}}},
		detailNamed(t, found, "mismatch"))
}

func TestTimeListRefusesANameUnderTheAttributes(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,attributes(id)")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestTimeCreateRefusesAnAttributeItCannotSend(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		{"--attribute", "Формат работы"},
		{"--attribute", "=ИИагент"},
		{"--attribute", "Формат работы="},
		{"--attribute", "Формат работы=Сам", "--attribute", "ФОРМАТ РАБОТЫ=ИИагент"},
	}
	for _, argv := range tests {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"time", "create", "DEV-1", "PT1H"}, argv)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func methodsOf(u *upstream) []string {
	var methods []string
	for _, request := range u.requests() {
		methods = append(methods, request.Method)
	}
	return methods
}
