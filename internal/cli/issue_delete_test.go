package cli_test

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The one name the read before a deletion asks for: the answer of the deletion itself carries nothing, so the
// id it is addressed by and the id it prints are settled here.
const deletedFields = "idReadable"

// An issue as that read sees it, with the $type the server names it by.
func issueNamed(readable string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `}`
}

// readThenDeletion is the handler of a deletion: read answers the GET that settles the id, and deletion
// answers the DELETE that follows it, so a scenario says what each half of the command was told.
func readThenDeletion(read, deletion http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletion(w, r)
			return
		}
		read(w, r)
	}
}

func deleting(t *testing.T, read, deletion http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, readThenDeletion(read, deletion))
}

// What YouTrack answers a deletion it carried out with: 200, an empty body and no content type at all.
func deletionDone() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
}

// noDeletion stands for the request a refusal before the deletion must not send.
func noDeletion(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a deletion reached the server", "%s %s", r.Method, r.URL)
	}
}

// The refusal an issue no token of the caller's may read becomes: the read answers 404 and nothing follows it.
func noIssueToDelete(address, id string) refusal {
	return refusal{
		code: "not_found",
		details: []detail{
			{"request", issueRequest(address, id, deletedFields)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id " + id + " not found"},
		},
	}
}

// What the server says about an issue it has none of, word for word.
func entityNotFound(id string) string {
	return `{"error":"Not Found","error_description":"Entity with id ` + id + ` not found"}`
}

// Everything settled before the network: how many ids the command takes, what an id may look like, and that
// no flag stands between the caller and the deletion.
func TestIssueDeleteRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no id", argv: []string{"issue", "delete"}},
		{name: "two ids", argv: []string{"issue", "delete", "DEV-1", "DEV-2"}},
		{name: "two dots", argv: []string{"issue", "delete", ".."}},
		{name: "an article id", argv: []string{"issue", "delete", "DEV-A-1"}},
		{name: "an internal id", argv: []string{"issue", "delete", "3-26"}},
		{name: "--yes", argv: []string{"issue", "delete", "DEV-1", "--yes"}},
		{name: "--force", argv: []string{"issue", "delete", "DEV-1", "--force"}},
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

// The help promises no way to say it again: a flag the command has none of would be read as a word of the
// caller's, and one it had would be a habit of typing it before every deletion.
func TestIssueDeleteHelpOffersNoConfirmation(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"issue", "delete", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.NotContains(t, got.stdout, "--yes")
	assert.NotContains(t, got.stdout, "--force")
}

// The whole of the command: the argument is read as the server resolves it, the deletion is addressed by
// the id that came back, and that id is the document.
func TestIssueDeleteReadsTheIDAndDeletesByIt(t *testing.T) {
	t.Parallel()
	server := deleting(t, answer(http.StatusOK, issueNamed("DEV-7")), deletionDone())

	got := runWith(t, server.env(), "issue", "delete", "dev-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\n"}, got)
	requests := server.requests()
	require.Len(t, requests, 2)
	assert.Equal(t, http.MethodGet, requests[0].Method)
	assert.Equal(t, "/api/issues/dev-7", requests[0].URL.Path)
	assert.Equal(t, url.Values{"fields": {deletedFields}}, requests[0].URL.Query())
	assert.Equal(t, http.MethodDelete, requests[1].Method)
	assert.Equal(t, "/api/issues/DEV-7", requests[1].URL.Path)
	assert.Empty(t, requests[1].URL.RawQuery, "a deletion asks for no fields")
	assert.Equal(t, "Bearer "+token, requests[1].Header.Get("Authorization"))
	assert.Equal(t, []string{"", ""}, server.asks(), "neither request carries a body")
}

