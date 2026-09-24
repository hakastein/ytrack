package cli_test

import (
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two issues of a scenario of link add, by the id the caller writes them as and the internal id YouTrack
// takes an issue at the other end of a link by.
const (
	addedSource     = "DEV-1"
	addedSourceID   = "3-19"
	addedPartner    = "DEV-2"
	addedPartnerID  = "3-20"
	addedOtherID    = "3-21"
	addedOtherIssue = "DEV-4"
)

// What the read before a write asks of the issue it writes on, and of the issue at the other end.
const (
	addSourceFields = "id,idReadable,links(id,direction,linkType(id,sourceToTarget,targetToSource," +
		"localizedSourceToTarget,localizedTargetToSource))"
	addPartnerFields = "id,idReadable"
)

// What the write itself asks for: the partner, the slot of the partner the issue now stands in, and inside it
// the issue with the whole of its own links, each of them counted.
func addWriteFields(partner string) string {
	return "id,links(direction,linkType(id),issues(id,links(direction," +
		"linkType(id,sourceToTarget,targetToSource),issuesSize,issues(id," + partner + "))))"
}

// A name of one end of a link type, as the server sends it: text, or null where the instance gave that end no
// translation of its own.
type phrase struct {
	text  string
	given bool
	// What the server sent in place of a name, where it sent something that is no name at all.
	raw string
}

func said(text string) phrase {
	return phrase{text: text, given: true}
}

// sentAs is a name of an end the server sent as that JSON, which is how a scenario sends one that is neither
// text nor the absence of text.
func sentAs(json string) phrase {
	return phrase{raw: json}
}

func (p phrase) sent() string {
	switch {
	case p.raw != "":
		return p.raw
	case !p.given:
		return "null"
	}
	return strconv.Quote(p.text)
}

// A link type as the catalogue read off an issue brings it: the id it goes by and the four names its two ends
// answer to.
type linkKind struct {
	id                      string
	sourceToTarget          phrase
	targetToSource          phrase
	localizedSourceToTarget phrase
	localizedTargetToSource phrase
}

func (k linkKind) sent() string {
	return `{"$type":"IssueLinkType","id":` + strconv.Quote(k.id) +
		`,"sourceToTarget":` + k.sourceToTarget.sent() +
		`,"targetToSource":` + k.targetToSource.sent() +
		`,"localizedSourceToTarget":` + k.localizedSourceToTarget.sent() +
		`,"localizedTargetToSource":` + k.localizedTargetToSource.sent() + `}`
}

// The five types of the polygon with the translations it gives their ends. Copy is the one the
// instance has no translation for, and it sends null at one end and an empty string at the other; the
// undirected type leaves the name of its second end empty as well.
func devLinkKinds() []linkKind {
	return []linkKind{
		{id: "163-0", sourceToTarget: said("relates to"), targetToSource: said(""),
			localizedSourceToTarget: said("связана с"), localizedTargetToSource: said("")},
		{id: "163-1", sourceToTarget: said("is required for"), targetToSource: said("depends on"),
			localizedSourceToTarget: said("обязательна для"), localizedTargetToSource: said("зависит от")},
		{id: "163-2", sourceToTarget: said("is duplicated by"), targetToSource: said("duplicates"),
			localizedSourceToTarget: said("дублирована"), localizedTargetToSource: said("дублирует")},
		{id: "163-3", sourceToTarget: said("parent for"), targetToSource: said("subtask of"),
			localizedSourceToTarget: said("родитель для"), localizedTargetToSource: said("подзадача для")},
		{id: "163-4", sourceToTarget: said("Скопирована в"), targetToSource: said("Копия"),
			localizedSourceToTarget: phrase{}, localizedTargetToSource: said("")},
	}
}

// One slot of the catalogue: the id the server addresses it by, the end the issue stands at and the type.
type catalogueSlot struct {
	id        string
	direction string
	kind      linkKind
}

func (s catalogueSlot) sent() string {
	return `{"$type":"IssueLink","id":` + strconv.Quote(s.id) +
		`,"direction":` + strconv.Quote(s.direction) + `,"linkType":` + s.kind.sent() + `}`
}

// The nine slots every issue of the polygon carries, addressed the way the server addresses them: digits and a
// dash for either end of an undirected type, an s for the source of a directed one and a t for its target.
func devLinkSlots() []catalogueSlot {
	kinds := devLinkKinds()
	slots := []catalogueSlot{{id: kinds[0].id, direction: "BOTH", kind: kinds[0]}}
	for _, kind := range kinds[1:] {
		slots = append(slots,
			catalogueSlot{id: kind.id + "s", direction: "OUTWARD", kind: kind},
			catalogueSlot{id: kind.id + "t", direction: "INWARD", kind: kind})
	}
	return slots
}

// devLinkSlot is the slot of the polygon that goes by that phrase, which is what a scenario names the answer
// to its write after.
func devLinkSlot(t *testing.T, id string) catalogueSlot {
	t.Helper()
	for _, slot := range devLinkSlots() {
		if slot.id == id {
			return slot
		}
	}
	require.Fail(t, "the polygon has no slot "+id)
	return catalogueSlot{}
}

func slotsOf(id, readable string, slots ...catalogueSlot) string {
	sent := make([]string, 0, len(slots))
	for _, slot := range slots {
		sent = append(sent, slot.sent())
	}
	return `{"$type":"Issue","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"links":[` + strings.Join(sent, ",") + `]}`
}

func polygonCatalogue() string {
	return slotsOf(addedSourceID, addedSource, devLinkSlots()...)
}

// What the read of the issue at the other end brings: the two ids and nothing else.
func addressedIssue(id, readable string) string {
	return `{"$type":"Issue","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) + `}`
}

// A slot of the partner in the answer to a write: the end and the type, which together say which link it is,
// and the issues at its other end.
func partnerSlot(direction, kind string, issues ...string) string {
	return `{"$type":"IssueLink","direction":` + strconv.Quote(direction) +
		`,"linkType":{"$type":"IssueLinkType","id":` + strconv.Quote(kind) + `}` +
		`,"issues":[` + strings.Join(issues, ",") + `]}`
}

// A slot of the issue nested in that answer: the document is printed off it, so it carries the phrases of its
// type and the count the server says it holds beside the issues themselves.
func sourceSlot(direction string, kind linkKind, issues ...string) string {
	return `{"$type":"IssueLink","direction":` + strconv.Quote(direction) +
		`,"linkType":{"$type":"IssueLinkType","id":` + strconv.Quote(kind.id) +
		`,"sourceToTarget":` + kind.sourceToTarget.sent() +
		`,"targetToSource":` + kind.targetToSource.sent() + `}` +
		`,"issuesSize":` + strconv.Itoa(len(issues)) + `,"issues":[` + strings.Join(issues, ",") + `]}`
}

// One issue at the other end of a link of the issue, by what the caller is printed one by unasked.
func linkedRecord(id, readable, summary string) string {
	return `{"$type":"Issue","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"summary":` + strconv.Quote(summary) + `}`
}

// The issue the call named first, as the answer to the write nests it under a slot of the partner.
func sourceUnder(slots ...string) string {
	return `{"$type":"Issue","id":"` + addedSourceID + `","links":[` + strings.Join(slots, ",") + `]}`
}

// The issue the write answers with, which is the partner it linked.
func writeAnswer(slots ...string) string {
	return `{"$type":"Issue","id":"` + addedPartnerID + `","links":[` + strings.Join(slots, ",") + `]}`
}

// The link an issue stands at the source of is the one its partner stands at the target of; an undirected type
// has one end, read the same from either side.
func theOtherEnd(direction string) string {
	switch direction {
	case "INWARD":
		return "OUTWARD"
	case "OUTWARD":
		return "INWARD"
	}
	return direction
}

// A write that went through as it was asked to: the partner holds the issue at the end opposite the phrase,
// and the issue holds the partner at the end of the phrase.
func linkWritten(slot catalogueSlot, held ...string) string {
	if len(held) == 0 {
		held = []string{sourceSlot(slot.direction, slot.kind, linkedRecord(addedPartnerID, addedPartner, "X"))}
	}
	return writeAnswer(partnerSlot(theOtherEnd(slot.direction), slot.kind.id, sourceUnder(held...)))
}

// linking is the server of a link add: each read answered by the id it goes out to, the write by the handler
// given.
func linking(t *testing.T, catalogue, partner string, write http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			write(w, r)
		case path.Base(r.URL.Path) == addedSource:
			answer(http.StatusOK, catalogue)(w, r)
		case path.Base(r.URL.Path) == addedPartner:
			answer(http.StatusOK, partner)(w, r)
		default:
			assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
		}
	})
}

