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

// unknownPhraseRefusal is the whole document a phrase no link goes by is refused with: the read that settled
// it, the issue the phrases were read off, and the phrase beside the ones nearest it.
func unknownPhraseRefusal(address, phrase string, nearest []any) faultDocument {
	return faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", issueRequest(address, addedSource, addSourceFields)},
			{"issue", addedSource},
			{"unknown", []any{[]detail{{"phrase", phrase}, {"nearest", nearest}}}},
		},
	}
}

// A phrase no link of the issue goes by is the caller's own to fix, so what they are handed is the
// phrases of that very issue: the ones within two edits of what they wrote, measured against the translations
// as well and shown as link list prints them, or every phrase there is where they wrote nothing near one.
func TestLinkAddNamesThePhrasesNearestTheOneNoLinkGoesBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phrase  string
		nearest []any
	}{
		{name: "a phrase a letter short", phrase: "depnds on", nearest: []any{"depends on"}},
		{
			// The translation is what the phrase is measured against; the phrase link list prints is what
			// comes back, since a caller handed a translation would write it back and be answered the same way.
			name:    "a translation of that end a letter short",
			phrase:  "завист от",
			nearest: []any{"depends on"},
		},
		{
			// Two letters off a translation written in Cyrillic, which is four bytes off it: the edits are
			// counted in letters, so a caller writing a language of two bytes to the letter is measured the
			// same way as one writing the phrase itself.
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
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedPartner)

			assert.Equal(t, unknownPhraseRefusal(server.url, tc.phrase, tc.nearest), requireRefusal(t, got))
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
		server := linking(t, catalogue, addressedIssue(addedPartnerID, addedPartner),
			respondWith(http.StatusOK, linkWritten(depends)))

		got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Equal(t, "/api/issues/"+addedSource+"/links/"+depends.id+"/issues", server.sentPaths()[2])
	})

	t.Run("the same phrase in another letter case", func(t *testing.T) {
		t.Parallel()
		server := linking(t, catalogue, addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

		got := runWith(t, server.env(), "link", "add", addedSource, "DEPENDS ON", addedPartner)

		assert.Equal(t, unknownPhraseRefusal(server.url, "DEPENDS ON", []any{"depends on", "mirrors"}),
			requireRefusal(t, got))
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
		server := linking(t, catalogue, addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

		got := runWith(t, server.env(), "link", "add", addedSource, "X", addedPartner)

		assert.Equal(t, faultDocument{
			code: "upstream_invalid",
			details: []detail{
				{"request", issueRequest(server.url, addedSource, addSourceFields)},
				{"issue", addedSource},
				{"phrase", "X"},
			},
		}, requireRefusal(t, got))
		assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
	})

	t.Run("a phrase of that same issue one slot goes by", func(t *testing.T) {
		t.Parallel()
		server := linking(t, catalogue, addressedIssue(addedPartnerID, addedPartner),
			respondWith(http.StatusOK, linkWritten(depend)))

		got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

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
			server := linking(t, catalogue, addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, "зависит от", addedPartner)

			assert.Equal(t, unknownPhraseRefusal(server.url, "зависит от", []any{"relates to"}),
				requireRefusal(t, got))
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
				addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, "is required for", addedPartner)

			found := requireRefusal(t, got)
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
				addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedPartner)

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, addedSource, addSourceFields)},
					{"issue", addedSource},
					{"phrase", tc.phrase},
				},
			}, requireRefusal(t, got))
			assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
		})
	}
}

// An issue is no partner of its own: YouTrack answers a link of an issue to itself with a 200 and writes
// nothing at all. Which two arguments name one issue is only known once both have been read, so the refusal
// names the read that settled it and the write never goes out.
func TestLinkAddRefusesLinkingAnIssueToItself(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		partner string
	}{
		{name: "the same id twice", partner: addedSource},
		{name: "the same issue in another letter case", partner: strings.ToLower(addedSource)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// One issue answers both reads, whichever of its ids the argument was written as.
			server := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					assert.Fail(t, "a write reached the server", "%s %s", r.Method, r.URL)
					return
				}
				respondWith(http.StatusOK, devInstanceCatalogue())(w, r)
			})

			got := runWith(t, server.env(), "link", "add", addedSource, "relates to", tc.partner)

			assert.Equal(t, faultDocument{
				code: "bad_usage",
				details: []detail{
					{"request", issueRequest(server.url, tc.partner, addPartnerFields)},
					{"issue", addedSource},
					{"partner", addedSource},
				},
			}, requireRefusal(t, got))
			assert.Equal(t, []string{"/api/issues/" + addedSource, "/api/issues/" + tc.partner}, server.sentPaths())
		})
	}
}

