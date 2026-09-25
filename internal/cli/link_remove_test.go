package cli_test

import (
	"io"
	"net/http"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const removedIssueLink = "163-1t"

func removing(t *testing.T, catalogue, target string, removal http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			removal(w, r)
		case path.Base(r.URL.Path) == addedSource:
			fake.JSON(http.StatusOK, catalogue)(w, r)
		case path.Base(r.URL.Path) == addedTarget:
			fake.JSON(http.StatusOK, target)(w, r)
		default:
			assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
		}
	})
}

func removingOnTheDevInstance(t *testing.T, removal http.HandlerFunc) *fake.Server {
	t.Helper()
	return removing(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget), removal)
}

func noRemoval(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a removal reached the server", "%s %s", r.Method, r.URL)
	}
}

func removalRequest(address string) string {
	return "DELETE " + address + "/api/issues/" + addedSource + "/links/" + removedIssueLink + "/issues/" + addedTargetID
}

func removalNames(phrase string) []detail {
	return []detail{{"issue", addedSource}, {"phrase", phrase}, {"target", addedTarget}}
}

func TestLinkRemoveRefusesACallThatNamesNoOneLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an issue that would reach another endpoint", argv: []string{"link", "remove", "..", "depends on", "DEV-2"}},
		{name: "a target issue that is an article", argv: []string{"link", "remove", "DEV-1", "depends on", "DEV-A-1"}},
		{name: "an empty phrase", argv: []string{"link", "remove", "DEV-1", "", "DEV-2"}},
		{name: "a phrase that is no text", argv: []string{"link", "remove", "DEV-1", "\xff", "DEV-2"}},
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
	server := removingOnTheDevInstance(t, deletionDone())

	got := runWith(t, server.Env(), "link", "remove", addedSource, "depends on", addedTarget)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "idReadable: \""+addedSource+"\"\nremoved:\n  \"depends on\":\n    - {idReadable: \""+
		addedTarget+"\"}\n", got.stdout)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		"/api/issues/" + addedSource,
		"/api/issues/" + addedTarget,
		"/api/issues/" + addedSource + "/links/" + removedIssueLink + "/issues/" + addedTargetID,
	}, server.Paths())
	assert.Equal(t, []string{"", "", ""}, server.Bodies(), "no request of a removal carries a body")
	assert.Equal(t, []string{addSourceFields, addTargetFields, ""}, server.Fields())
	requests := server.Requests()
	require.Len(t, requests, 3)
	assert.Empty(t, requests[2].URL.RawQuery, "a removal asks for nothing")
}

func TestLinkRemovePrintsThePhraseOfTheSlotRatherThanTheOneWritten(t *testing.T) {
	t.Parallel()
	const written = "ЗАВИСИТ ОТ"

	t.Run("the document of the link that is gone", func(t *testing.T) {
		t.Parallel()
		server := removingOnTheDevInstance(t, deletionDone())

		got := runWith(t, server.Env(), "link", "remove", addedSource, written, addedTarget)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Equal(t, []detail{
			{"idReadable", addedSource},
			{"removed", []detail{{"depends on", []any{[]detail{{"idReadable", addedTarget}}}}}},
		}, requireDocument(t, got.stdout))
		assert.NotContains(t, got.stdout, written)
	})

	t.Run("the refusal about a link the issue holds none of", func(t *testing.T) {
		t.Parallel()
		server := removingOnTheDevInstance(t, fake.JSON(http.StatusNotFound, entityNotFound(addedTargetID)))

		got := runWith(t, server.Env(), "link", "remove", addedSource, written, addedTarget)

		found := requireFault(t, got)
		assert.Equal(t, "not_found", found.code)
		assert.Equal(t, removalNames("depends on"), found.details[1:4])
	})
}

func TestLinkRemoveRefusesALinkTheIssueDoesNotHave(t *testing.T) {
	t.Parallel()
	server := removingOnTheDevInstance(t, fake.JSON(http.StatusNotFound, entityNotFound(addedTargetID)))

	got := runWith(t, server.Env(), "link", "remove", addedSource, "depends on", addedTarget)

	assert.Equal(t, faultDocument{
		code: "not_found",
		details: append([]detail{{"request", removalRequest(server.URL)}},
			append(removalNames("depends on"),
				detail{"upstream_status", 404},
				detail{"upstream_error", "Not Found"},
				detail{"upstream_message", "Entity with id " + addedTargetID + " not found"})...),
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
			server := removingOnTheDevInstance(t, tc.removal)

			got := runWith(t, server.Env(), "link", "remove", addedSource, "depends on", addedTarget)

			found := requireUncertainty(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, removalNames("depends on"), found.details[1:4])
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

func TestLinkRemoveRefusesBeforeTheRemovalTheWayAddDoes(t *testing.T) {
	t.Parallel()
	depend := devLinkKinds()[1]
	tests := []struct {
		name      string
		catalogue string
		phrase    string
		target    string
		want      func(address string) faultDocument
		paths     []string
	}{
		{
			name:      "a phrase no link of the issue goes by",
			catalogue: devInstanceCatalogue(),
			phrase:    "depnds on",
			target:    addedTarget,
			want: func(address string) faultDocument {
				return unknownPhraseFault(address, "depnds on", []any{"depends on"})
			},
			paths: []string{"/api/issues/" + addedSource},
		},
		{
			name: "a slot addressed against the end the answer put it at",
			catalogue: issueLinksOf(addedSourceID, addedSource,
				catalogueLink{id: "42-1s", direction: "INWARD", kind: depend}),
			phrase: "depends on",
			target: addedTarget,
			want: func(address string) faultDocument {
				return faultDocument{
					code: "upstream_invalid",
					details: []detail{
						{"request", issueRequest(address, addedSource, addSourceFields)},
						{"issue", addedSource},
						{"phrase", "depends on"},
					},
				}
			},
			paths: []string{"/api/issues/" + addedSource},
		},
		{
			name:      "the issue and the target issue being one issue",
			catalogue: devInstanceCatalogue(),
			phrase:    "relates to",
			target:    addedSource,
			want: func(address string) faultDocument {
				return faultDocument{
					code: "bad_usage",
					details: []detail{
						{"request", issueRequest(address, addedSource, addTargetFields)},
						{"issue", addedSource},
						{"target", addedSource},
					},
				}
			},
			paths: []string{"/api/issues/" + addedSource, "/api/issues/" + addedSource},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removing(t, tc.catalogue, addressedIssue(addedTargetID, addedTarget), noRemoval(t))

			got := runWith(t, server.Env(), "link", "remove", addedSource, tc.phrase, tc.target)

			assert.Equal(t, tc.want(server.URL), requireFault(t, got))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}