// linkingThePolygon is that server with the catalogue of the polygon and an answer to the write that stands up
// to the check of both its ends.
func linkingThePolygon(t *testing.T, slot catalogueSlot) *upstream {
	t.Helper()
	return linking(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner),
		answer(http.StatusOK, linkWritten(slot)))
}

// Every way of writing link add that names no one link, refused before any request: the arity, the form of
// either id and a phrase that matches nothing there is to match.
func TestLinkAddRefusesACallThatNamesNoOneLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: []string{"link", "add"}},
		{name: "no partner", argv: []string{"link", "add", "DEV-1", "depends on"}},
		{name: "a fourth word", argv: []string{"link", "add", "DEV-1", "depends on", "DEV-2", "DEV-3"}},
		{name: "an issue that would reach another endpoint", argv: []string{"link", "add", "..", "depends on", "DEV-2"}},
		{name: "a partner that is an article", argv: []string{"link", "add", "DEV-1", "depends on", "DEV-A-1"}},
		{name: "an empty phrase", argv: []string{"link", "add", "DEV-1", "", "DEV-2"}},
		{name: "a phrase that is no text", argv: []string{"link", "add", "DEV-1", "\xff", "DEV-2"}},
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

// The help names the default a partner is printed by and sends the reader to the command that prints the
// phrases there are.
func TestLinkAddHelpNamesTheDefaultAndWhereThePhrasesComeFrom(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"link", "add", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, linkListPartner)
}

