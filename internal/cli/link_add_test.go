package cli_test

import (
	"net/http"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	addedSource     = "DEV-1"
	addedSourceID   = "3-19"
	addedTarget     = "DEV-2"
	addedTargetID   = "3-20"
	addedOtherID    = "3-21"
	addedOtherIssue = "DEV-4"
)

const (
	addSourceFields = "id,idReadable,links(id,direction,linkType(id,sourceToTarget,targetToSource," +
		"localizedSourceToTarget,localizedTargetToSource))"
	addTargetFields = "id,idReadable"
)

func addWriteFields(target string) string {
	return "id,links(direction,linkType(id),issues(id,links(direction," +
		"linkType(id,sourceToTarget,targetToSource),issuesSize,issues(id," + target + "))))"
}

type phrase struct {
	text  string
	given bool
	raw   string
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

func addressedIssue(id, readable string) string {
	return `{"$type":"Issue","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) + `}`
}

func targetIssueLink(direction, kind string, issues ...string) string {
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

func linkedRecord(id, readable, summary string) string {
	return `{"$type":"Issue","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"summary":` + strconv.Quote(summary) + `}`
}

func sourceUnder(links ...string) string {
	return `{"$type":"Issue","id":"` + addedSourceID + `","links":[` + strings.Join(links, ",") + `]}`
}

func writeAnswerOfTheTarget(links ...string) string {
	return `{"$type":"Issue","id":"` + addedTargetID + `","links":[` + strings.Join(links, ",") + `]}`
}

func theOtherEnd(direction string) string {
	switch direction {
	case "INWARD":
		return "OUTWARD"
	case "OUTWARD":
		return "INWARD"
	}
	return direction
}

func linkWritten(link catalogueLink, held ...string) string {
	if len(held) == 0 {
		held = []string{sourceIssueLink(link.direction, link.kind, linkedRecord(addedTargetID, addedTarget, "X"))}
	}
	return writeAnswerOfTheTarget(targetIssueLink(theOtherEnd(link.direction), link.kind.id, sourceUnder(held...)))
}

func linking(t *testing.T, catalogue, target string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			write(w, r)
		case path.Base(r.URL.Path) == addedSource:
			fake.JSON(http.StatusOK, catalogue)(w, r)
		case path.Base(r.URL.Path) == addedTarget:
			fake.JSON(http.StatusOK, target)(w, r)
		default:
			assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
		}
	})
}

func linkingTheDevInstance(t *testing.T, link catalogueLink) *fake.Server {
	t.Helper()
	return linking(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget),
		fake.JSON(http.StatusOK, linkWritten(link)))
}

func TestLinkAddRefusesACallThatNamesNoOneLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an issue that would reach another endpoint", argv: []string{"link", "add", "..", "depends on", "DEV-2"}},
		{name: "a target issue that is an article", argv: []string{"link", "add", "DEV-1", "depends on", "DEV-A-1"}},
		{name: "an empty phrase", argv: []string{"link", "add", "DEV-1", "", "DEV-2"}},
		{name: "a phrase that is no text", argv: []string{"link", "add", "DEV-1", "\xff", "DEV-2"}},
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

			got := runWith(t, server.Env(), "link", "add", addedSource, tc.phrase, addedTarget)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{
				"/api/issues/" + addedSource,
				"/api/issues/" + addedTarget,
				"/api/issues/" + addedSource + "/links/" + tc.link + "/issues",
			}, server.Paths())
			assert.Equal(t, []string{"", "", `{"id":"` + addedTargetID + `"}`}, server.Bodies())
			for _, written := range []string{tc.phrase, strings.ToLower(tc.phrase)} {
				for _, target := range server.Targets() {
					assert.NotContains(t, target, written)
				}
				for _, body := range server.Bodies() {
					assert.NotContains(t, body, written)
				}
			}
			assert.Equal(t, []string{addSourceFields, addTargetFields, addWriteFields(linkListTarget)},
				server.Fields())
		})
	}
}

