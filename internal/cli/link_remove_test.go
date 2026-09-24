package cli_test

import (
	"io"
	"net/http"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The slot of the polygon a scenario of link remove works on: the end an issue that waits stands at.
const removedSlot = "163-1t"

// removing is the server of a link remove: each read answered by the id it goes out to, the removal itself by
// the handler given.
func removing(t *testing.T, catalogue, partner string, removal http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			removal(w, r)
		case path.Base(r.URL.Path) == addedSource:
			answer(http.StatusOK, catalogue)(w, r)
		case path.Base(r.URL.Path) == addedPartner:
			answer(http.StatusOK, partner)(w, r)
		default:
			assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
		}
	})
}

// removingOnThePolygon is that server with the catalogue of the polygon behind both reads.
func removingOnThePolygon(t *testing.T, removal http.HandlerFunc) *upstream {
	t.Helper()
	return removing(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner), removal)
}

// noRemoval stands for the request a refusal before the removal must not send.
func noRemoval(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a removal reached the server", "%s %s", r.Method, r.URL)
	}
}

// The request a removal goes out as: the issue by the readable id the read gave, the slot by the id the server
// addresses it by, and the partner by the internal id, which is the one form YouTrack takes in that segment.
func removalRequest(address string) string {
	return "DELETE " + address + "/api/issues/" + addedSource + "/links/" + removedSlot + "/issues/" + addedPartnerID
}

// The three names of the link every refusal about a removal carries: they mean the same on any instance, while
// the id of the slot and the internal id of the partner stand in request and in upstream_* alone.
func removalNames(phrase string) []detail {
	return []detail{{"issue", addedSource}, {"phrase", phrase}, {"partner", addedPartner}}
}

// Every way of writing link remove that names no one link, refused before any request: the arity, the form
// of either id and a phrase that matches nothing there is to match. A removal prints what it took away and
// nothing else, so an expression is a flag the command has none of.
func TestLinkRemoveRefusesACallThatNamesNoOneLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: []string{"link", "remove"}},
		{name: "no partner", argv: []string{"link", "remove", "DEV-1", "depends on"}},
		{name: "a fourth word", argv: []string{"link", "remove", "DEV-1", "depends on", "DEV-2", "DEV-3"}},
		{name: "an issue that would reach another endpoint", argv: []string{"link", "remove", "..", "depends on", "DEV-2"}},
		{name: "a partner that is an article", argv: []string{"link", "remove", "DEV-1", "depends on", "DEV-A-1"}},
		{name: "an empty phrase", argv: []string{"link", "remove", "DEV-1", "", "DEV-2"}},
		{name: "a phrase that is no text", argv: []string{"link", "remove", "DEV-1", "\xff", "DEV-2"}},
		{
			name: "an expression of what to print",
			argv: []string{"link", "remove", "DEV-1", "depends on", "DEV-2", "--fields", "idReadable"},
		},
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

// The help promises nothing to ask for: what a removal prints is the link it took away, and a flag naming
// what a partner is printed by would promise a partner that is no longer at the other end of anything.
func TestLinkRemoveHelpPromisesNoExpression(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"link", "remove", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "ytrack link remove")
	assert.NotContains(t, got.stdout, "--fields")
}

// The whole of the command: the phrase is resolved against the read of the issue, the removal goes out to
// the slot that read gave with the partner addressed by its internal id, and it carries neither a body nor a
// query. What it prints is the link that is gone, in the shape link list prints links in.
func TestLinkRemoveTakesTheLinkAwayBySlotAndInternalID(t *testing.T) {
	t.Parallel()
	server := removingOnThePolygon(t, deletionDone())

	got := runWith(t, server.env(), "link", "remove", addedSource, "depends on", addedPartner)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, "idReadable: \""+addedSource+"\"\nremoved:\n  \"depends on\":\n    - {idReadable: \""+
		addedPartner+"\"}\n", got.stdout)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		"/api/issues/" + addedSource,
		"/api/issues/" + addedPartner,
		"/api/issues/" + addedSource + "/links/" + removedSlot + "/issues/" + addedPartnerID,
	}, server.sentPaths())
	assert.Equal(t, []string{"", "", ""}, server.asks(), "no request of a removal carries a body")
	assert.Equal(t, []string{addSourceFields, addPartnerFields, ""}, server.sentFields())
	requests := server.requests()
	require.Len(t, requests, 3)
	assert.Empty(t, requests[2].URL.RawQuery, "a removal asks for nothing")
}