// A phrase is resolved against the slots of the issue itself: in any letter case, against the phrase link
// list prints and against the name the instance translates that end by. Neither the phrase nor any form of it
// reaches YouTrack — what goes out is the id the server gave the slot and the internal id of the partner.
func TestLinkAddResolvesThePhraseAgainstTheSlotsOfTheIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		phrase string
		slot   string
	}{
		{name: "the phrase itself", phrase: "depends on", slot: "163-1t"},
		{name: "the phrase in title case", phrase: "Depends On", slot: "163-1t"},
		{name: "the phrase in upper case", phrase: "DEPENDS ON", slot: "163-1t"},
		{name: "the translation of that end", phrase: "зависит от", slot: "163-1t"},
		{name: "the translation in upper case", phrase: "ЗАВИСИТ ОТ", slot: "163-1t"},
		{name: "a type the instance translates at neither end", phrase: "скопирована в", slot: "163-4s"},
		{name: "the other end of that type", phrase: "КОПИЯ", slot: "163-4t"},
		{name: "the translation of an undirected type", phrase: "связана с", slot: "163-0"},
		{name: "the phrase of that same type", phrase: "relates to", slot: "163-0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slot := devLinkSlot(t, tc.slot)
			server := linkingThePolygon(t, slot)

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedPartner)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{
				"/api/issues/" + addedSource,
				"/api/issues/" + addedPartner,
				"/api/issues/" + addedSource + "/links/" + tc.slot + "/issues",
			}, server.sentPaths())
			assert.Equal(t, []string{"", "", `{"id":"` + addedPartnerID + `"}`}, server.asks())
			for _, written := range []string{tc.phrase, strings.ToLower(tc.phrase)} {
				for _, target := range server.sentTargets() {
					assert.NotContains(t, target, written)
				}
				for _, body := range server.asks() {
					assert.NotContains(t, body, written)
				}
			}
			assert.Equal(t, []string{addSourceFields, addPartnerFields, addWriteFields(linkListPartner)},
				server.sentFields())
		})
	}
}

