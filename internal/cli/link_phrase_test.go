package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func everyDevInstancePhrase() []any {
	return []any{"depends on", "duplicates", "is duplicated by", "is required for", "parent for",
		"relates to", "subtask of", "Копия", "Скопирована в"}
}

func unknownPhraseFault(address, phrase string, nearest []any) faultDocument {
	return faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", issueRequest(address, addedSource, addSourceFields)},
			{"issue", addedSource},
			{"unknown", []any{[]detail{{"phrase", phrase}, {"nearest", nearest}}}},
		},
	}
}

func TestLinkAddNamesThePhrasesNearestTheOneNoLinkGoesBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phrase  string
		nearest []any
	}{
		{name: "a phrase a letter short", phrase: "depnds on", nearest: []any{"depends on"}},
		{
			name:    "a translation of that end a letter short",
			phrase:  "завист от",
			nearest: []any{"depends on"},
		},
		{
			name:    "a translation of that end two letters short",
			phrase:  "завист о",
			nearest: []any{"depends on"},
		},
		{
			name:    "the phrase itself with a space after it",
			phrase:  "depends on ",
			nearest: []any{"depends on"},
		},
		{name: "a phrase near none of them", phrase: "zzzzzzzzzz", nearest: everyDevInstancePhrase()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedTarget)

			assert.Equal(t, unknownPhraseFault(server.url, tc.phrase, tc.nearest), requireFault(t, got))
			assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
		})
	}
}

func issueLinksOfOnePhrase() (catalogueLink, catalogueLink) {
	mirror := linkKind{id: "9-1", sourceToTarget: phraseOf("mirrors"), targetToSource: phraseOf("mirrored by"),
		localizedSourceToTarget: phraseOf("Depends On"), localizedTargetToSource: phraseOf("зеркалит")}
	depend := devLinkKinds()[1]
	return catalogueLink{id: "9-1s", direction: "OUTWARD", kind: mirror},
		catalogueLink{id: depend.id + "t", direction: "INWARD", kind: depend}
}

func TestLinkAddResolvesThePhraseTwoSlotsAnswerToByItsOwnSpelling(t *testing.T) {
	t.Parallel()
	mirrors, depends := issueLinksOfOnePhrase()
	catalogue := issueLinksOf(addedSourceID, addedSource, mirrors, depends)

	t.Run("the phrase of the slot it is the phrase of", func(t *testing.T) {
		t.Parallel()
		server := linking(t, catalogue, addressedIssue(addedTargetID, addedTarget),
			respondWith(http.StatusOK, linkWritten(depends)))

		got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedTarget)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Equal(t, "/api/issues/"+addedSource+"/links/"+depends.id+"/issues", server.sentPaths()[2])
	})

	t.Run("the same phrase in another letter case", func(t *testing.T) {
		t.Parallel()
		server := linking(t, catalogue, addressedIssue(addedTargetID, addedTarget), noLinkWritten(t))

		got := runWith(t, server.env(), "link", "add", addedSource, "DEPENDS ON", addedTarget)

		assert.Equal(t, unknownPhraseFault(server.url, "DEPENDS ON", []any{"depends on", "mirrors"}),
			requireFault(t, got))
		assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
	})
}

