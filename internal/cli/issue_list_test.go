package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	issuesPath = "/api/issues"
	countPath  = "/api/issuesGetter/count"
)

const (
	namedState = "State"
	namedType  = "Type"
)

const sentIssueListFields = "idReadable,summary," + translatedCustomFieldsFields + ",created"

type listedIssue struct {
	id                 string
	internal           string
	summary            string
	state              string
	createdEpochMillis string
}

func (i listedIssue) sent() string {
	return `{"summary":` + strconv.Quote(i.summary) + `,"$type":"Issue","id":` + strconv.Quote(i.internal) +
		`,"customFields":` + receivedFields(
		receivedField{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
		receivedField{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement(i.state)},
	) + `,"idReadable":` + strconv.Quote(i.id) + `,"created":` + i.createdEpochMillis + `}`
}

func listedDEV1() string {
	return listedIssue{id: "DEV-1", internal: "3-19", summary: "First",
		state: "Open", createdEpochMillis: "1788134400875"}.sent()
}

func listedDEV2() string {
	return listedIssue{id: "DEV-2", internal: "3-20", summary: "Second",
		state: "Closed", createdEpochMillis: "1788134401000"}.sent()
}

const (
	printedDEV1Row = `  - {idReadable: "DEV-1", summary: "First", ` +
		`customFields: {"State": "Open", "Type": "Task"}, created: "2026-08-31T00:00:00.875Z"}` + "\n"
	printedDEV2Row = `  - {idReadable: "DEV-2", summary: "Second", ` +
		`customFields: {"State": "Closed", "Type": "Task"}, created: "2026-08-31T00:00:01Z"}` + "\n"
	printedDEV3Row = `  - {idReadable: "DEV-3", summary: "Third", ` +
		`customFields: {"State": "New", "Type": "Task"}, created: "2026-08-31T00:00:02.4Z"}` + "\n"
)

func countHandler(count string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, `{"$type":"IssueCountResponse","count":`+count+`}`)
}

func sentTo(server *fake.Server, path string) int {
	sent := 0
	for _, p := range server.Paths() {
		if p == path {
			sent++
		}
	}
	return sent
}

func TestIssueListTakesItsSearchFromTheQueryFlagAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no search at all", argv: []string{"issue", "list"}},
		{name: "a search that is no UTF-8", argv: []string{"issue", "list", "--query", "\xff"}},
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

func TestIssueListRefusesCommentsAskedForInTheExpression(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "issue", "list", "--query", "", "--fields", "+comments")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestIssueListPrintsTheIssuesTheSearchFinds(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[`+listedDEV1()+`,`+listedDEV2()+`]`)))

	got := runWith(t, server.Env(), "issue", "list", "--query", " project: DEV ", "--limit", "3")

	want := "total: 2\nreturned: 2\ntruncated: false\nissues:\n" + printedDEV1Row + printedDEV2Row
	assert.Equal(t, outcome{stdout: want}, got)
	requireMarkedUpFirst(t, server, " project: DEV ")
	assert.Equal(t, []string{fake.AssistPath, issuesPath}, server.Paths())
	assert.Equal(t, url.Values{
		"query":        {" project: DEV "},
		"customFields": {namedState, namedType},
		"fields":       {sentIssueListFields},
		"$top":         {"3"},
	}, server.Request(t, 1).URL.Query())
}

func recordKeys(record *yaml.Node) []string {
	var keys []string
	for pair := range slices.Chunk(record.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}