// Which slot a phrase names is settled by the end it is the name of, not by where the slot stands in the
// answer and not by anything composed out of the id of the type: the two ends of one type go out to two ids
// neither of which can be derived from it.
func TestLinkAddWritesToTheEndThePhraseNames(t *testing.T) {
	t.Parallel()
	kind := linkKind{id: "9-9", sourceToTarget: said("is required for"), targetToSource: said("depends on"),
		localizedSourceToTarget: said("обязательна для"), localizedTargetToSource: said("зависит от")}
	// The end the issue stands at the target of arrives first, before the one it stands at the source of.
	slots := []catalogueSlot{
		{id: "42-1t", direction: "INWARD", kind: kind},
		{id: "42-1s", direction: "OUTWARD", kind: kind},
	}
	tests := []struct {
		name   string
		phrase string
		at     int
	}{
		{name: "the phrase of the end that arrived first", phrase: "depends on", at: 0},
		{name: "the phrase of the end that arrived second", phrase: "is required for", at: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slot := slots[tc.at]
			server := linking(t, slotsOf(addedSourceID, addedSource, slots...),
				addressedIssue(addedPartnerID, addedPartner), answer(http.StatusOK, linkWritten(slot)))

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedPartner)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, "/api/issues/"+addedSource+"/links/"+slot.id+"/issues", server.sentPaths()[2])
			assert.Equal(t, tc.phrase, keysOf(nodeAt(t, requireMapping(t, "stdout", got.stdout), "links"))[0])
		})
	}
}

// A 200 says YouTrack took the body, not that it wrote the link the phrase named, so the one answer is
// read from both of its ends. What it prints is the whole of the issue's links afterwards, the ones no call
// touched among them, because a write is answered with the state it left behind.
func TestLinkAddPrintsTheIssueTheWriteLeftBehind(t *testing.T) {
	t.Parallel()
	depend := devLinkSlot(t, "163-1t")
	subtask := devLinkSlot(t, "163-3t")
	held := []string{
		sourceSlot(depend.direction, depend.kind, linkedRecord(addedPartnerID, addedPartner, "Блокирующая задача")),
		sourceSlot(subtask.direction, subtask.kind, linkedRecord(addedOtherID, addedOtherIssue, "Родительская задача")),
	}
	server := linking(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner),
		answer(http.StatusOK, linkWritten(depend, held...)))

	got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{
		{"total", 2},
		{"returned", 2},
		{"truncated", false},
		{"links", []detail{
			{"depends on", []any{[]detail{{"idReadable", addedPartner}, {"summary", "Блокирующая задача"}}}},
			{"subtask of", []any{[]detail{{"idReadable", addedOtherIssue}, {"summary", "Родительская задача"}}}},
		}},
	}, requireDocument(t, got.stdout))
	for _, hidden := range []string{addedSourceID, addedPartnerID, "163-", "INWARD", "issuesSize"} {
		assert.NotContains(t, got.stdout, hidden)
	}
}