func TestLinkAddRefusesTwoSlotsUnderOnePhraseAndWritesEveryOther(t *testing.T) {
	t.Parallel()
	directed := linkKind{id: "9-1", sourceToTarget: phraseOf("X"), targetToSource: phraseOf("Y"),
		localizedSourceToTarget: phraseOf(""), localizedTargetToSource: phraseOf("")}
	undirected := linkKind{id: "9-2", sourceToTarget: phraseOf("X"), targetToSource: phraseOf(""),
		localizedSourceToTarget: phraseOf(""), localizedTargetToSource: phraseOf("")}
	depend := devIssueLink(t, "163-1t")
	catalogue := issueLinksOf(addedSourceID, addedSource,
		catalogueLink{id: "9-1s", direction: "OUTWARD", kind: directed},
		catalogueLink{id: "9-2", direction: "BOTH", kind: undirected},
		depend)

	t.Run("the phrase both of them are printed under", func(t *testing.T) {
		t.Parallel()
		server := linking(t, catalogue, addressedIssue(addedTargetID, addedTarget), noLinkWritten(t))

		got := runWith(t, server.env(), "link", "add", addedSource, "X", addedTarget)

		assert.Equal(t, faultDocument{
			code: "upstream_invalid",
			details: []detail{
				{"request", issueRequest(server.url, addedSource, addSourceFields)},
				{"issue", addedSource},
				{"phrase", "X"},
			},
		}, requireFault(t, got))
		assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
	})

	t.Run("a phrase of that same issue one slot goes by", func(t *testing.T) {
		t.Parallel()
		server := linking(t, catalogue, addressedIssue(addedTargetID, addedTarget),
			respondWith(http.StatusOK, linkWritten(depend)))

		got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedTarget)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Equal(t, "/api/issues/"+addedSource+"/links/"+depend.id+"/issues", server.sentPaths()[2])
		assert.Equal(t, []string{"depends on"},
			keysOf(nodeAt(t, requireMapping(t, "stdout", got.stdout), "links")))
	})
}

func TestLinkAddNeverResolvesAnEndTheTypeLeavesUnnamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		targetToSource phrase
	}{
		{name: "an end the type names with an empty string", targetToSource: phraseOf("")},
		{name: "an end the type names with null", targetToSource: phrase{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			relates := devLinkKinds()[0]
			nameless := linkKind{id: "9-1", sourceToTarget: phraseOf("is required for"), targetToSource: tc.targetToSource,
				localizedSourceToTarget: phraseOf("обязательна для"), localizedTargetToSource: phraseOf("зависит от")}
			catalogue := issueLinksOf(addedSourceID, addedSource,
				catalogueLink{id: relates.id, direction: "BOTH", kind: relates},
				catalogueLink{id: "9-1t", direction: "INWARD", kind: nameless})
			server := linking(t, catalogue, addressedIssue(addedTargetID, addedTarget), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, "зависит от", addedTarget)

			assert.Equal(t, unknownPhraseFault(server.url, "зависит от", []any{"relates to"}),
				requireFault(t, got))
			assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
		})
	}
}

func TestLinkAddRefusesANameOfAnEndThatIsNeitherTextNorNull(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind linkKind
	}{
		{
			name: "the phrase of the end as a number",
			kind: linkKind{id: "9-1", sourceToTarget: sentAs("42"), targetToSource: phraseOf("depends on"),
				localizedSourceToTarget: phraseOf("обязательна для"), localizedTargetToSource: phraseOf("зависит от")},
		},
		{
			name: "the translation of the end as a JSON object",
			kind: linkKind{id: "9-1", sourceToTarget: phraseOf("is required for"), targetToSource: phraseOf("depends on"),
				localizedSourceToTarget: sentAs(`{"ru":"обязательна для"}`), localizedTargetToSource: phraseOf("зависит от")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, issueLinksOf(addedSourceID, addedSource,
				catalogueLink{id: "9-1s", direction: "OUTWARD", kind: tc.kind}),
				addressedIssue(addedTargetID, addedTarget), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, "is required for", addedTarget)

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, detail{"request", issueRequest(server.url, addedSource, addSourceFields)},
				found.details[0])
			assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
		})
	}
}

