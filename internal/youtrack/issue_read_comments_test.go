package youtrack_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const issueReadCommentFields = "comments(id,author(login),created,text,deleted)"

const issueReadCommentsFrom = 1788134400000

func issueReadComment(id string, second int, text string) string {
	return `{"$type":"IssueComment","id":` + strconv.Quote(id) + `,"author":{"$type":"User","login":"author"},` +
		`"created":` + strconv.Itoa(issueReadCommentsFrom+second*1000) + `,"text":` + strconv.Quote(text) + `,"deleted":false}`
}

func issueReadDeletedComment(id string, second int) string {
	return `{"$type":"IssueComment","id":` + strconv.Quote(id) + `,"author":{"$type":"User","login":"author"},` +
		`"created":` + strconv.Itoa(issueReadCommentsFrom+second*1000) + `,"text":null,"deleted":true}`
}

func issueReadPrintedComment(id, created, text string) *render.Node {
	return render.NewMap(
		render.Pair{Key: "id", Value: render.NewString(id)},
		render.Pair{Key: "author", Value: render.NewMap(render.Pair{Key: "login", Value: render.NewString("author")})},
		render.Pair{Key: "created", Value: render.NewString(created)},
		render.Pair{Key: "text", Value: render.NewText(text)},
	)
}

func TestShowIssuePrintsTheCommentsOldestFirst(t *testing.T) {
	t.Parallel()
	early := issueReadPrintedComment("7-2", "2026-08-31T00:00:00Z", "Early")
	middle := issueReadPrintedComment("7-3", "2026-08-31T00:00:01Z", "Middle")
	late := issueReadPrintedComment("7-1", "2026-08-31T00:00:03Z", "Late")
	body := `{"$type":"Issue","idReadable":"DEV-1","comments":[` +
		issueReadComment("7-3", 1, "Middle") + `,` +
		issueReadComment("7-1", 3, "Late") + `,` +
		issueReadDeletedComment("7-4", 2) + `,` +
		issueReadComment("7-2", 0, "Early") + `]}`
	tests := []struct {
		name     string
		comments string
		printed  []*render.Node
	}{
		{name: "every one of them", comments: "all", printed: []*render.Node{early, middle, late}},
		{name: "the last two", comments: "2", printed: []*render.Node{middle, late}},
		{name: "more than the issue has", comments: "10", printed: []*render.Node{early, middle, late}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comments, err := youtrack.ParseComments(tc.comments)
			require.NoError(t, err)
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, fault := issueReadShown(t, server, "idReadable", comments)

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(
				render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
				render.Pair{Key: "comments", Value: render.NewList(tc.printed...)},
			), node)
		})
	}
}

func TestShowIssueAsksForCommentsOnlyWhereItPrintsThem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		comments string
		sent     string
		printed  *render.Node
	}{
		{
			name:     "every comment",
			comments: "all",
			sent:     "idReadable," + issueReadCommentFields,
			printed: render.NewMap(
				render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
				render.Pair{Key: "comments", Value: render.NewList([]*render.Node{}...)},
			),
		},
		{
			name:     "none of them",
			comments: "0",
			sent:     "idReadable",
			printed:  render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comments, err := youtrack.ParseComments(tc.comments)
			require.NoError(t, err)
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","comments":[]}`))

			node, fault := issueReadShown(t, server, "idReadable", comments)

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestShowIssueRefusesCommentsOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		comments string
		received string
	}{
		{name: "no array at all", comments: "all", received: `null`},
		{name: "a null in place of a comment", comments: "all", received: `[null]`},
		{
			name:     "deleted as a word",
			comments: "all",
			received: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"author"},"created":0,` +
				`"text":"","deleted":"true"}]`,
		},
		{
			name:     "the moment written as a string",
			comments: "all",
			received: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"author"},"created":"0",` +
				`"text":"","deleted":false}]`,
		},
		{
			name:     "the moment written as a string on a comment the count leaves out",
			comments: "1",
			received: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"author"},"created":"0",` +
				`"text":"","deleted":false},` + issueReadComment("7-2", 1, "Late") + `]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comments, err := youtrack.ParseComments(tc.comments)
			require.NoError(t, err)
			body := `{"$type":"Issue","idReadable":"DEV-1","comments":` + tc.received + `}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, fault := issueReadShown(t, server, "idReadable", comments)

			target := issueReadPath + "?fields=idReadable," + issueReadCommentFields
			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, target), body), faultOf(t, fault))
		})
	}
}

func TestParseCommentsRefusesWhatIsNeitherAllNorACount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{name: "nothing", text: ""},
		{name: "a negative count", text: "-1"},
		{name: "a word of its own", text: "every"},
		{name: "a fraction", text: "1.5"},
		{name: "a count past what a number holds", text: "9223372036854775808"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := youtrack.ParseComments(tc.text)

			assert.Error(t, err)
		})
	}
}