// A phrase is resolved in any letter case and against the translation of the end as well, and what is
// printed afterwards is the phrase of the slot rather than the spelling that reached it: the key of removed is
// the key link list prints the link under, so a reader who looks the link up by it finds it, and the phrase of
// a refusal is the one the link is addressed by rather than the one the caller happened to type.
func TestLinkRemovePrintsThePhraseOfTheSlotRatherThanTheOneWritten(t *testing.T) {
	t.Parallel()
	// The translation of that end in upper case: neither the letter case nor the language of the canonical
	// phrase.
	const written = "ЗАВИСИТ ОТ"

	t.Run("the document of the link that is gone", func(t *testing.T) {
		t.Parallel()
		server := removingOnThePolygon(t, deletionDone())

		got := runWith(t, server.env(), "link", "remove", addedSource, written, addedPartner)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Equal(t, []detail{
			{"idReadable", addedSource},
			{"removed", []detail{{"depends on", []any{[]detail{{"idReadable", addedPartner}}}}}},
		}, requireDocument(t, got.stdout))
		assert.NotContains(t, got.stdout, written)
	})

	t.Run("the refusal about a link the issue holds none of", func(t *testing.T) {
		t.Parallel()
		server := removingOnThePolygon(t, answer(http.StatusNotFound, entityNotFound(addedPartnerID)))

		got := runWith(t, server.env(), "link", "remove", addedSource, written, addedPartner)

		found := requireRefusal(t, got)
		assert.Equal(t, "not_found", found.code)
		assert.Equal(t, removalNames("depends on"), found.details[1:4])
	})
}

func TestLinkRemoveRefusesALinkTheIssueDoesNotHold(t *testing.T) {
	t.Parallel()
	server := removingOnThePolygon(t, answer(http.StatusNotFound, entityNotFound(addedPartnerID)))

	got := runWith(t, server.env(), "link", "remove", addedSource, "depends on", addedPartner)

	assert.Equal(t, refusal{
		code: "not_found",
		details: append([]detail{{"request", removalRequest(server.url)}},
			append(removalNames("depends on"),
				detail{"upstream_status", 404},
				detail{"upstream_error", "Not Found"},
				detail{"upstream_message", "Entity with id " + addedPartnerID + " not found"})...),
	}, requireRefusal(t, got))
}

// A removal is answered with nothing at all, so anything under a 200 is something other than the endpoint
// that was asked; and an answer that never came or came from a gateway leaves the caller with a call they
// cannot simply send again. Either way the request went out, so the exit code says the instance may have moved.
func TestLinkRemoveIsUncertainWhereTheAnswerIsNotTheServersOwn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		removal http.HandlerFunc
		code    string
	}{
		{name: "a JSON object under a 200", removal: body("application/json", `{"x":1}`), code: "upstream_lied"},
		{
			name:    "a web page under a 200",
			removal: body("text/html", "<!doctype html>\n<html><body>Log in</body></html>"),
			code:    "upstream_lied",
		},
		{name: "an answer that never came", removal: breakOff, code: "write_uncertain"},
		{name: "an answer from a gateway", removal: gateway(http.StatusBadGateway), code: "write_uncertain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removingOnThePolygon(t, tc.removal)

			got := runWith(t, server.env(), "link", "remove", addedSource, "depends on", addedPartner)

			found := requireUncertainty(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, removalNames("depends on"), found.details[1:4])
			assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

// body is an answer of that content type and that text, which is what a removal is never answered with.
func body(contentType, text string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, text)
	}
}