func TestLinkAddRefusesASlotAddressedAgainstItsOwnEnd(t *testing.T) {
	t.Parallel()
	relates, depend := devLinkKinds()[0], devLinkKinds()[1]
	tests := []struct {
		name   string
		link   catalogueLink
		phrase string
	}{
		{
			name:   "the end at the source of a type addressed with no suffix at all",
			link:   catalogueLink{id: "42-1", direction: "OUTWARD", kind: depend},
			phrase: "is required for",
		},
		{
			name:   "the end at the target of a type addressed as the one at its source",
			link:   catalogueLink{id: "42-1s", direction: "INWARD", kind: depend},
			phrase: "depends on",
		},
		{
			name:   "an undirected type addressed as the target end of a directed one",
			link:   catalogueLink{id: "42-0t", direction: "BOTH", kind: relates},
			phrase: "relates to",
		},
		{
			name:   "an id that would reach an endpoint other than the slot",
			link:   catalogueLink{id: "..", direction: "BOTH", kind: relates},
			phrase: "relates to",
		},
		{
			name:   "a suffix of the right end on a path that climbs out of the slots",
			link:   catalogueLink{id: "../../hub-1t", direction: "INWARD", kind: depend},
			phrase: "depends on",
		},
		{
			name:   "an id whose number before the dash is no number",
			link:   catalogueLink{id: "hub-1t", direction: "INWARD", kind: depend},
			phrase: "depends on",
		},
		{
			name:   "the suffix of the source end in upper case",
			link:   catalogueLink{id: "42-1S", direction: "OUTWARD", kind: depend},
			phrase: "is required for",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, issueLinksOf(addedSourceID, addedSource, tc.link),
				addressedIssue(addedTargetID, addedTarget), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedTarget)

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, addedSource, addSourceFields)},
					{"issue", addedSource},
					{"phrase", tc.phrase},
				},
			}, requireFault(t, got))
			assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
		})
	}
}

func TestLinkAddRefusesLinkingAnIssueToItself(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
	}{
		{name: "the same id twice", target: addedSource},
		{name: "the same issue in another letter case", target: strings.ToLower(addedSource)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					assert.Fail(t, "a write reached the server", "%s %s", r.Method, r.URL)
					return
				}
				respondWith(http.StatusOK, devInstanceCatalogue())(w, r)
			})

			got := runWith(t, server.env(), "link", "add", addedSource, "relates to", tc.target)

			assert.Equal(t, faultDocument{
				code: "bad_usage",
				details: []detail{
					{"request", issueRequest(server.url, tc.target, addTargetFields)},
					{"issue", addedSource},
					{"target", addedSource},
				},
			}, requireFault(t, got))
			assert.Equal(t, []string{"/api/issues/" + addedSource, "/api/issues/" + tc.target}, server.sentPaths())
		})
	}
}

func TestLinkAddRefusesAReadThatNamesAnIssueByNeitherOfItsIDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		target string
		read   string
		fields string
		paths  []string
	}{
		{
			name:   "the issue written on by a readable id that would reach another endpoint",
			source: issueLinksOf(addedSourceID, "..", devIssueLinks()...),
			target: addressedIssue(addedTargetID, addedTarget),
			read:   addedSource,
			fields: addSourceFields,
			paths:  []string{"/api/issues/" + addedSource},
		},
		{
			name:   "the issue written on by an id that is no internal id",
			source: issueLinksOf("x", addedSource, devIssueLinks()...),
			target: addressedIssue(addedTargetID, addedTarget),
			read:   addedSource,
			fields: addSourceFields,
			paths:  []string{"/api/issues/" + addedSource},
		},
		{
			name:   "the issue at the other end by the id it is addressed by rather than the internal one",
			source: devInstanceCatalogue(),
			target: addressedIssue(addedTarget, addedTarget),
			read:   addedTarget,
			fields: addTargetFields,
			paths:  []string{"/api/issues/" + addedSource, "/api/issues/" + addedTarget},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, tc.source, tc.target, noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedTarget)

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, detail{"request", issueRequest(server.url, tc.read, tc.fields)}, found.details[0])
			assert.Equal(t, []string{"upstream_status", "upstream_body"},
				[]string{found.details[1].key, found.details[2].key})
			assert.Equal(t, tc.paths, server.sentPaths())
		})
	}
}