// Both ids of an issue go out again before anything is written: the readable one becomes a path segment
// and the internal one becomes the body, so a read that gives either of them in a shape ytrack does not send
// ends the call where it arrived rather than at an endpoint nobody named.
func TestLinkAddRefusesAReadThatNamesAnIssueByNeitherOfItsIDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		source  string
		partner string
		// The read the refusal names: the issue it went out to and what it asked of that issue.
		read   string
		fields string
		paths  []string
	}{
		{
			name:    "the issue written on by a readable id that would reach another endpoint",
			source:  issueLinksOf(addedSourceID, "..", devIssueLinks()...),
			partner: addressedIssue(addedPartnerID, addedPartner),
			read:    addedSource,
			fields:  addSourceFields,
			paths:   []string{"/api/issues/" + addedSource},
		},
		{
			name:    "the issue written on by an id that is no internal id",
			source:  issueLinksOf("x", addedSource, devIssueLinks()...),
			partner: addressedIssue(addedPartnerID, addedPartner),
			read:    addedSource,
			fields:  addSourceFields,
			paths:   []string{"/api/issues/" + addedSource},
		},
		{
			name:    "the issue at the other end by the id it is addressed by rather than the internal one",
			source:  devInstanceCatalogue(),
			partner: addressedIssue(addedPartner, addedPartner),
			read:    addedPartner,
			fields:  addPartnerFields,
			paths:   []string{"/api/issues/" + addedSource, "/api/issues/" + addedPartner},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, tc.source, tc.partner, noLinkWritten(t))

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, detail{"request", issueRequest(server.url, tc.read, tc.fields)}, found.details[0])
			assert.Equal(t, []string{"upstream_status", "upstream_body"},
				[]string{found.details[1].key, found.details[2].key})
			assert.Equal(t, tc.paths, server.sentPaths())
		})
	}
}

func TestLinkAddNamesThePhrasesOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "source")
	partner := aContractIssue(t, dev, "partner")
	tests := []struct {
		name    string
		phrase  string
		nearest []any
	}{
		{name: "a phrase near none the instance has", phrase: "zzzzzzzzzz", nearest: everyDevInstancePhrase()},
		{name: "a phrase a letter short and in another letter case", phrase: "Depnds On", nearest: []any{"depends on"}},
	}
	for _, tc := range tests {
		sent := len(dev.requests())

		got := runWith(t, dev.env(), "link", "add", source, tc.phrase, partner)

		found := requireRefusal(t, got)
		assert.Equal(t, "unknown_name", found.code, tc.name)
		assert.Equal(t, []detail{{"issue", source},
			{"unknown", []any{[]detail{{"phrase", tc.phrase}, {"nearest", tc.nearest}}}}},
			found.details[1:], tc.name)
		// The phrases were settled by the one read of the issue they were read off, and nothing else went out.
		assert.Equal(t, []string{"/api/issues/" + source}, dev.sentPaths()[sent:], tc.name)
	}
}

func TestLinkAddRefusesLinkingAnIssueOfTheDevInstanceToItself(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "source")
	sent := len(dev.requests())

	for _, partner := range []string{source, strings.ToLower(source)} {
		got := runWith(t, dev.env(), "link", "add", source, "relates to", partner)

		found := requireRefusal(t, got)
		assert.Equal(t, "bad_usage", found.code, partner)
		assert.Equal(t, []detail{{"issue", source}, {"partner", source}}, found.details[1:], partner)
	}
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodGet, http.MethodGet},
		sentMethods(dev)[sent:])
}

// The issue at the other end is read before the write, so an issue the token has none of is a 404 of that
// read: the body naming it would have come back as a 400 about a field nobody wrote.
func TestLinkAddRefusesAPartnerTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "source")
	sent := len(dev.requests())

	got := runWith(t, dev.env(), "link", "add", source, "depends on", "DEV-99999")

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Entity with id DEV-99999 not found", detailNamed(t, found, "upstream_message"))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(dev)[sent:])
	assert.Equal(t, []string{"/api/issues/" + source, "/api/issues/DEV-99999"}, dev.sentPaths()[sent:])
}
