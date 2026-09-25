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

// What the read before a write asks of the issue where the call names an attribute: the types of work, as for
// --type, and beside them the attributes of the project with their values.
const sentWorkItemSettingsFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name)," +
	"attributes(id,name,values(id,name)))))"

// devIssueWithAttributes is the read of DEV-1 where the call names an attribute: the types of work of DEV and its
// one attribute, as a live instance keeps it.
func devIssueWithAttributes() string {
	withTypes := devIssueWithWorkItemTypes()
	attributes := `,"attributes":[{"$type":"WorkItemProjectAttribute","id":"309-0","name":"Формат работы","values":[` +
		`{"$type":"WorkItemAttributeValue","id":"506-0","name":"Сам"},` +
		`{"$type":"WorkItemAttributeValue","id":"506-1","name":"ИИагент"}]}]`
	return strings.TrimSuffix(withTypes, "}}}}") + attributes + "}}}}"
}

// sentAttributes is the attributes the body of the write carried, read as JSON reads it.
func sentAttributes(t *testing.T, u *upstream) any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body), "the body that went out: %s", lastAsk(u))
	return body["attributes"]
}

// An attribute and its value go out by the ids the project gives them, read in one request before the write and
// found without regard to letter case; the work item prints the attribute under its name.
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

// A type and an attribute named together cost the one read: both are settings of the same project.
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

// Every name that answers to no attribute of the project, and every value no attribute takes, is refused at
// once with the names nearest it, and nothing is written.
func TestTimeUpdateRefusesAnAttributeTheProjectHasNot(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t, respondWith(http.StatusOK, devIssueWithAttributes()),
		func(http.ResponseWriter, *http.Request) { t.Error("a work item was written") })

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--attribute", "Формат работы=ИИ",
		"--clear", "Формат")

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, "DEV", detailNamed(t, found, "project"))
	assert.Equal(t, []any{
		[]detail{{"attribute", "Формат работы"}, {"value", "ИИ"}, {"nearest", []any{"ИИагент", "Сам"}}},
		[]detail{{"attribute", "Формат"}, {"nearest", []any{"Формат работы"}}},
	}, detailNamed(t, found, "unknown"))
	assert.Equal(t, []string{http.MethodGet}, methodsOf(server))
}

// --clear takes an attribute away by an explicit null, and a name that is neither type nor text is an attribute.
func TestTimeUpdateTakesAnAttributeAway(t *testing.T) {
	t.Parallel()
	server := writingTimeOfAType(t, respondWith(http.StatusOK, devIssueWithAttributes()),
		respondWith(http.StatusOK, answeredWorkItem{}.json()))

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-7", "--clear", "Формат работы")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []any{map[string]any{"id": "309-0", "value": nil}}, sentAttributes(t, server))
}

// One attribute set and taken away in one call is two writes of one place, refused before anything is read.
func TestTimeUpdateRefusesAnAttributeSetAndRemoved(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "update", "DEV-1", "199-7", "--attribute", "Формат работы=Сам",
		"--clear", "ФОРМАТ РАБОТЫ")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// A value the server kept other than the one written is the write going through and landing elsewhere.
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

// The attributes of a work item are a block ytrack composes itself, so no name stands under it.
func TestTimeListRefusesANameUnderTheAttributes(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,attributes(id)")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// A --attribute the call cannot send is refused before anything is read.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
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

func TestTimeWritesAndTakesAwayAnAttributeOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	id := workItemOn(t, dev, "DEV-1", "PT5M", "--attribute", "Формат работы=ИИагент", "--text", "атрибут")
	cleared := runWith(t, dev.env(), "time", "update", "DEV-1", id, "--clear", "Формат работы", "--fields", "attributes")
	removed := runWith(t, dev.env(), "time", "delete", "DEV-1", id)

	assert.Equal(t, outcome{stdout: "attributes:\n  \"Формат работы\": null\n"}, cleared)
	require.Equal(t, 0, removed.code, "stderr: %s", removed.stderr)
}
