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

func phraseOf(text string) phrase {
	return phrase{text: text, given: true}
}

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

func devLinkKinds() []linkKind {
	return []linkKind{
		{id: "163-0", sourceToTarget: phraseOf("relates to"), targetToSource: phraseOf(""),
			localizedSourceToTarget: phraseOf("связана с"), localizedTargetToSource: phraseOf("")},
		{id: "163-1", sourceToTarget: phraseOf("is required for"), targetToSource: phraseOf("depends on"),
			localizedSourceToTarget: phraseOf("обязательна для"), localizedTargetToSource: phraseOf("зависит от")},
		{id: "163-2", sourceToTarget: phraseOf("is duplicated by"), targetToSource: phraseOf("duplicates"),
			localizedSourceToTarget: phraseOf("дублирована"), localizedTargetToSource: phraseOf("дублирует")},
		{id: "163-3", sourceToTarget: phraseOf("parent for"), targetToSource: phraseOf("subtask of"),
			localizedSourceToTarget: phraseOf("родитель для"), localizedTargetToSource: phraseOf("подзадача для")},
		{id: "163-4", sourceToTarget: phraseOf("Скопирована в"), targetToSource: phraseOf("Копия"),
			localizedSourceToTarget: phrase{}, localizedTargetToSource: phraseOf("")},
	}
}

type catalogueLink struct {
	id        string
	direction string
	kind      linkKind
}

func (s catalogueLink) sent() string {
	return `{"$type":"IssueLink","id":` + strconv.Quote(s.id) +
		`,"direction":` + strconv.Quote(s.direction) + `,"linkType":` + s.kind.sent() + `}`
}

func devIssueLinks() []catalogueLink {
	kinds := devLinkKinds()
	links := []catalogueLink{{id: kinds[0].id, direction: "BOTH", kind: kinds[0]}}
	for _, kind := range kinds[1:] {
		links = append(links,
			catalogueLink{id: kind.id + "s", direction: "OUTWARD", kind: kind},
			catalogueLink{id: kind.id + "t", direction: "INWARD", kind: kind})
	}
	return links
}

func devIssueLink(t *testing.T, id string) catalogueLink {
	t.Helper()
	for _, link := range devIssueLinks() {
		if link.id == id {
			return link
		}
	}
	require.Fail(t, "the dev instance has no slot "+id)
	return catalogueLink{}
}

func issueLinksOf(id, readable string, links ...catalogueLink) string {
	sent := make([]string, 0, len(links))
	for _, link := range links {
		sent = append(sent, link.sent())
	}
	return `{"$type":"Issue","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"links":[` + strings.Join(sent, ",") + `]}`
}

func devInstanceCatalogue() string {
	return issueLinksOf(addedSourceID, addedSource, devIssueLinks()...)
}

