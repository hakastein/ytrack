package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func shownIssue() string {
	return `{"summary":"First","$type":"Issue","id":"3-19",` +
		`"tags":[{"name":"Tag","$type":"IssueTag"}],"reporter":{"$type":"User","login":"reporter"},` +
		`"created":1788134400000,"updated":1788134401000,"resolved":null,"idReadable":"DEV-1",` +
		`"customFields":` + receivedFields(
		receivedField{name: "State", valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("Open")},
		receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
	) + `,"links":` + receivedLinks(receivedLink{
		direction: "INWARD", sourceToTarget: "source to target", targetToSource: "target to source",
		issues: []string{targetIssue("DEV-2", "Second")},
	}) + `,"description":"First line\nSecond line","comments":[]}`
}

const printedIssue = `idReadable: "DEV-1"
summary: "First"
reporter:
  login: "reporter"
created: "2026-08-31T00:00:00Z"
updated: "2026-08-31T00:00:01Z"
resolved: null
tags:
  - {name: "Tag"}
customFields:
  "Type": "Task"
  "State": "Open"
links:
  "target to source":
    - {idReadable: "DEV-2", summary: "Second"}
description: |-
  First line
  Second line
comments: []
`

const askedIssueFields = "idReadable,summary,reporter(login),created,updated,resolved,tags(name)," +
	customFieldsFields + ",links(issues(idReadable,summary)," + linkParts + "),description"

const sentIssueFields = askedIssueFields + "," + commentFields

func issueRequest(address, id, fields string) string {
	return "GET " + address + "/api/issues/" + url.PathEscape(id) + "?fields=" + fields
}

func TestIssueShowRefusesAnArgumentThatIsNoReadableID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "a dot", id: "."},
		{name: "two dots", id: ".."},
		{name: "a code alone", id: "DEV"},
		{name: "a code and a dash", id: "DEV-"},
		{name: "a letter after the number", id: "DEV-1x"},
		{name: "a path after the id", id: "DEV-1/.."},
		{name: "an escape in place of the number", id: "DEV-%31"},
		{name: "a line ending after the id", id: "DEV-1\n"},
		{name: "a space before the id", id: " DEV-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "issue", "show", tc.id)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestIssueShowRefusesTheInternalIDTheServerResolves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an internal id", id: "3-19"},
		{name: "an internal id with a leading zero", id: "03-19"},
		{name: "an internal id whose second number has a zero", id: "3-09"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "issue", "show", tc.id)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestIssueShowPrintsTheDefaultFieldsWithTheComments(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, shownIssue()))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1")

	assert.Equal(t, outcome{stdout: printedIssue}, got)
	assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
	sent := server.Request(t, 0)
	assert.Equal(t, http.MethodGet, sent.Method)
	assert.Equal(t, url.Values{"fields": {sentIssueFields}}, sent.URL.Query())
	assert.Equal(t, "Bearer "+fake.Token, sent.Header.Get("Authorization"))
	assert.Equal(t, "application/json", sent.Header.Get("Accept"))
}

func TestIssueShowSendsEveryIDTheFormAllows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      string
		escaped string
	}{
		{name: "a code in lower case", id: "dev-1", escaped: "dev-1"},
		{name: "a number with a leading zero", id: "DEV-01", escaped: "DEV-01"},
		{name: "an underscore in the code", id: "DEV_X-1", escaped: "DEV_X-1"},
		{name: "letters outside ASCII", id: "ДЕВ-1", escaped: "%D0%94%D0%95%D0%92-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, shownIssue()))

			got := runWith(t, server.Env(), "issue", "show", tc.id)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			requests := server.Requests()
			require.Len(t, requests, 1)
			assert.Equal(t, "/api/issues/"+tc.escaped, requests[0].URL.EscapedPath())
			assert.Equal(t, "/api/issues/"+tc.id, requests[0].URL.Path)
		})
	}
}

func requireDocument(t *testing.T, stdout string) []detail {
	t.Helper()
	pairs := []detail{}
	for pair := range slices.Chunk(requireMapping(t, "stdout", stdout).Content, 2) {
		pairs = append(pairs, detail{key: pair[0].Value, value: requireValue(t, pair[1])})
	}
	return pairs
}
