package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const linkListTarget = "idReadable,summary"

func linkListFields(target string) string {
	return "links(issues(" + target + ")," + linkParts + ",issuesSize)"
}

type issueLinkEntry struct {
	id             string
	direction      string
	sourceToTarget string
	targetToSource string
	issues         []string
	held           string
	countless      bool
}

func linked(link receivedLink) issueLinkEntry {
	return issueLinkEntry{direction: link.direction, sourceToTarget: link.sourceToTarget,
		targetToSource: link.targetToSource, issues: link.issues}
}

func issueLinkEntries() []issueLinkEntry {
	links := make([]issueLinkEntry, 0, len(emptyIssueLinks()))
	for _, link := range emptyIssueLinks() {
		links = append(links, linked(link))
	}
	return links
}

func (s issueLinkEntry) sent(id string) string {
	if s.id != "" {
		id = s.id
	}
	size := ""
	if !s.countless {
		held := s.held
		if held == "" {
			held = strconv.Itoa(len(s.issues))
		}
		size = `,"issuesSize":` + held
	}
	return `{"$type":"IssueLink","id":` + strconv.Quote(id) +
		`,"direction":` + strconv.Quote(s.direction) +
		`,"linkType":{"$type":"IssueLinkType","name":"Type","sourceToTarget":` + strconv.Quote(s.sourceToTarget) +
		`,"targetToSource":` + strconv.Quote(s.targetToSource) + `}` + size +
		`,"issues":[` + strings.Join(s.issues, ",") + `]}`
}

func issueLinksResponse(links ...issueLinkEntry) string {
	sent := make([]string, 0, len(links))
	for i, link := range links {
		sent = append(sent, link.sent("163-"+strconv.Itoa(i)))
	}
	return `{"$type":"Issue","links":[` + strings.Join(sent, ",") + `]}`
}

func TestLinkListRefusesACallThatNamesNoOneIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the command alone", argv: []string{"link"}},
		{name: "a subcommand the command has none of", argv: []string{"link", "bogus"}},
		{name: "no issue", argv: []string{"link", "list"}},
		{name: "two issues", argv: []string{"link", "list", "DEV-1", "DEV-2"}},
		{name: "a path that would reach another endpoint", argv: []string{"link", "list", ".."}},
		{name: "the id of an article", argv: []string{"link", "list", "DEV-A-1"}},
		{name: "an internal id", argv: []string{"link", "list", "3-19"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestLinkListHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"link", "list", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, linkListTarget)
	assert.Contains(t, got.stdout, "ytrack link list")
}

func TestLinkListRefusesAPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a limit", argv: []string{"--limit", "1"}},
		{name: "a skip", argv: []string{"--skip", "1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"link", "list", "DEV-1"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestLinkListRefusesNamesThatExistOnlyOnTheIssueAskedFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the comments of a target issue", expression: "comments(text)"},
		{name: "a custom field of a target issue named bare", expression: "+customFields(State)"},
		{name: "a custom field of a target issue named in double quotes", expression: `+customFields("State")`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "link", "list", "DEV-1", "--fields", tc.expression)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestLinkRefusesNamesWrittenUnderTheSlotOfATarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "link list", argv: []string{"link", "list", "DEV-1"}},
		{name: "link add", argv: []string{"link", "add", "DEV-1", "depends on", "DEV-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append(tc.argv, "--fields", "+links(id)")...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestLinkListPrintsThePhrasesOfAnIssueInTheOrderReceived(t *testing.T) {
	t.Parallel()
	links := issueLinkEntries()
	links[2].id = "42-1t"
	links[2].issues = []string{targetIssue("DEV-3", "[bug] fix login")}
	links[0].id = "42-0"
	links[0].issues = []string{targetIssue("DEV-9", "Y"), targetIssue("DEV-2", "X")}
	links[0], links[2] = links[2], links[0]
	body := issueLinksResponse(links...)
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "link", "list", "dev-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []detail{
		{"total", 3},
		{"returned", 3},
		{"truncated", false},
		{"links", []detail{
			{"depends on", []any{[]detail{{"idReadable", "DEV-3"}, {"summary", "[bug] fix login"}}}},
			{"relates to", []any{
				[]detail{{"idReadable", "DEV-9"}, {"summary", "Y"}},
				[]detail{{"idReadable", "DEV-2"}, {"summary", "X"}},
			}},
		}},
	}, requireDocument(t, got.stdout))
	for _, hidden := range []string{"$type", "42-1t", "42-0", "163-", "INWARD", "BOTH", "issuesSize"} {
		assert.NotContains(t, got.stdout, hidden)
	}
	assert.Equal(t, []string{"/api/issues/dev-1"}, server.sentPaths())
	assert.Equal(t, []string{linkListFields(linkListTarget)}, server.sentFields())
}