// An answer that does not hold the link the write asked for at both of its ends is the server saying one
// thing and having done another. The write went through whatever the answer says, so the refusal comes with
// the exit code of a call that changed the instance without printing what it left behind.
func TestLinkAddRefusesAnAnswerThatDoesNotHoldTheLink(t *testing.T) {
	t.Parallel()
	slot := devLinkSlot(t, "163-1t")
	record := linkedRecord(addedPartnerID, addedPartner, "Блокирующая задача")
	tests := []struct {
		name    string
		written string
	}{
		{
			// The same end at both sides: the link would run the way the caller asked it not to.
			name: "the issue at the end the phrase names rather than the one opposite it",
			written: writeAnswer(partnerSlot(slot.direction, slot.kind.id,
				sourceUnder(sourceSlot(slot.direction, slot.kind, record)))),
		},
		{
			name: "the issue holding the partner at no end at all",
			written: writeAnswer(partnerSlot(theOtherEnd(slot.direction), slot.kind.id,
				sourceUnder(sourceSlot(theOtherEnd(slot.direction), slot.kind, record)))),
		},
		{
			// The end is the one the phrase names and the type is another the issue has at that same end: the
			// link that came back is printed under a phrase the call never wrote.
			name: "the issue holding the partner under a type other than the one the phrase names",
			written: writeAnswer(partnerSlot(theOtherEnd(slot.direction), slot.kind.id,
				sourceUnder(sourceSlot(slot.direction, devLinkKinds()[4], record)))),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner),
				answer(http.StatusOK, tc.written))

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

			found := requireUncertainty(t, got)
			assert.Equal(t, refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", "POST " + server.url + "/api/issues/" + addedSource + "/links/" + slot.id +
						"/issues?fields=" + addWriteFields(linkListPartner)},
					{"issue", addedSource},
					{"phrase", "depends on"},
					{"partner", addedPartner},
				},
			}, found)
		})
	}
}

// The rest of what the answer to a write is held to: it is about the partner the body named, and the issue
// it carries holds links at all.
func TestLinkAddRefusesAnAnswerAboutSomethingElse(t *testing.T) {
	t.Parallel()
	slot := devLinkSlot(t, "163-1t")
	record := linkedRecord(addedPartnerID, addedPartner, "Блокирующая задача")
	tests := []struct {
		name    string
		written string
	}{
		{
			name: "an issue other than the partner",
			written: `{"$type":"Issue","id":"3-99","links":[` +
				partnerSlot(theOtherEnd(slot.direction), slot.kind.id,
					sourceUnder(sourceSlot(slot.direction, slot.kind, record))) + `]}`,
		},
		{
			name: "an issue with no links at all",
			written: writeAnswer(partnerSlot(theOtherEnd(slot.direction), slot.kind.id,
				`{"$type":"Issue","id":"`+addedSourceID+`"}`)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner),
				answer(http.StatusOK, tc.written))

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, []detail{{"issue", addedSource}, {"phrase", "depends on"}, {"partner", addedPartner}},
				found.details[1:4])
		})
	}
}

// A phrase the issue has no link under is the caller's to fix, so the refusal hands them the phrases of
// that issue nearest what they wrote and nothing goes out but the read that settled them.
func TestLinkAddRefusesAPhraseNoLinkOfTheIssueGoesBy(t *testing.T) {
	t.Parallel()
	server := linking(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

	got := runWith(t, server.env(), "link", "add", addedSource, "depnds on", addedPartner)

	assert.Equal(t, refusal{
		code: "unknown_name",
		details: []detail{
			{"request", issueRequest(server.url, addedSource, addSourceFields)},
			{"issue", addedSource},
			{"unknown", []any{[]detail{{"phrase", "depnds on"}, {"nearest", []any{"depends on"}}}}},
		},
	}, requireRefusal(t, got))
	assert.Equal(t, []string{"/api/issues/" + addedSource}, server.sentPaths())
}

// Either issue may be one the token has none of, and the read of it is what says so: the server answers a
// body naming an issue it cannot find with a 400 of a text about a field nobody wrote.
func TestLinkAddRefusesAnIssueTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		missing string
		paths   []string
	}{
		{name: "the issue the link is written on", missing: addedSource, paths: []string{"/api/issues/DEV-1"}},
		{
			name:    "the issue at the other end",
			missing: addedPartner,
			paths:   []string{"/api/issues/DEV-1", "/api/issues/DEV-2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost:
					assert.Fail(t, "a write reached the server", "%s %s", r.Method, r.URL)
				case path.Base(r.URL.Path) == tc.missing:
					answer(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id `+
						tc.missing+` not found"}`)(w, r)
				default:
					answer(http.StatusOK, polygonCatalogue())(w, r)
				}
			})

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

			found := requireRefusal(t, got)
			assert.Equal(t, "not_found", found.code)
			assert.Equal(t, "Entity with id "+tc.missing+" not found", detailNamed(t, found, "upstream_message"))
			assert.Equal(t, tc.paths, server.sentPaths())
		})
	}
}

