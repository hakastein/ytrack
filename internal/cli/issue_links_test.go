package cli_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// What the tool asks of every link slot of an issue beside the issues at its other end: the end this issue
// stands at and both phrases of the type, which together settle the one phrase the slot is printed under.
const linkParts = "direction,linkType(sourceToTarget,targetToSource)"

// linksFields is the fields= one slot goes out as, with what the caller asked of the partner issues.
func linksFields(slot, partner string) string {
	return slot + "(issues(" + partner + ")," + linkParts + ")"
}

// A link slot as the server sends it: one end of one type, with the issues standing at the other end.
type arrivedLink struct {
	direction      string
	sourceToTarget string
	targetToSource string
	// The issues at the other end, as JSON; none at all where the slot is empty.
	issues []string
}

func (l arrivedLink) sent(id string) string {
	return `{"$type":"IssueLink","id":` + strconv.Quote(id) +
		`,"direction":` + strconv.Quote(l.direction) +
		`,"linkType":{"$type":"IssueLinkType","name":"Type","sourceToTarget":` + strconv.Quote(l.sourceToTarget) +
		`,"targetToSource":` + strconv.Quote(l.targetToSource) +
		`},"issues":[` + strings.Join(l.issues, ",") + `]}`
}

// arrivedLinks is the array of slots one answer holds, each under the id the instance generated for it.
func arrivedLinks(links ...arrivedLink) string {
	sent := make([]string, 0, len(links))
	for i, link := range links {
		sent = append(sent, link.sent("163-"+strconv.Itoa(i)))
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func partnerIssue(id, summary string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(id) + `,"summary":` + strconv.Quote(summary) + `}`
}

func issueWithLinks(links ...arrivedLink) string {
	return `{"$type":"Issue","idReadable":"DEV-1","links":` + arrivedLinks(links...) + `}`
}

// The nine slots every issue of the polygon carries, all of them empty.
func emptySlots() []arrivedLink {
	return []arrivedLink{
		{direction: "BOTH", sourceToTarget: "relates to"},
		{direction: "OUTWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
		{direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
		{direction: "OUTWARD", sourceToTarget: "is duplicated by", targetToSource: "duplicates"},
		{direction: "INWARD", sourceToTarget: "is duplicated by", targetToSource: "duplicates"},
		{direction: "OUTWARD", sourceToTarget: "parent for", targetToSource: "subtask of"},
		{direction: "INWARD", sourceToTarget: "parent for", targetToSource: "subtask of"},
		{direction: "OUTWARD", sourceToTarget: "Скопирована в", targetToSource: "Копия"},
		{direction: "INWARD", sourceToTarget: "Скопирована в", targetToSource: "Копия"},
	}
}

// showLinks is the block one answer prints under links, as the mapping of phrases.
func showLinks(t *testing.T, body, expression string) (outcome, *yaml.Node) {
	t.Helper()
	server := serve(t, answer(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", expression)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{linksFields("links", "idReadable")}, server.sentFields())
	block := nodeAt(t, requireMapping(t, "stdout", got.stdout), "links")
	require.Equal(t, yaml.MappingNode, block.Kind, "stdout: %q", got.stdout)
	return got, block
}

// keysOf is the keys of a mapping in the order printed.
func keysOf(node *yaml.Node) []string {
	keys := []string{}
	for pair := range slices.Chunk(node.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}

// A phrase is read from the end the issue stands at: the same type stands in two slots, and the issue at the
// target of a directed link is the one the link points to, not the one it points from.
func TestIssueShowPrintsALinkUnderThePhraseOfItsOwnEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		link   arrivedLink
		phrase string
	}{
		{
			name:   "at the source of a directed link",
			link:   arrivedLink{direction: "OUTWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
			phrase: "is required for",
		},
		{
			name:   "at the target of a directed link",
			link:   arrivedLink{direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
			phrase: "depends on",
		},
		{
			// An undirected type reads the same from either end and leaves the phrase back empty.
			name:   "at either end of an undirected link",
			link:   arrivedLink{direction: "BOTH", sourceToTarget: "relates to"},
			phrase: "relates to",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.link.issues = []string{partnerIssue("DEV-2", "Отклонённая задача")}

			got, block := showLinks(t, issueWithLinks(tc.link), "links")

			assert.Equal(t, []string{tc.phrase}, keysOf(block), "stdout: %q", got.stdout)
			assert.Equal(t, yaml.DoubleQuotedStyle, block.Content[0].Style, "the phrase stands bare")
		})
	}
}

// Every issue carries a slot for each end of each type the instance has, and all but a few of them are empty:
// an empty slot is no link, so it is left out, and an issue with no link at all prints the key empty.
func TestIssueShowLeavesOutTheEmptyLinkSlots(t *testing.T) {
	t.Parallel()

	got, block := showLinks(t, issueWithLinks(emptySlots()...), "links")

	assert.Empty(t, block.Content, "stdout: %q", got.stdout)
	assert.NotContains(t, got.stdout, "163-")
}

// The caller who asks nothing of the issues at the other end is answered the id each of them is addressed by,
// which is what goes out as well: a link is of no use without a way to name the issue it reaches.
func TestIssueShowAsksForTheReadableIDOfEveryPartnerByDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the slot alone", expression: "links"},
		{name: "the issues of the slot alone", expression: "links(issues)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithLinks(arrivedLink{
				direction: "BOTH", sourceToTarget: "relates to",
				issues: []string{partnerIssue("DEV-2", "Отклонённая задача")},
			})

			got, _ := showLinks(t, body, tc.expression)

			assert.Equal(t, []detail{{"links", []detail{
				{"relates to", []any{[]detail{{"idReadable", "DEV-2"}}}},
			}}}, requireDocument(t, got.stdout))
			assert.NotContains(t, got.stdout, "Отклонённая задача")
		})
	}
}

// A block of phrases cannot carry a link the server names nothing, and two links of one phrase would print as
// one key, so either is the end of the call rather than a document missing a link.
func TestIssueShowRefusesLinksTheServerNamesBadly(t *testing.T) {
	t.Parallel()
	partner := []string{partnerIssue("DEV-2", "Отклонённая задача")}
	tests := []struct {
		name    string
		arrived []arrivedLink
	}{
		{
			name: "two links of one phrase",
			arrived: []arrivedLink{
				{direction: "OUTWARD", sourceToTarget: "X", targetToSource: "Y", issues: partner},
				{direction: "BOTH", sourceToTarget: "X", issues: partner},
			},
		},
		{
			name: "a link holding issues and going by no phrase",
			arrived: []arrivedLink{
				{direction: "INWARD", sourceToTarget: "is required for", targetToSource: "", issues: partner},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithLinks(tc.arrived...)
			server := serve(t, answer(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "links")

			assert.Equal(t, refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", linksFields("links", "idReadable"))},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireRefusal(t, got))
		})
	}
}

// A link slot with each member written as it stands, so that a scenario may send a shape the specification
// does not allow while every name asked for is there: a name the answer lacks altogether is the judgment's to
// refuse, and what is left for the block to hold against the specification is the shape.
func slotHolding(issues, direction, linkType string) string {
	return `{"$type":"IssueLink","id":"163-0","issues":` + issues + `,"direction":` + direction +
		`,"linkType":` + linkType + `}`
}

// The slots are read whole before any of them is printed, so a part standing in a shape the specification does
// not give it ends the call rather than printing a block with a link missing or under the wrong phrase.
func TestIssueShowRefusesLinksOfAShapeTheSpecificationDoesNotGive(t *testing.T) {
	t.Parallel()
	const partners = `[{"$type":"Issue","idReadable":"DEV-2"}]`
	const linkType = `{"$type":"IssueLinkType","sourceToTarget":"relates to","targetToSource":"relates to"}`
	tests := []struct {
		name string
		// What stands under links, as JSON.
		slots string
	}{
		{name: "a slot is no object", slots: `[[` + slotHolding(partners, `"BOTH"`, linkType) + `]]`},
		{name: "the issues of a slot are no array", slots: `[` + slotHolding(`null`, `"BOTH"`, linkType) + `]`},
		{name: "an issue at the other end is no object", slots: `[` + slotHolding(`[null]`, `"BOTH"`, linkType) + `]`},
		{name: "the end the issue stands at is no text", slots: `[` + slotHolding(partners, `null`, linkType) + `]`},
		{name: "the type of a link is no object", slots: `[` + slotHolding(partners, `"BOTH"`, `null`) + `]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","idReadable":"DEV-1","links":` + tc.slots + `}`
			server := serve(t, answer(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "links")

			assert.Equal(t, refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", linksFields("links", "idReadable"))},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireRefusal(t, got))
		})
	}
}

// A slot is printed as the phrase against the issues it reaches, so the issues are the only thing there is to
// ask of it: its id, its direction and its type are how YouTrack holds a link, not what the link is.
func TestIssueShowRefusesNamesWrittenUnderALinkSlot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the id of the slot", expression: "links(id)"},
		{name: "the end the issue stands at", expression: "links(direction)"},
		{name: "the type of the link", expression: "links(linkType(name))"},
		{name: "the id of the parent slot", expression: "parent(id)"},
		{name: "the trimmed issues of the subtasks slot", expression: "subtasks(trimmedIssues(idReadable))"},
		{name: "under the issues of a link", expression: "links(issues(parent(id)))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--fields", tc.expression)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// DEV-1 of the polygon stands at one end of each of the five types: the phrases read outwards from it, the
// order is the one the server keeps the slots in, and the slots it shares no issue with are not printed. The
// id of a slot and the end it stands at are the server's own way of holding a link and never leave ytrack.
func TestIssueShowPrintsTheLinksOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-1",
		"--fields", "links(issues(idReadable,summary))", "--comments=0")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{{"links", []detail{
		{"relates to", []any{[]detail{{"idReadable", "DEV-2"}, {"summary", "Отклонённая задача"}}}},
		{"depends on", []any{[]detail{{"idReadable", "DEV-3"}, {"summary", "Блокирующая задача"}}}},
		{"is duplicated by", []any{[]detail{{"idReadable", "DEV-5"}, {"summary", "Дубль задачи в работе"}}}},
		{"subtask of", []any{[]detail{{"idReadable", "DEV-4"}, {"summary", "Родительская задача"}}}},
		{"Скопирована в", []any{[]detail{{"idReadable", "DEV-6"}, {"summary", "Копия задачи в работе"}}}},
	}}}, requireDocument(t, got.stdout))
	for _, hidden := range []string{"163-", "INWARD", "OUTWARD", "BOTH"} {
		assert.NotContains(t, got.stdout, hidden)
	}
	assert.Equal(t, []string{linksFields("links", "idReadable,summary")}, dev.sentFields())
	assert.Len(t, dev.requests(), 1)
}

// parent and subtasks are slots of the same type read from either end, so they print the same way links does:
// DEV-4 is the parent of DEV-1, and the slot for a parent of its own is empty.
func TestIssueShowPrintsTheParentAndSubtaskSlotsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-4", "--fields", "links,parent,subtasks", "--comments=0")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	parentFor := []detail{{"parent for", []any{[]detail{{"idReadable", "DEV-1"}}}}}
	assert.Equal(t, []detail{
		{"links", parentFor},
		{"parent", []detail{}},
		{"subtasks", parentFor},
	}, requireDocument(t, got.stdout))
	assert.Equal(t, []string{strings.Join([]string{
		linksFields("links", "idReadable"),
		linksFields("parent", "idReadable"),
		linksFields("subtasks", "idReadable"),
	}, ",")}, dev.sentFields())
}