func TestLinkListPrintsNothingOfTheIssueAskedFor(t *testing.T) {
	t.Parallel()
	link := linked(receivedLink{
		direction: "BOTH", sourceToTarget: "relates to",
		issues: []string{targetIssue("DEV-2", "Отклонённая задача")},
	})
	server := serve(t, respondWith(http.StatusOK, issueLinksResponse(link)))

	got := runWith(t, server.env(), "link", "list", "dev-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"total", "returned", "truncated", "links"},
		keysOf(requireMapping(t, "stdout", got.stdout)))
	assert.NotContains(t, got.stdout, "DEV-1")
	assert.Equal(t, []string{"/api/issues/dev-1"}, server.sentPaths())
	assert.Equal(t, []string{linkListFields(linkListTarget)}, server.sentFields())
}

func TestLinkListPrintsAnIssueWithNoLinkAtAll(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, issueLinksResponse(issueLinkEntries()...)))

	got := runWith(t, server.env(), "link", "list", "DEV-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []detail{
		{"total", 0},
		{"returned", 0},
		{"truncated", false},
		{"links", []detail{}},
	}, requireDocument(t, got.stdout))
}

func TestLinkListCountsByWhatTheServerSaysAnIssueLinkHas(t *testing.T) {
	t.Parallel()
	targets := []string{
		targetIssue("DEV-2", "Отклонённая задача"),
		targetIssue("DEV-3", "Блокирующая задача"),
		targetIssue("DEV-4", "Родительская задача"),
	}

	t.Run("more than arrived", func(t *testing.T) {
		t.Parallel()
		link := issueLinkEntry{direction: "BOTH", sourceToTarget: "relates to", issues: targets, held: "5"}
		server := serve(t, respondWith(http.StatusOK, issueLinksResponse(link)))

		got := runWith(t, server.env(), "link", "list", "DEV-1")

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		document := requireDocument(t, got.stdout)
		assert.Equal(t, []detail{{"total", 5}, {"returned", 3}, {"truncated", true}}, document[:3])
	})

	t.Run("fewer than arrived", func(t *testing.T) {
		t.Parallel()
		link := issueLinkEntry{direction: "BOTH", sourceToTarget: "relates to", issues: targets, held: "1"}
		body := issueLinksResponse(link)
		server := serve(t, respondWith(http.StatusOK, body))

		got := runWith(t, server.env(), "link", "list", "DEV-1")

		found := requireFault(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
		assert.Equal(t, body, detailNamed(t, found, "upstream_body"))
	})

	t.Run("no count at all", func(t *testing.T) {
		t.Parallel()
		link := issueLinkEntry{direction: "BOTH", sourceToTarget: "relates to", issues: targets, countless: true}
		server := serve(t, respondWith(http.StatusOK, issueLinksResponse(link)))

		got := runWith(t, server.env(), "link", "list", "DEV-1")

		found := requireFault(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
		assert.Equal(t, []any{[]detail{{"field", "links(issuesSize)"}, {"type", "IssueLink"}}},
			detailNamed(t, found, "missing"))
	})

	t.Run("a count that is no whole number", func(t *testing.T) {
		t.Parallel()
		link := issueLinkEntry{direction: "BOTH", sourceToTarget: "relates to", issues: targets, held: "1.5"}
		server := serve(t, respondWith(http.StatusOK, issueLinksResponse(link)))

		got := runWith(t, server.env(), "link", "list", "DEV-1")

		found := requireFault(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
	})
}

func TestLinkListRefusesLinksTheServerNamesBadly(t *testing.T) {
	t.Parallel()
	target := []string{targetIssue("DEV-2", "Отклонённая задача")}
	tests := []struct {
		name  string
		links []issueLinkEntry
	}{
		{
			name: "two links of one phrase",
			links: []issueLinkEntry{
				{direction: "OUTWARD", sourceToTarget: "X", targetToSource: "Y", issues: target},
				{direction: "BOTH", sourceToTarget: "X", issues: target},
			},
		},
		{
			name: "a link holding issues and going by no phrase",
			links: []issueLinkEntry{
				{direction: "INWARD", sourceToTarget: "is required for", targetToSource: "", issues: target},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, issueLinksResponse(tc.links...)))

			got := runWith(t, server.env(), "link", "list", "DEV-1")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
		})
	}
}

func TestLinkListPrintsTheTextOfATargetOnItsLine(t *testing.T) {
	t.Parallel()
	const text = "первая\nвторая\xe2\x80\xa8третья"
	target := `{"$type":"Issue","idReadable":"DEV-2","summary":"Отклонённая задача","description":` +
		strconv.Quote(text) + `}`
	link := issueLinkEntry{direction: "BOTH", sourceToTarget: "relates to", issues: []string{target}}
	server := serve(t, respondWith(http.StatusOK, issueLinksResponse(link)))

	got := runWith(t, server.env(), "link", "list", "DEV-1", "--fields", "+description")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{linkListFields(linkListTarget + ",description")}, server.sentFields())
	record := nodeAt(t, requireMapping(t, "stdout", got.stdout), "links", "relates to")
	require.Equal(t, yaml.SequenceNode, record.Kind, "stdout: %q", got.stdout)
	assert.Equal(t, text, nodeAt(t, record.Content[0], "description").Value)
}

func TestLinkListPrintsTheLinksOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "link", "list", "DEV-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	printed := requireDocument(t, got.stdout)
	assert.Equal(t, []detail{{"total", 5}, {"returned", 5}, {"truncated", false}}, printed[:3])
	block := nodeAt(t, requireMapping(t, "stdout", got.stdout), "links")
	for phrase, target := range map[string]string{
		"relates to":       "DEV-2",
		"depends on":       "DEV-3",
		"is duplicated by": "DEV-5",
		"subtask of":       "DEV-4",
		"Скопирована в":    "DEV-6",
	} {
		assert.Equal(t, target, nodeAt(t, block, phrase, "idReadable").Value, "under %q", phrase)
	}
	assert.NotContains(t, got.stdout, "163-")
	assert.Len(t, dev.requests(), 1)
}

func TestLinkListPrintsTheOtherEndOfEveryLinkOfTheDevInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		issue  string
		phrase string
	}{
		{name: "the other end of relates", issue: "DEV-2", phrase: "relates to"},
		{name: "the other end of depends on", issue: "DEV-3", phrase: "is required for"},
		{name: "the other end of subtask of", issue: "DEV-4", phrase: "parent for"},
		{name: "the other end of is duplicated by", issue: "DEV-5", phrase: "duplicates"},
		{name: "the other end of the copy", issue: "DEV-6", phrase: "Копия"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, dev.env(), "link", "list", tc.issue)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			block := nodeAt(t, requireMapping(t, "stdout", got.stdout), "links")
			assert.Equal(t, "DEV-1", nodeAt(t, block, tc.phrase, "idReadable").Value, "under %q", tc.phrase)
			assert.NotContains(t, got.stdout, "163-")
		})
	}
}

func TestLinkListRefusesAnIssueTheDevInstanceDoesNotShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		issue   string
		limited bool
	}{
		{name: "an issue nobody has", issue: "DEV-99999"},
		{name: "an issue the limited user cannot see", issue: "DEV-1", limited: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)
			env := dev.env()
			if tc.limited {
				env = []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
			}

			got := runWith(t, env, "link", "list", tc.issue)

			assert.Equal(t, faultDocument{
				code: "not_found",
				details: []detail{
					{"request", issueRequest(dev.url, tc.issue, linkListFields(linkListTarget))},
					{"upstream_status", 404},
					{"upstream_error", "Not Found"},
					{"upstream_message", "Entity with id " + tc.issue + " not found"},
				},
			}, requireFault(t, got))
			assert.Len(t, dev.requests(), 1)
		})
	}
}