// An issue the read does not find is a refusal and nothing else: the deletion of an id that is not there
// would be answered 404 as well, and this way the caller hears it before anything is destroyed.
func TestIssueDeleteRefusesAnIssueTheReadDoesNotFind(t *testing.T) {
	t.Parallel()
	server := deleting(t, answer(http.StatusNotFound, entityNotFound("dev-7")), noDeletion(t))

	got := runWith(t, server.env(), "issue", "delete", "dev-7")

	assert.Equal(t, noIssueToDelete(server.url, "dev-7"), requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// What the server says about the deletion itself passes on with the request it answered, which is the one
// that carried the id out.
func TestIssueDeleteRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		status          int
		upstreamError   string
		upstreamMessage string
		code            string
		details         []detail
	}{
		{
			name:            "the issue is gone between the read and the deletion",
			status:          http.StatusNotFound,
			upstreamError:   "Not Found",
			upstreamMessage: "Entity with id DEV-7 not found",
			code:            "not_found",
		},
		{
			name:            "the token may read the issue and not delete it",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Insufficient rights",
			code:            "denied",
			details:         []detail{authFromEnv()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"error":` + strconv.Quote(tc.upstreamError) + `,"error_description":` + strconv.Quote(tc.upstreamMessage) + `}`
			server := deleting(t, answer(http.StatusOK, issueNamed("DEV-7")), answer(tc.status, body))

			got := runWith(t, server.env(), "issue", "delete", "DEV-7")

			want := refusal{
				code: tc.code,
				details: append([]detail{
					{"request", "DELETE " + server.url + "/api/issues/DEV-7"},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

// A deletion is answered with nothing, so a 200 carrying anything at all is something other than the
// endpoint that was asked: a login page, a proxy, or an answer about another call entirely. Whatever answered,
// it answered 2xx to a deletion that went out, so the issue may be gone and the exit code is 2.
func TestIssueDeleteRefusesA200ThatCarriesABody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "a JSON object", contentType: "application/json", body: `{"x":1}`},
		{name: "a web page", contentType: "text/html", body: "<!doctype html>\n<html><body>Log in</body></html>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := deleting(t, answer(http.StatusOK, issueNamed("DEV-7")), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			})

			got := runWith(t, server.env(), "issue", "delete", "DEV-7")

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", "DELETE " + server.url + "/api/issues/DEV-7"},
					{"upstream_status", 200},
					{"upstream_body", tc.body},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

// The id that came back is sent straight out as a path segment, so it is held to the form ytrack sends
// before it goes: the generated client would resolve ".." against the endpoint and reach /api/.
func TestIssueDeleteRefusesAReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrived string
	}{
		{name: "two dots", arrived: `".."`},
		{name: "a path after the id", arrived: `"DEV-7/.."`},
		{name: "the id of an article", arrived: `"DEV-A-7"`},
		{name: "a number", arrived: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","idReadable":` + tc.arrived + `}`
			server := deleting(t, answer(http.StatusOK, body), noDeletion(t))

			got := runWith(t, server.env(), "issue", "delete", "dev-7")

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", issueRequest(server.url, "dev-7", deletedFields)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// The polygon has no such issue, and the read says so before a deletion is sent to a number nobody used.
func TestIssueDeleteRefusesAnIssueTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "delete", "DEV-99999")

	assert.Equal(t, noIssueToDelete(dev.url, "DEV-99999"), requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}

// An issue hidden from a token is no issue at all to it: the read answers the same 404 it answers for an
// issue nobody has, and DEV-1 is left where it stands.
func TestIssueDeleteSendsNoDeletionForAnIssueTheLimitedUserCannotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "issue", "delete", "DEV-1")

	assert.Equal(t, noIssueToDelete(dev.url, "DEV-1"), requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}

// sentMethods is the method of each request the server was sent, in order: what a scenario holds a deletion to
// is both which requests went out and which did not.
func sentMethods(u *upstream) []string {
	var methods []string
	for _, request := range u.requests() {
		methods = append(methods, request.Method)
	}
	return methods
}
