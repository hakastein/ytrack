package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const issueReadPhraseFields = "direction,linkType(sourceToTarget,targetToSource)"

const issueReadTarget = `[{"$type":"Issue","idReadable":"DEV-2","summary":"Second"}]`

func issueReadLink(issues, direction, linkType string) string {
	return `{"$type":"IssueLink","issues":` + issues + `,"direction":` + direction + `,"linkType":` + linkType + `}`
}

func issueReadLinkType(sourceToTarget, targetToSource string) string {
	return `{"$type":"IssueLinkType","sourceToTarget":` + sourceToTarget + `,"targetToSource":` + targetToSource + `}`
}

func issueReadDirected() string {
	return issueReadLinkType(`"source to target"`, `"target to source"`)
}

func issueReadLinked(phrase string, targets ...*render.Node) *render.Node {
	return render.NewMap(render.FromData(phrase, render.NewList(targets...)))
}

func issueReadTargetID() *render.Node {
	return render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-2")})
}

func TestShowIssuePrintsALinkUnderThePhraseOfItsEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		direction string
		phrase    string
	}{
		{name: "at the source of a directed link", direction: `"OUTWARD"`, phrase: "source to target"},
		{name: "at the target of a directed link", direction: `"INWARD"`, phrase: "target to source"},
		{name: "at either end of an undirected link", direction: `"BOTH"`, phrase: "source to target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","links":[` + issueReadLink(issueReadTarget, tc.direction, issueReadDirected()) + `]}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, fault := issueReadShown(t, server, "links", youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(render.Pair{Key: "links", Value: issueReadLinked(tc.phrase, issueReadTargetID())}), node)
		})
	}
}

func TestShowIssuePrintsTheParentAndTheSubtasksAsLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		direction  string
		phrase     string
	}{
		{name: "the parent", expression: "parent", direction: `"INWARD"`, phrase: "target to source"},
		{name: "the subtasks", expression: "subtasks", direction: `"OUTWARD"`, phrase: "source to target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","` + tc.expression + `":` + issueReadLink(issueReadTarget, tc.direction, issueReadDirected()) + `}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(render.Pair{Key: tc.expression, Value: issueReadLinked(tc.phrase, issueReadTargetID())}), node)
		})
	}
}

func TestShowIssueLeavesOutALinkThatHoldsNoIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","links":[`+
		issueReadLink(`[]`, `"OUTWARD"`, issueReadDirected())+`,`+
		issueReadLink(`[]`, `"INWARD"`, issueReadDirected())+`,`+
		issueReadLink(`[]`, `"BOTH"`, issueReadLinkType(`"undirected"`, `""`))+`,`+
		issueReadLink(`[]`, `"INWARD"`, issueReadLinkType(`"source to target"`, `null`))+`]}`))

	node, fault := issueReadShown(t, server, "links", youtrack.Comments{})

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(render.Pair{Key: "links", Value: render.NewMap([]render.Pair{}...)}), node)
}

func TestShowIssuePrintsTheTargetsOfALinkByTheirReadableIDUnlessAskedOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		target     *render.Node
	}{
		{name: "the slot alone", expression: "links", target: issueReadTargetID()},
		{name: "the issues of the slot alone", expression: "links(issues)", target: issueReadTargetID()},
		{
			name:       "a field of the issues",
			expression: "links(issues(summary))",
			target:     render.NewMap(render.Pair{Key: "summary", Value: render.NewString("Second")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","links":[` + issueReadLink(issueReadTarget, `"OUTWARD"`, issueReadDirected()) + `]}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(render.Pair{Key: "links", Value: issueReadLinked("source to target", tc.target)}), node)
		})
	}
}

func TestShowIssueAsksForThePhraseOfEveryLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		sent       string
	}{
		{name: "the slot alone", expression: "links", sent: "links(issues(idReadable)," + issueReadPhraseFields + ")"},
		{
			name:       "a field of the issues",
			expression: "links(issues(summary))",
			sent:       "links(issues(summary)," + issueReadPhraseFields + ")",
		},
		{name: "the parent", expression: "parent", sent: "parent(issues(idReadable)," + issueReadPhraseFields + ")"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","links":[],"parent":null}`))

			_, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestShowIssueRefusesLinksOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		links string
	}{
		{
			name: "two links of one phrase",
			links: `[` + issueReadLink(issueReadTarget, `"OUTWARD"`, issueReadLinkType(`"twin"`, `"other"`)) + `,` +
				issueReadLink(issueReadTarget, `"BOTH"`, issueReadLinkType(`"twin"`, `""`)) + `]`,
		},
		{
			name:  "a link holding issues under an empty phrase",
			links: `[` + issueReadLink(issueReadTarget, `"INWARD"`, issueReadLinkType(`"source to target"`, `""`)) + `]`,
		},
		{
			name:  "a link holding issues under a phrase that is no text",
			links: `[` + issueReadLink(issueReadTarget, `"INWARD"`, issueReadLinkType(`"source to target"`, `null`)) + `]`,
		},
		{name: "a slot that is no object", links: `[[` + issueReadLink(issueReadTarget, `"BOTH"`, issueReadDirected()) + `]]`},
		{name: "a slot that is null", links: `[null,` + issueReadLink(issueReadTarget, `"BOTH"`, issueReadDirected()) + `]`},
		{name: "the issues of a slot are no array", links: `[` + issueReadLink(`null`, `"BOTH"`, issueReadDirected()) + `]`},
		{name: "an issue at the other end is no object", links: `[` + issueReadLink(`[null]`, `"BOTH"`, issueReadDirected()) + `]`},
		{name: "the end the issue stands at is no text", links: `[` + issueReadLink(issueReadTarget, `null`, issueReadDirected()) + `]`},
		{name: "the type of a link is no object", links: `[` + issueReadLink(issueReadTarget, `"BOTH"`, `null`) + `]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","links":` + tc.links + `}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, fault := issueReadShown(t, server, "links", youtrack.Comments{})

			target := issueReadPath + "?fields=links(issues(idReadable)," + issueReadPhraseFields + ")"
			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, target), body), faultOf(t, fault))
		})
	}
}

func TestShowIssuePrintsLinksTheServerSentAsNullAsNull(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		body       string
		key        string
	}{
		{name: "the links", expression: "links", body: `{"$type":"Issue","links":null}`, key: "links"},
		{name: "the parent", expression: "parent", body: `{"$type":"Issue","parent":null}`, key: "parent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			node, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(render.Pair{Key: tc.key, Value: render.NewNull()}), node)
		})
	}
}