func TestLinkAddWritesToTheEndThePhraseNames(t *testing.T) {
	t.Parallel()
	kind := linkKind{id: "9-9", sourceToTarget: phraseOf("is required for"), targetToSource: phraseOf("depends on"),
		localizedSourceToTarget: phraseOf("обязательна для"), localizedTargetToSource: phraseOf("зависит от")}
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
				addressedIssue(addedTargetID, addedTarget), fake.JSON(http.StatusOK, linkWritten(link)))

			got := runWith(t, server.Env(), "link", "add", addedSource, tc.phrase, addedTarget)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, "/api/issues/"+addedSource+"/links/"+link.id+"/issues", server.Paths()[2])
			assert.Equal(t, tc.phrase, keysOf(nodeAt(t, requireMapping(t, "stdout", got.stdout), "links"))[0])
		})
	}
}

func TestLinkAddPrintsTheIssueTheWriteLeftBehind(t *testing.T) {
	t.Parallel()
	depend := devIssueLink(t, "163-1t")
	subtask := devIssueLink(t, "163-3t")
	held := []string{
		sourceIssueLink(depend.direction, depend.kind, linkedRecord(addedTargetID, addedTarget, "Блокирующая задача")),
		sourceIssueLink(subtask.direction, subtask.kind, linkedRecord(addedOtherID, addedOtherIssue, "Родительская задача")),
	}
	server := linking(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget),
		fake.JSON(http.StatusOK, linkWritten(depend, held...)))

	got := runWith(t, server.Env(), "link", "add", addedSource, "depends on", addedTarget)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{
		{"total", 2},
		{"returned", 2},
		{"truncated", false},
		{"links", []detail{
			{"depends on", []any{[]detail{{"idReadable", addedTarget}, {"summary", "Блокирующая задача"}}}},
			{"subtask of", []any{[]detail{{"idReadable", addedOtherIssue}, {"summary", "Родительская задача"}}}},
		}},
	}, requireDocument(t, got.stdout))
	for _, hidden := range []string{addedSourceID, addedTargetID, "163-", "INWARD", "issuesSize"} {
		assert.NotContains(t, got.stdout, hidden)
	}
}

func TestLinkAddRefusesAResponseWithoutTheLink(t *testing.T) {
	t.Parallel()
	link := devIssueLink(t, "163-1t")
	record := linkedRecord(addedTargetID, addedTarget, "Блокирующая задача")
	tests := []struct {
		name    string
		written string
	}{
		{
			name: "the issue at the end the phrase names rather than the one opposite it",
			written: writeAnswerOfTheTarget(targetIssueLink(link.direction, link.kind.id,
				sourceUnder(sourceIssueLink(link.direction, link.kind, record)))),
		},
		{
			name: "the issue holding the target issue at no end at all",
			written: writeAnswerOfTheTarget(targetIssueLink(theOtherEnd(link.direction), link.kind.id,
				sourceUnder(sourceIssueLink(theOtherEnd(link.direction), link.kind, record)))),
		},
		{
			name: "the issue holding the target issue under a type other than the one the phrase names",
			written: writeAnswerOfTheTarget(targetIssueLink(theOtherEnd(link.direction), link.kind.id,
				sourceUnder(sourceIssueLink(link.direction, devLinkKinds()[4], record)))),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget),
				fake.JSON(http.StatusOK, tc.written))

			got := runWith(t, server.Env(), "link", "add", addedSource, "depends on", addedTarget)

			found := requireUncertainty(t, got)
			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", "POST " + server.URL + "/api/issues/" + addedSource + "/links/" + link.id +
						"/issues?fields=" + addWriteFields(linkListTarget)},
					{"issue", addedSource},
					{"phrase", "depends on"},
					{"target", addedTarget},
				},
			}, found)
		})
	}
}

