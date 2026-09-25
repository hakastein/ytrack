package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const removedWorkItemFields = "id,issue(idReadable)"

func workItemReadRequest(address, issue, id string) string {
	return "GET " + address + workItemPath(issue, id) + "?fields=" + removedWorkItemFields
}

func workItemDeletionRequest(address, issue, id string) string {
	return "DELETE " + address + workItemPath(issue, id)
}

func workItemOfAnIssue(id, issue string) string {
	return `{"$type":"IssueWorkItem","id":` + strconv.Quote(id) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(issue) + `}}`
}

func removingTime(t *testing.T, read, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, readThenDeletion(read, deletion))
}

func TestTimeDeleteRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an issue that is no issue", argv: []string{"time", "delete", "DEV-A-1", "199-6"}},
		{name: "an id that is no internal id", argv: []string{"time", "delete", "DEV-1", "199-6-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTimeDeleteReadsTheWorkItemAndThenRemovesIt(t *testing.T) {
	t.Parallel()
	server := removingTime(t, fake.JSON(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")), deletionDone())

	got := runWith(t, server.Env(), "time", "delete", "dev-1", "199-7")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "id: \"199-7\"\nissue:\n  idReadable: \"DEV-1\"\n", got.stdout)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		workItemPath("dev-1", "199-7") + "?fields=" + removedWorkItemFields,
		workItemPath("DEV-1", "199-7") + "?",
	}, server.Targets())
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestTimeDeleteReadsTheAnswerOfEachHalf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		read     http.HandlerFunc
		deletion func(t *testing.T) http.HandlerFunc
		code     string
		methods  []string
	}{
		{
			name:     "a work item the read does not find",
			read:     fake.JSON(http.StatusNotFound, entityNotFound("199-7")),
			deletion: noDeletion,
			code:     "not_found",
			methods:  []string{http.MethodGet},
		},
		{
			name: "a work item taken away between the read and the removal",
			read: fake.JSON(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")),
			deletion: func(*testing.T) http.HandlerFunc {
				return fake.JSON(http.StatusNotFound, entityNotFound("199-7"))
			},
			code:    "not_found",
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "a token that may read the work item and not remove it",
			read: fake.JSON(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")),
			deletion: func(*testing.T) http.HandlerFunc {
				return fake.JSON(http.StatusForbidden, `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`)
			},
			code:    "denied",
			methods: []string{http.MethodGet, http.MethodDelete},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removingTime(t, tc.read, tc.deletion(t))

			got := runWith(t, server.Env(), "time", "delete", "DEV-1", "199-7")

			assert.Equal(t, tc.code, requireFault(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTimeDeleteRemovesNothingByAReadOfAnotherShape(t *testing.T) {
	t.Parallel()
	read := workItemOfAnIssue("..", "DEV-1")
	server := removingTime(t, fake.JSON(http.StatusOK, read), noDeletion(t))

	got := runWith(t, server.Env(), "time", "delete", "DEV-1", "199-7")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", workItemReadRequest(server.URL, "DEV-1", "199-7")},
			{"upstream_status", 200},
			{"upstream_body", read},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTimeDeleteRefusesAnAnswerToTheRemovalThatCarriesABody(t *testing.T) {
	t.Parallel()
	server := removingTime(t, fake.JSON(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")),
		fake.JSON(http.StatusOK, `{"x":1}`))

	got := runWith(t, server.Env(), "time", "delete", "DEV-1", "199-7")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", workItemDeletionRequest(server.URL, "DEV-1", "199-7")},
			{"upstream_status", 200},
			{"upstream_body", `{"x":1}`},
		},
	}, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
}