// What the server says about a write it refused goes on word for word, HTML entities and all, and a write
// whose answer never came or came from something other than YouTrack leaves the caller with a call they cannot
// simply send again.
func TestLinkAddCarriesWhatTheServerSaidAboutTheWrite(t *testing.T) {
	t.Parallel()
	const cycle = "Subtask &mdash; обнаружена циклическая связь: DEV-26 &rarr; DEV-25 &rarr; DEV-26"
	tests := []struct {
		name  string
		write http.HandlerFunc
		code  string
		exit  int
	}{
		{
			name: "a write the server refused",
			write: answer(http.StatusBadRequest,
				`{"error":"invalid_properties","error_description":`+strconv.Quote(cycle)+`}`),
			code: "rejected",
			exit: 1,
		},
		{name: "a write whose answer never came", write: breakOff, code: "write_uncertain", exit: 2},
		{name: "a write answered by a gateway", write: gateway(http.StatusBadGateway), code: "write_uncertain", exit: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner), tc.write)

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, []detail{{"issue", addedSource}, {"phrase", "depends on"}, {"partner", addedPartner}},
				found.details[1:4])
			if tc.code == "rejected" {
				assert.Equal(t, cycle, detailNamed(t, found, "upstream_message"))
			}
		})
	}
}

// --fields says what an issue at the other end of a link is printed by, the way it does for link list:
// the names ytrack fills in for a block of an issue go out beside what the caller wrote and reach no document.
func TestLinkAddPrintsAPartnerByWhatWasAskedOfIt(t *testing.T) {
	t.Parallel()
	slot := devLinkSlot(t, "163-1t")
	state := arrivedField{name: "State", valueType: "state", ordinal: "1", binding: "180-1",
		value: bundleElement("Новая")}
	tests := []struct {
		name       string
		expression string
		record     string
		asked      string
		printed    []detail
	}{
		{
			name:       "fewer names than the default",
			expression: "idReadable",
			record:     `{"$type":"Issue","id":"` + addedPartnerID + `","idReadable":"` + addedPartner + `"}`,
			asked:      "idReadable",
			printed:    []detail{{"idReadable", addedPartner}},
		},
		{
			// A block of an issue is read through a composition of the tool's own, and a partner is an issue.
			name:       "a block of the partner added to the default",
			expression: "+customFields",
			record: `{"$type":"Issue","id":"` + addedPartnerID + `","idReadable":"` + addedPartner +
				`","summary":"X","customFields":` + arrivedFields(state) + `}`,
			asked:   linkListPartner + "," + customFieldsFields,
			printed: []detail{{"idReadable", addedPartner}, {"summary", "X"}, {"customFields", []detail{{"State", "Новая"}}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, polygonCatalogue(), addressedIssue(addedPartnerID, addedPartner),
				answer(http.StatusOK, linkWritten(slot, sourceSlot(slot.direction, slot.kind, tc.record))))

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner,
				"--fields", tc.expression)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []detail{{"depends on", []any{tc.printed}}}, nodeValue(t, got.stdout, "links"))
			assert.Equal(t, addWriteFields(tc.asked), server.sentFields()[2])
		})
	}
}

// noLinkWritten stands for the write a refusal before it must not reach.
func noLinkWritten(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a write reached the server", "%s %s", r.Method, r.URL)
	}
}

