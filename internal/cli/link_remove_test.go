package cli_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const removedLinkPath = "/api/issues/DEV-1/links/5-1t/issues/3-2"

func removalNames() []detail {
	return []detail{{"issue", "DEV-1"}, {"phrase", "needs"}, {"target", "DEV-2"}}
}

func TestLinkRemoveRefusesACallThatNamesNoOneLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an issue that would reach another endpoint", argv: []string{"link", "remove", "..", "needs", "DEV-2"}},
		{name: "a target issue that is an article", argv: []string{"link", "remove", "DEV-1", "needs", "DEV-A-1"}},
		{name: "an empty phrase", argv: []string{"link", "remove", "DEV-1", "", "DEV-2"}},
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

func TestLinkRemoveTakesTheLinkAwayBySlotAndInternalID(t *testing.T) {
	t.Parallel()
	server := linking(t, deletionDone())

	got := runWith(t, server.Env(), "link", "remove", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\nremoved:\n  \"needs\":\n    - {idReadable: \"DEV-2\"}\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		"/api/issues/DEV-1?fields=" + addSourceFields,
		"/api/issues/DEV-2?fields=" + addTargetFields,
		removedLinkPath + "?",
	}, server.Targets())
	assert.Equal(t, []string{"", "", ""}, server.Bodies(), "no request of a removal carries a body")
}

func TestLinkRemoveRefusesALinkTheIssueDoesNotHave(t *testing.T) {
	t.Parallel()
	server := linking(t, fake.JSON(http.StatusNotFound, entityNotFound("3-2")))

	got := runWith(t, server.Env(), "link", "remove", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, faultDocument{
		code: "not_found",
		details: append([]detail{{"request", "DELETE " + server.URL + removedLinkPath}},
			append(removalNames(),
				detail{"upstream_status", 404},
				detail{"upstream_error", "Not Found"},
				detail{"upstream_message", "Entity with id 3-2 not found"})...),
	}, requireFault(t, got))
}

func TestLinkRemoveIsUncertainWhereTheAnswerIsNotTheServersOwn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		removal http.HandlerFunc
		code    string
	}{
		{name: "a JSON object under a 200", removal: body("application/json", `{"x":1}`), code: "upstream_invalid"},
		{
			name:    "a web page under a 200",
			removal: body("text/html", "<!doctype html>\n<html><body>Log in</body></html>"),
			code:    "upstream_invalid",
		},
		{name: "an answer that never came", removal: breakOff, code: "write_uncertain"},
		{name: "an answer from a gateway", removal: gateway(http.StatusBadGateway), code: "write_uncertain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, tc.removal)

			got := runWith(t, server.Env(), "link", "remove", "DEV-1", "needs", "DEV-2")

			found := requireUncertainty(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, removalNames(), found.details[1:4])
			assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

func body(contentType, text string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, text)
	}
}
