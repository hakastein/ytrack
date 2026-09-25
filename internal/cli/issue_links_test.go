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

const linkParts = "direction,linkType(sourceToTarget,targetToSource)"

func linksFields(link, target string) string {
	return link + "(issues(" + target + ")," + linkParts + ")"
}

type receivedLink struct {
	direction      string
	sourceToTarget string
	targetToSource string
	issues         []string
}

func (l receivedLink) sent(id string) string {
	return `{"$type":"IssueLink","id":` + strconv.Quote(id) +
		`,"direction":` + strconv.Quote(l.direction) +
		`,"linkType":{"$type":"IssueLinkType","name":"Type","sourceToTarget":` + strconv.Quote(l.sourceToTarget) +
		`,"targetToSource":` + strconv.Quote(l.targetToSource) +
		`},"issues":[` + strings.Join(l.issues, ",") + `]}`
}

func receivedLinks(links ...receivedLink) string {
	sent := make([]string, 0, len(links))
	for i, link := range links {
		sent = append(sent, link.sent("163-"+strconv.Itoa(i)))
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func targetIssue(id, summary string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(id) + `,"summary":` + strconv.Quote(summary) + `}`
}

func issueWithLinks(links ...receivedLink) string {
	return `{"$type":"Issue","idReadable":"DEV-1","links":` + receivedLinks(links...) + `}`
}

func emptyIssueLinks() []receivedLink {
	return []receivedLink{
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

func showLinks(t *testing.T, body, expression string) (outcome, *yaml.Node) {
	t.Helper()
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", expression)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{linksFields("links", "idReadable")}, server.sentFields())
	block := nodeAt(t, requireMapping(t, "stdout", got.stdout), "links")
	require.Equal(t, yaml.MappingNode, block.Kind, "stdout: %q", got.stdout)
	return got, block
}

func keysOf(node *yaml.Node) []string {
	keys := []string{}
	for pair := range slices.Chunk(node.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}

func TestIssueShowPrintsALinkUnderThePhraseOfItsOwnEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		link   receivedLink
		phrase string
	}{
		{
			name:   "at the source of a directed link",
			link:   receivedLink{direction: "OUTWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
			phrase: "is required for",
		},
		{
			name:   "at the target of a directed link",
			link:   receivedLink{direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
			phrase: "depends on",
		},
		{
			name:   "at either end of an undirected link",
			link:   receivedLink{direction: "BOTH", sourceToTarget: "relates to"},
			phrase: "relates to",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.link.issues = []string{targetIssue("DEV-2", "Отклонённая задача")}

			got, block := showLinks(t, issueWithLinks(tc.link), "links")

			assert.Equal(t, []string{tc.phrase}, keysOf(block), "stdout: %q", got.stdout)
			assert.Equal(t, yaml.DoubleQuotedStyle, block.Content[0].Style, "the phrase stands bare")
		})
	}
}

func TestIssueShowLeavesOutTheEmptyLinkSlots(t *testing.T) {
	t.Parallel()

	got, block := showLinks(t, issueWithLinks(emptyIssueLinks()...), "links")

	assert.Empty(t, block.Content, "stdout: %q", got.stdout)
	assert.NotContains(t, got.stdout, "163-")
}

func TestIssueShowAsksForTheReadableIDOfEveryTargetByDefault(t *testing.T) {
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
			body := issueWithLinks(receivedLink{
				direction: "BOTH", sourceToTarget: "relates to",
				issues: []string{targetIssue("DEV-2", "Отклонённая задача")},
			})

			got, _ := showLinks(t, body, tc.expression)

			assert.Equal(t, []detail{{"links", []detail{
				{"relates to", []any{[]detail{{"idReadable", "DEV-2"}}}},
			}}}, requireDocument(t, got.stdout))
			assert.NotContains(t, got.stdout, "Отклонённая задача")
		})
	}
}

func TestIssueShowRefusesLinksTheServerNamesBadly(t *testing.T) {
	t.Parallel()
	target := []string{targetIssue("DEV-2", "Отклонённая задача")}
	tests := []struct {
		name     string
		received []receivedLink
	}{
		{
			name: "two links of one phrase",
			received: []receivedLink{
				{direction: "OUTWARD", sourceToTarget: "X", targetToSource: "Y", issues: target},
				{direction: "BOTH", sourceToTarget: "X", issues: target},
			},
		},
		{
			name: "a link holding issues and going by no phrase",
			received: []receivedLink{
				{direction: "INWARD", sourceToTarget: "is required for", targetToSource: "", issues: target},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithLinks(tc.received...)
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "links")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", linksFields("links", "idReadable"))},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireFault(t, got))
		})
	}
}

func issueLinkWith(issues, direction, linkType string) string {
	return `{"$type":"IssueLink","id":"163-0","issues":` + issues + `,"direction":` + direction +
		`,"linkType":` + linkType + `}`
}

func TestIssueShowRefusesLinksOfAShapeTheSpecificationDoesNotGive(t *testing.T) {
	t.Parallel()
	const targets = `[{"$type":"Issue","idReadable":"DEV-2"}]`
	const linkType = `{"$type":"IssueLinkType","sourceToTarget":"relates to","targetToSource":"relates to"}`
	tests := []struct {
		name  string
		links string
	}{
		{name: "a slot is no object", links: `[[` + issueLinkWith(targets, `"BOTH"`, linkType) + `]]`},
		{name: "the issues of a slot are no array", links: `[` + issueLinkWith(`null`, `"BOTH"`, linkType) + `]`},
		{name: "an issue at the other end is no object", links: `[` + issueLinkWith(`[null]`, `"BOTH"`, linkType) + `]`},
		{name: "the end the issue stands at is no text", links: `[` + issueLinkWith(targets, `null`, linkType) + `]`},
		{name: "the type of a link is no object", links: `[` + issueLinkWith(targets, `"BOTH"`, `null`) + `]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","idReadable":"DEV-1","links":` + tc.links + `}`
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "links")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", linksFields("links", "idReadable"))},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireFault(t, got))
		})
	}
}

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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

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