// The links of two issues of the contract test's own, written and read back on the polygon: the phrase
// goes out as the id the server addresses the slot by, the end it names is the end the link is written at, and
// writing the same link twice leaves one link behind.
func TestLinkAddLinksTwoIssuesOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "source")
	partner := aContractIssue(t, dev, "partner")

	sent := len(dev.requests())
	got := runWith(t, dev.env(), "link", "add", source, "depends on", partner)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	document := requireDocument(t, got.stdout)
	assert.Equal(t, []detail{{"total", 1}, {"returned", 1}, {"truncated", false}},
		document[:3])
	assert.Equal(t, partner,
		nodeAt(t, requireMapping(t, "stdout", got.stdout), "links", "depends on", "idReadable").Value)

	// The slot the write went out to is the one the read before it sent, at the end the phrase names, and its
	// id says which end that is.
	slot := path.Base(strings.TrimSuffix(dev.sentPaths()[sent+2], "/issues"))
	assert.Regexp(t, `^[0-9]+-[0-9]+t$`, slot)
	assert.Equal(t, slotOfThePhrase(t, dev.answers()[sent], "INWARD", "depends on"), slot)

	// The other end of the link stands on the partner under the phrase of that end, and the phrase of this one
	// is nowhere on it.
	other := runWith(t, dev.env(), "link", "list", partner)
	require.Equal(t, 0, other.code, "stderr: %s", other.stderr)
	block := nodeAt(t, requireMapping(t, "stdout", other.stdout), "links")
	assert.Equal(t, source, nodeAt(t, block, "is required for", "idReadable").Value)
	assert.NotContains(t, keysOf(block), "depends on")

	// The same link written again is the same link: YouTrack writes it once, and the call that asked for it is
	// answered the same way.
	sent = len(dev.requests())
	again := runWith(t, dev.env(), "link", "add", source, "ЗАВИСИТ ОТ", partner)
	require.Equal(t, 0, again.code, "stderr: %s", again.stderr)
	assert.Equal(t, slot, path.Base(strings.TrimSuffix(dev.sentPaths()[sent+2], "/issues")))

	held := runWith(t, dev.env(), "link", "list", source)
	require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
	assert.Equal(t, []detail{{"total", 1}, {"returned", 1}, {"truncated", false}},
		requireDocument(t, held.stdout)[:3])
}

// aContractIssue files one issue of the contract test's own in DEV, by the title that names the scenario, and
// takes it away again once the scenario is through. DEV requires four custom fields of a new issue, and the
// link fixtures of the polygon are read by these tests and never written.
func aContractIssue(t *testing.T, dev *upstream, role string) string {
	t.Helper()
	title := "ytrack contract " + t.Name() + " " + role
	got := runWith(t, dev.env(), "issue", "create", "DEV", "--summary", title,
		"--field", "Type=Task",
		"--field", "Категория=Развитие технологий",
		"--field", "Клиент=ACME",
		"--field", "Модуль системы=Инфраструктура. DevOps",
		"--fields", "idReadable")
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

// slotOfThePhrase is the id the answer to a read addresses the slot of that end and that phrase by, which is
// what the path of the write is held against: a segment equal to it was read off the issue, not composed.
func slotOfThePhrase(t *testing.T, body []byte, direction, phrase string) string {
	t.Helper()
	var read struct {
		Links []struct {
			ID        string `json:"id"`
			Direction string `json:"direction"`
			LinkType  struct {
				SourceToTarget string `json:"sourceToTarget"`
				TargetToSource string `json:"targetToSource"`
			} `json:"linkType"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal(body, &read), "the answer read: %s", body)
	for _, slot := range read.Links {
		named := slot.LinkType.SourceToTarget
		if slot.Direction == "INWARD" {
			named = slot.LinkType.TargetToSource
		}
		if slot.Direction == direction && named == phrase {
			return slot.ID
		}
	}
	require.Fail(t, "the issue holds no slot of that phrase", "%s %s: %s", direction, phrase, body)
	return ""
}

// nodeValue is what the document printed under that key, read the way a refusal's details are read.
func nodeValue(t *testing.T, stdout, key string) any {
	t.Helper()
	for _, printed := range requireDocument(t, stdout) {
		if printed.key == key {
			return printed.value
		}
	}
	require.Fail(t, "the document printed no "+key, "%q", stdout)
	return nil
}
