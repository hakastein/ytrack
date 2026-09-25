package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func removingTime(t *testing.T, read, deletion http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, readThenDeletion(read, deletion))
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
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeDeleteReadsTheWorkItemAndThenRemovesIt(t *testing.T) {
	t.Parallel()
	server := removingTime(t, respondWith(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")), deletionDone())

	got := runWith(t, server.env(), "time", "delete", "dev-1", "199-7")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "id: \"199-7\"\nissue:\n  idReadable: \"DEV-1\"\n", got.stdout)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		workItemPath("dev-1", "199-7") + "?fields=" + removedWorkItemFields,
		workItemPath("DEV-1", "199-7") + "?",
	}, server.sentTargets())
	assert.Equal(t, []string{"", ""}, server.asks())
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
			read:     respondWith(http.StatusNotFound, entityNotFound("199-7")),
			deletion: noDeletion,
			code:     "not_found",
			methods:  []string{http.MethodGet},
		},
		{
			name: "a work item taken away between the read and the removal",
			read: respondWith(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")),
			deletion: func(*testing.T) http.HandlerFunc {
				return respondWith(http.StatusNotFound, entityNotFound("199-7"))
			},
			code:    "not_found",
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "a token that may read the work item and not remove it",
			read: respondWith(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")),
			deletion: func(*testing.T) http.HandlerFunc {
				return respondWith(http.StatusForbidden, `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`)
			},
			code:    "denied",
			methods: []string{http.MethodGet, http.MethodDelete},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removingTime(t, tc.read, tc.deletion(t))

			got := runWith(t, server.env(), "time", "delete", "DEV-1", "199-7")

			assert.Equal(t, tc.code, requireFault(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTimeDeleteRemovesNothingAddressedByWhatTheReadGave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read string
	}{
		{name: "an id that is no internal id", read: workItemOfAnIssue("..", "DEV-1")},
		{name: "an id that is not a string", read: `{"$type":"IssueWorkItem","id":7,"issue":null}`},
		{name: "a readable id of an article", read: workItemOfAnIssue("199-7", "DEV-A-1")},
		{
			name: "no issue at all",
			read: `{"$type":"IssueWorkItem","id":"199-7","issue":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removingTime(t, respondWith(http.StatusOK, tc.read), noDeletion(t))

			got := runWith(t, server.env(), "time", "delete", "DEV-1", "199-7")

			assert.Equal(t, "upstream_invalid", requireFault(t, got).code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestTimeDeleteRefusesAnAnswerToTheRemovalThatCarriesABody(t *testing.T) {
	t.Parallel()
	server := removingTime(t, respondWith(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")),
		respondWith(http.StatusOK, `{"x":1}`))

	got := runWith(t, server.env(), "time", "delete", "DEV-1", "199-7")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, detail{"request", workItemDeletionRequest(server.url, "DEV-1", "199-7")}, found.details[0])
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
}