func TestLinkAddRefusesAnAnswerAboutSomethingElse(t *testing.T) {
	t.Parallel()
	link := devIssueLink(t, "163-1t")
	record := linkedRecord(addedTargetID, addedTarget, "Блокирующая задача")
	tests := []struct {
		name    string
		written string
	}{
		{
			name: "an issue other than the target issue",
			written: `{"$type":"Issue","id":"3-99","links":[` +
				targetIssueLink(theOtherEnd(link.direction), link.kind.id,
					sourceUnder(sourceIssueLink(link.direction, link.kind, record))) + `]}`,
		},
		{
			name: "an issue with no links at all",
			written: writeAnswerOfTheTarget(targetIssueLink(theOtherEnd(link.direction), link.kind.id,
				`{"$type":"Issue","id":"`+addedSourceID+`"}`)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget),
				fake.JSON(http.StatusOK, tc.written))

			got := runWith(t, server.Env(), "link", "add", addedSource, "depends on", addedTarget)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []detail{{"issue", addedSource}, {"phrase", "depends on"}, {"target", addedTarget}},
				found.details[1:4])
		})
	}
}

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
			missing: addedTarget,
			paths:   []string{"/api/issues/DEV-1", "/api/issues/DEV-2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost:
					assert.Fail(t, "a write reached the server", "%s %s", r.Method, r.URL)
				case path.Base(r.URL.Path) == tc.missing:
					fake.JSON(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id `+
						tc.missing+` not found"}`)(w, r)
				default:
					fake.JSON(http.StatusOK, devInstanceCatalogue())(w, r)
				}
			})

			got := runWith(t, server.Env(), "link", "add", addedSource, "depends on", addedTarget)

			found := requireFault(t, got)
			assert.Equal(t, "not_found", found.code)
			assert.Equal(t, "Entity with id "+tc.missing+" not found", detailNamed(t, found, "upstream_message"))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestLinkAddCarriesWhatTheServerSaidAboutTheWrite(t *testing.T) {
	t.Parallel()
	const cycleWithHTMLEntities = "Subtask &mdash; обнаружена циклическая связь: DEV-26 &rarr; DEV-25 &rarr; DEV-26"
	tests := []struct {
		name  string
		write http.HandlerFunc
		code  string
		exit  int
	}{
		{
			name: "a write the server refused",
			write: fake.JSON(http.StatusBadRequest,
				`{"error":"invalid_properties","error_description":`+strconv.Quote(cycleWithHTMLEntities)+`}`),
			code: "rejected",
			exit: 1,
		},
		{name: "a write whose answer never came", write: breakOff, code: "write_uncertain", exit: 2},
		{name: "a write answered by a gateway", write: gateway(http.StatusBadGateway), code: "write_uncertain", exit: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget), tc.write)

			got := runWith(t, server.Env(), "link", "add", addedSource, "depends on", addedTarget)

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, []detail{{"issue", addedSource}, {"phrase", "depends on"}, {"target", addedTarget}},
				found.details[1:4])
			if tc.code == "rejected" {
				assert.Equal(t, cycleWithHTMLEntities, detailNamed(t, found, "upstream_message"))
			}
		})
	}
}

func TestLinkAddPrintsATargetByWhatWasAskedOfIt(t *testing.T) {
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
			record:     `{"$type":"Issue","id":"` + addedTargetID + `","idReadable":"` + addedTarget + `"}`,
			asked:      "idReadable",
			printed:    []detail{{"idReadable", addedTarget}},
		},
		{
			name:       "a block of the target issue added to the default",
			expression: "+customFields",
			record: `{"$type":"Issue","id":"` + addedTargetID + `","idReadable":"` + addedTarget +
				`","summary":"X","customFields":` + receivedFields(state) + `}`,
			asked:   linkListTarget + "," + customFieldsFields,
			printed: []detail{{"idReadable", addedTarget}, {"summary", "X"}, {"customFields", []detail{{"State", "Новая"}}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linking(t, devInstanceCatalogue(), addressedIssue(addedTargetID, addedTarget),
				fake.JSON(http.StatusOK, linkWritten(link, sourceIssueLink(link.direction, link.kind, tc.record))))

			got := runWith(t, server.Env(), "link", "add", addedSource, "depends on", addedTarget,
				"--fields", tc.expression)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []detail{{"depends on", []any{tc.printed}}}, nodeValue(t, got.stdout, "links"))
			assert.Equal(t, addWriteFields(tc.asked), server.Fields()[2])
		})
	}
}

func noLinkWritten(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a write reached the server", "%s %s", r.Method, r.URL)
	}
}

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
