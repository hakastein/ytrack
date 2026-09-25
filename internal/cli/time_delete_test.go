package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The one thing the read before a removal asks for, and the whole of what a removal prints: a work item
// carries no readable id of its own, so the pair it is addressed by is its identity.
const removedWorkItemFields = "id,issue(idReadable)"

// The two requests a removal of a work item goes out as, which are the ones a refusal about it names.
func workItemReadRequest(address, issue, id string) string {
	return "GET " + address + workItemPath(issue, id) + "?fields=" + removedWorkItemFields
}

func workItemDeletionRequest(address, issue, id string) string {
	return "DELETE " + address + workItemPath(issue, id)
}

// The work item as the read before a removal sees it: the id it goes by and the issue it hangs from.
func workItemOfAnIssue(id, issue string) string {
	return `{"$type":"IssueWorkItem","id":` + strconv.Quote(id) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(issue) + `}}`
}

// removingTime is the server of a removal of a work item: read answers the GET that settles what is printed
// and where the removal goes, and deletion the DELETE that follows it.
func removingTime(t *testing.T, read, deletion http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, readThenDeletion(read, deletion))
}

// What a removal takes: the issue and the id, both as arguments, and nothing else at all. A single id is
// no address, so a call carrying one is short of an argument rather than given a bad one, and there is no flag
// to say the removal twice.
func TestTimeDeleteRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "neither issue nor id", argv: []string{"time", "delete"}},
		{name: "an issue and no id", argv: []string{"time", "delete", "DEV-1"}},
		{name: "the id of the work item alone", argv: []string{"time", "delete", "199-6"}},
		{name: "a second id", argv: []string{"time", "delete", "DEV-1", "199-6", "199-7"}},
		{name: "a flag that says it twice", argv: []string{"time", "delete", "DEV-1", "199-6", "--yes"}},
		{name: "a flag that says it anyway", argv: []string{"time", "delete", "DEV-1", "199-6", "--force"}},
		{name: "an expression of its own", argv: []string{"time", "delete", "DEV-1", "199-6", "--fields", "id"}},
		{name: "an issue that is no issue", argv: []string{"time", "delete", "DEV-A-1", "199-6"}},
		{name: "an id that is no internal id", argv: []string{"time", "delete", "DEV-1", "199-6-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// Nothing in the help offers a way to say the removal twice: ytrack removes what it was told to remove,
// once, and what a caller reads the work item with first stands there instead.
func TestTimeDeleteHelpOffersNoConfirmation(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"time", "delete", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "ytrack time list")
	assert.NotContains(t, got.stdout, "--yes")
	assert.NotContains(t, got.stdout, "--force")
}

// The whole of the command: the work item is read while it is still there, and the removal that follows
// goes out to the readable id that read gave, carrying no body and no query at all. What the read brought back
// is the document — nothing is read after a write.
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

// What the server says passes on word for word, whichever half of the command it was answering: a work
// item the read does not find is a refusal with nothing destroyed, and one the removal is refused is the
// server's word about a work item that is still there.
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

			assert.Equal(t, tc.code, requireRefusal(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// What the read brought back becomes two path segments of the removal, so both are held to the form ytrack
// sends before anything is destroyed: ".." for an id would turn the removal of a work item into a write to the
// work items of the issue, and the readable id of an article names no issue at all.
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

			assert.Equal(t, "upstream_invalid", requireRefusal(t, got).code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// A removal is answered with nothing at all, so a 200 carrying anything is the answer of something other
// than the endpoint that was asked: the work item may well be gone, and the exit code says the caller cannot
// answer it by sending the call again.
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

func TestTimeDeleteRemovesAWorkItemOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	item := workItemOn(t, dev, issue, "PT1H", "--text", "ytrack contract первая")
	kept := workItemOn(t, dev, issue, "PT30M", "--text", "ytrack contract вторая")

	got := runWith(t, dev.env(), "time", "delete", issue, item)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "issue"}, keysOf(mapping))
	assert.Equal(t, item, nodeAt(t, mapping, "id").Value)
	assert.Equal(t, issue, nodeAt(t, mapping, "issue", "idReadable").Value)

	listed := theWorkItemsOf(t, dev, issue)
	require.Len(t, listed, 1)
	assert.Equal(t, kept, listed[0]["id"])

	shown := runWith(t, dev.env(), "issue", "show", issue, "--comments=0", "--fields", "customFields")
	require.Equal(t, 0, shown.code, "stderr: %s", shown.stderr)
	assert.Equal(t, "PT30M",
		nodeAt(t, requireMapping(t, "stdout", shown.stdout), "customFields", "Затраченное время").Value)

	before := len(dev.requests())
	again := runWith(t, dev.env(), "time", "delete", issue, item)
	assert.Equal(t, "not_found", requireRefusal(t, again).code)
	assert.Equal(t, []string{workItemPath(issue, item)}, pathsSince(dev, before))
}

// A token that may not see the issue is answered as if the work item were not there, on the read, so the
// removal never goes out and the admin finds the work item where it was.
func TestTimeDeleteRefusesAnIssueTheLimitedUserMayNotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	item := workItemOn(t, dev, issue, "PT1H", "--text", "ytrack contract первая")
	before := len(dev.requests())

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"time", "delete", issue, item)

	assert.Equal(t, "not_found", requireRefusal(t, got).code)
	assert.Equal(t, []string{workItemPath(issue, item)}, pathsSince(dev, before))

	listed := theWorkItemsOf(t, dev, issue)
	require.Len(t, listed, 1)
	assert.Equal(t, item, listed[0]["id"])
}