// A removal resolves its phrase against the same read a write does and is held to the same guards, so
// every refusal that settles a link before anything goes out reads on remove as it reads on add: the same
// code and the same details, and no request past the reads that settled it.
func TestLinkRemoveRefusesBeforeTheRemovalTheWayAddDoes(t *testing.T) {
	t.Parallel()
	depend := devLinkKinds()[1]
	tests := []struct {
		name      string
		catalogue string
		phrase    string
		partner   string
		want      func(address string) refusal
		paths     []string
	}{
		{
			name:      "a phrase no link of the issue goes by",
			catalogue: polygonCatalogue(),
			phrase:    "depnds on",
			partner:   addedPartner,
			want: func(address string) refusal {
				return unknownPhraseRefusal(address, "depnds on", []any{"depends on"})
			},
			paths: []string{"/api/issues/" + addedSource},
		},
		{
			name: "a slot addressed against the end the answer put it at",
			catalogue: slotsOf(addedSourceID, addedSource,
				catalogueSlot{id: "42-1s", direction: "INWARD", kind: depend}),
			phrase:  "depends on",
			partner: addedPartner,
			want: func(address string) refusal {
				return refusal{
					code: "upstream_lied",
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
			name:      "the issue and the partner being one issue",
			catalogue: polygonCatalogue(),
			phrase:    "relates to",
			partner:   addedSource,
			want: func(address string) refusal {
				return refusal{
					code: "bad_usage",
					details: []detail{
						{"request", issueRequest(address, addedSource, addPartnerFields)},
						{"issue", addedSource},
						{"partner", addedSource},
					},
				}
			},
			paths: []string{"/api/issues/" + addedSource, "/api/issues/" + addedSource},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := removing(t, tc.catalogue, addressedIssue(addedPartnerID, addedPartner), noRemoval(t))

			got := runWith(t, server.env(), "link", "remove", addedSource, tc.phrase, tc.partner)

			assert.Equal(t, tc.want(server.url), requireRefusal(t, got))
			assert.Equal(t, tc.paths, server.sentPaths())
		})
	}
}

// A link of two issues of the contract test's own, written and then taken away on the polygon: both ends
// go together, the issue the call was made on is left holding nothing, and the same call sent again is
// answered not_found, because by then there is no link to take away.
func TestLinkRemoveOnTheDevInstanceUnlinksBothIssuesAndThenFindsNothing(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "source")
	partner := aContractIssue(t, dev, "partner")

	filed := runWith(t, dev.env(), "link", "add", source, "depends on", partner)
	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{partner}, partnersUnder(t, filed.stdout, "depends on"))

	sent := len(dev.requests())
	got := runWith(t, dev.env(), "link", "remove", source, "depends on", partner)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{
		{"idReadable", source},
		{"removed", []detail{{"depends on", []any{[]detail{{"idReadable", partner}}}}}},
	}, requireDocument(t, got.stdout))
	// The removal was addressed by the slot the read before it sent, at the end the phrase names.
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(dev)[sent:])
	assert.Regexp(t, `^[0-9]+-[0-9]+t$`, path.Base(path.Dir(path.Dir(dev.sentPaths()[sent+2]))))

	// Neither issue is left holding half a link: the end the call never named went away with the one it did.
	for _, issue := range []string{source, partner} {
		held := runWith(t, dev.env(), "link", "list", issue)

		require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
		assert.Equal(t, []detail{{"total", 0}, {"returned", 0}, {"truncated", false}},
			requireDocument(t, held.stdout)[:3], issue)
		assert.Empty(t, keysOf(nodeAt(t, requireMapping(t, "stdout", held.stdout), "links")), issue)
	}

	again := runWith(t, dev.env(), "link", "remove", source, "depends on", partner)

	found := requireRefusal(t, again)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, []detail{{"issue", source}, {"phrase", "depends on"}, {"partner", partner}}, found.details[1:4])
	assert.Equal(t, 404, detailNamed(t, found, "upstream_status"))
}

// A link is taken away from the end the phrase names, and the phrase of the other end names an end this
// issue does not stand at: the server answers 404 for it as it does for a link nobody ever wrote, and the link
// itself is left where it was.
func TestLinkRemoveOnTheDevInstanceLeavesTheLinkNamedFromTheEndItIsNotAt(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "the one that waits")
	partner := aContractIssue(t, dev, "the one that blocks")

	filed := runWith(t, dev.env(), "link", "add", source, "depends on", partner)
	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{partner}, partnersUnder(t, filed.stdout, "depends on"))

	got := runWith(t, dev.env(), "link", "remove", source, "is required for", partner)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, []detail{{"issue", source}, {"phrase", "is required for"}, {"partner", partner}},
		found.details[1:4])

	held := runWith(t, dev.env(), "link", "list", source)

	require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
	assert.Equal(t, []string{partner}, partnersUnder(t, held.stdout, "depends on"))
}

// One link read from its two ends is one link: naming it from the issue at the other end, under the
// phrase of that end, takes away the very link the first issue was linked by, and both are left holding
// nothing.
func TestLinkRemoveOnTheDevInstanceTakesTheLinkAwayFromEitherEnd(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "the one that waits")
	partner := aContractIssue(t, dev, "the one that blocks")

	filed := runWith(t, dev.env(), "link", "add", source, "depends on", partner)
	require.Equal(t, 0, filed.code, "stderr: %s", filed.stderr)
	require.Equal(t, []string{partner}, partnersUnder(t, filed.stdout, "depends on"))

	got := runWith(t, dev.env(), "link", "remove", partner, "is required for", source)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{
		{"idReadable", partner},
		{"removed", []detail{{"is required for", []any{[]detail{{"idReadable", source}}}}}},
	}, requireDocument(t, got.stdout))

	for _, issue := range []string{source, partner} {
		held := runWith(t, dev.env(), "link", "list", issue)

		require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
		assert.Equal(t, []detail{{"total", 0}, {"returned", 0}, {"truncated", false}},
			requireDocument(t, held.stdout)[:3], issue)
		assert.Empty(t, keysOf(nodeAt(t, requireMapping(t, "stdout", held.stdout), "links")), issue)
	}
}
