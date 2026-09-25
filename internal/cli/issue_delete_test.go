package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const deletedFields = "idReadable"

func issueNamed(readable string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `}`
}

func readThenDeletion(read, deletion http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletion(w, r)
			return
		}
		read(w, r)
	}
}

func deleting(t *testing.T, read, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, readThenDeletion(read, deletion))
}

func deletionDone() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
}

func noDeletion(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a deletion reached the server", "%s %s", r.Method, r.URL)
	}
}

func noIssueToDelete(address, id string) faultDocument {
	return faultDocument{
		code: "not_found",
		details: []detail{
			{"request", issueRequest(address, id, deletedFields)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id " + id + " not found"},
		},
	}
}

func entityNotFound(id string) string {
	return `{"error":"Not Found","error_description":"Entity with id ` + id + ` not found"}`
}

func TestIssueDeleteRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "two dots", argv: []string{"issue", "delete", ".."}},
		{name: "an article id", argv: []string{"issue", "delete", "DEV-A-1"}},
		{name: "an internal id", argv: []string{"issue", "delete", "3-26"}},
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

func TestIssueDeleteReadsTheIDAndDeletesByIt(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), deletionDone())

	got := runWith(t, server.Env(), "issue", "delete", "dev-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{"/api/issues/dev-7?fields=" + deletedFields, "/api/issues/DEV-7?"}, server.Targets())
	assert.Equal(t, "Bearer "+fake.Token, server.Last(t).Header.Get("Authorization"))
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestIssueDeleteRefusesAnIssueTheReadDoesNotFind(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusNotFound, entityNotFound("dev-7")), noDeletion(t))

	got := runWith(t, server.Env(), "issue", "delete", "dev-7")

	assert.Equal(t, noIssueToDelete(server.URL, "dev-7"), requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

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
			server := deleting(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), fake.JSON(tc.status, body))

			got := runWith(t, server.Env(), "issue", "delete", "DEV-7")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", "DELETE " + server.URL + "/api/issues/DEV-7"},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

func TestIssueDeleteRefusesA200ThatCarriesABody(t *testing.T) {
	t.Parallel()
	const body = `{"x":1}`
	server := deleting(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "issue", "delete", "DEV-7")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "DELETE " + server.URL + "/api/issues/DEV-7"},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
}

func TestIssueDeleteRefusesAReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","idReadable":"DEV-7/.."}`
	server := deleting(t, fake.JSON(http.StatusOK, body), noDeletion(t))

	got := runWith(t, server.Env(), "issue", "delete", "dev-7")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", issueRequest(server.URL, "dev-7", deletedFields)},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func sentMethods(u *fake.Server) []string {
	var methods []string
	for _, request := range u.Requests() {
		methods = append(methods, request.Method)
	}
	return methods
}