// What the read of the issue at the other end brings: the two ids and nothing else.
func addressedIssue(id, readable string) string {
	return `{"$type":"Issue","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) + `}`
}

func partnerIssueLink(direction, kind string, issues ...string) string {
	return `{"$type":"IssueLink","direction":` + strconv.Quote(direction) +
		`,"linkType":{"$type":"IssueLinkType","id":` + strconv.Quote(kind) + `}` +
		`,"issues":[` + strings.Join(issues, ",") + `]}`
}

func sourceIssueLink(direction string, kind linkKind, issues ...string) string {
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

func sourceUnder(links ...string) string {
	return `{"$type":"Issue","id":"` + addedSourceID + `","links":[` + strings.Join(links, ",") + `]}`
}

// The issue the write answers with, which is the partner it linked.
func writeAnswer(links ...string) string {
	return `{"$type":"Issue","id":"` + addedPartnerID + `","links":[` + strings.Join(links, ",") + `]}`
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
func linkWritten(link catalogueLink, held ...string) string {
	if len(held) == 0 {
		held = []string{sourceIssueLink(link.direction, link.kind, linkedRecord(addedPartnerID, addedPartner, "X"))}
	}
	return writeAnswer(partnerIssueLink(theOtherEnd(link.direction), link.kind.id, sourceUnder(held...)))
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
			respondWith(http.StatusOK, catalogue)(w, r)
		case path.Base(r.URL.Path) == addedPartner:
			respondWith(http.StatusOK, partner)(w, r)
		default:
			assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
		}
	})
}

func linkingTheDevInstance(t *testing.T, link catalogueLink) *upstream {
	t.Helper()
	return linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner),
		respondWith(http.StatusOK, linkWritten(link)))
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
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

func TestLinkAddResolvesThePhraseAgainstTheSlotsOfTheIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		phrase string
		link   string
	}{
		{name: "the phrase itself", phrase: "depends on", link: "163-1t"},
		{name: "the phrase in title case", phrase: "Depends On", link: "163-1t"},
		{name: "the phrase in upper case", phrase: "DEPENDS ON", link: "163-1t"},
		{name: "the translation of that end", phrase: "зависит от", link: "163-1t"},
		{name: "the translation in upper case", phrase: "ЗАВИСИТ ОТ", link: "163-1t"},
		{name: "a type the instance translates at neither end", phrase: "скопирована в", link: "163-4s"},
		{name: "the other end of that type", phrase: "КОПИЯ", link: "163-4t"},
		{name: "the translation of an undirected type", phrase: "связана с", link: "163-0"},
		{name: "the phrase of that same type", phrase: "relates to", link: "163-0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			link := devIssueLink(t, tc.link)
			server := linkingTheDevInstance(t, link)

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedPartner)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{
				"/api/issues/" + addedSource,
				"/api/issues/" + addedPartner,
				"/api/issues/" + addedSource + "/links/" + tc.link + "/issues",
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

func TestLinkAddWritesToTheEndThePhraseNames(t *testing.T) {
	t.Parallel()
	kind := linkKind{id: "9-9", sourceToTarget: phraseOf("is required for"), targetToSource: phraseOf("depends on"),
		localizedSourceToTarget: phraseOf("обязательна для"), localizedTargetToSource: phraseOf("зависит от")}
	// The end the issue stands at the target of arrives first, before the one it stands at the source of.
	links := []catalogueLink{
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
			link := links[tc.at]
			server := linking(t, issueLinksOf(addedSourceID, addedSource, links...),
				addressedIssue(addedPartnerID, addedPartner), respondWith(http.StatusOK, linkWritten(link)))

			got := runWith(t, server.env(), "link", "add", addedSource, tc.phrase, addedPartner)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, "/api/issues/"+addedSource+"/links/"+link.id+"/issues", server.sentPaths()[2])
			assert.Equal(t, tc.phrase, keysOf(nodeAt(t, requireMapping(t, "stdout", got.stdout), "links"))[0])
		})
	}
}

// A 200 says YouTrack took the body, not that it wrote the link the phrase named, so the one answer is
// read from both of its ends. What it prints is the whole of the issue's links afterwards, the ones no call
// touched among them, because a write is answered with the state it left behind.
func TestLinkAddPrintsTheIssueTheWriteLeftBehind(t *testing.T) {
	t.Parallel()
	depend := devIssueLink(t, "163-1t")
	subtask := devIssueLink(t, "163-3t")
	held := []string{
		sourceIssueLink(depend.direction, depend.kind, linkedRecord(addedPartnerID, addedPartner, "Блокирующая задача")),
		sourceIssueLink(subtask.direction, subtask.kind, linkedRecord(addedOtherID, addedOtherIssue, "Родительская задача")),
	}
	server := linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner),
		respondWith(http.StatusOK, linkWritten(depend, held...)))

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
func TestLinkAddRefusesAResponseWithoutTheLink(t *testing.T) {
	t.Parallel()
	link := devIssueLink(t, "163-1t")
	record := linkedRecord(addedPartnerID, addedPartner, "Блокирующая задача")
	tests := []struct {
		name    string
		written string
	}{
		{
			// The same end at both sides: the link would run the way the caller asked it not to.
			name: "the issue at the end the phrase names rather than the one opposite it",
			written: writeAnswer(partnerIssueLink(link.direction, link.kind.id,
				sourceUnder(sourceIssueLink(link.direction, link.kind, record)))),
		},
		{
			name: "the issue holding the partner at no end at all",
			written: writeAnswer(partnerIssueLink(theOtherEnd(link.direction), link.kind.id,
				sourceUnder(sourceIssueLink(theOtherEnd(link.direction), link.kind, record)))),
		},
		{
			// The end is the one the phrase names and the type is another the issue has at that same end: the
			// link that came back is printed under a phrase the call never wrote.
			name: "the issue holding the partner under a type other than the one the phrase names",
			written: writeAnswer(partnerIssueLink(theOtherEnd(link.direction), link.kind.id,
				sourceUnder(sourceIssueLink(link.direction, devLinkKinds()[4], record)))),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner),
				respondWith(http.StatusOK, tc.written))

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

			found := requireUncertainty(t, got)
			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", "POST " + server.url + "/api/issues/" + addedSource + "/links/" + link.id +
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
	link := devIssueLink(t, "163-1t")
	record := linkedRecord(addedPartnerID, addedPartner, "Блокирующая задача")
	tests := []struct {
		name    string
		written string
	}{
		{
			name: "an issue other than the partner",
			written: `{"$type":"Issue","id":"3-99","links":[` +
				partnerIssueLink(theOtherEnd(link.direction), link.kind.id,
					sourceUnder(sourceIssueLink(link.direction, link.kind, record))) + `]}`,
		},
		{
			name: "an issue with no links at all",
			written: writeAnswer(partnerIssueLink(theOtherEnd(link.direction), link.kind.id,
				`{"$type":"Issue","id":"`+addedSourceID+`"}`)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner),
				respondWith(http.StatusOK, tc.written))

			got := runWith(t, server.env(), "link", "add", addedSource, "depends on", addedPartner)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []detail{{"issue", addedSource}, {"phrase", "depends on"}, {"partner", addedPartner}},
				found.details[1:4])
		})
	}
}

// A phrase the issue has no link under is the caller's to fix, so the refusal hands them the phrases of
// that issue nearest what they wrote and nothing goes out but the read that settled them.
func TestLinkAddRefusesAPhraseNoLinkOfTheIssueGoesBy(t *testing.T) {
	t.Parallel()
	server := linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner), noLinkWritten(t))

	got := runWith(t, server.env(), "link", "add", addedSource, "depnds on", addedPartner)

	assert.Equal(t, faultDocument{
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
					respondWith(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id `+
						tc.missing+` not found"}`)(w, r)
				default:
					respondWith(http.StatusOK, devInstanceCatalogue())(w, r)
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
			write: respondWith(http.StatusBadRequest,
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
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner), tc.write)

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
	link := devIssueLink(t, "163-1t")
	state := receivedField{name: "State", valueType: "state", ordinal: "1", binding: "180-1",
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
				`","summary":"X","customFields":` + receivedFields(state) + `}`,
			asked:   linkListPartner + "," + customFieldsFields,
			printed: []detail{{"idReadable", addedPartner}, {"summary", "X"}, {"customFields", []detail{{"State", "Новая"}}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedPartnerID, addedPartner),
				respondWith(http.StatusOK, linkWritten(link, sourceIssueLink(link.direction, link.kind, tc.record))))

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

	link := path.Base(strings.TrimSuffix(dev.sentPaths()[sent+2], "/issues"))
	assert.Regexp(t, `^[0-9]+-[0-9]+t$`, link)
	assert.Equal(t, issueLinkOfThePhrase(t, dev.answers()[sent], "INWARD", "depends on"), link)

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
	assert.Equal(t, link, path.Base(strings.TrimSuffix(dev.sentPaths()[sent+2], "/issues")))

	held := runWith(t, dev.env(), "link", "list", source)
	require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
	assert.Equal(t, []detail{{"total", 1}, {"returned", 1}, {"truncated", false}},
		requireDocument(t, held.stdout)[:3])
}

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

func issueLinkOfThePhrase(t *testing.T, body []byte, direction, phrase string) string {
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
	for _, link := range read.Links {
		named := link.LinkType.SourceToTarget
		if link.Direction == "INWARD" {
			named = link.LinkType.TargetToSource
		}
		if link.Direction == direction && named == phrase {
			return link.ID
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
