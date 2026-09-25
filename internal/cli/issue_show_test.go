package cli_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func issueInProgress() string {
	return `{"summary":"[bug] fix login","$type":"Issue","id":"3-19",` +
		`"tags":[{"name":"история-полигона","$type":"IssueTag"}],"reporter":{"$type":"User","login":"admin"},` +
		`"created":1789035410875,"updated":1787942509046,"resolved":null,"idReadable":"DEV-1",` +
		`"customFields":` + receivedFields(
		receivedField{name: "State", valueType: "state", ordinal: "8", binding: "180-14",
			value: bundleElement("In Progress")},
		receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15",
			value: bundleElement("Task")},
	) + `,"links":` + receivedLinks(receivedLink{
		direction: "INWARD", sourceToTarget: "parent for", targetToSource: "subtask of",
		issues: []string{targetIssue("DEV-4", "Родительская задача")},
	}) + `,"description":"Шаги:\n1. открыть\n2. войти","comments":[]}`
}

const printedInProgress = `idReadable: "DEV-1"
summary: "[bug] fix login"
reporter:
  login: "admin"
created: "2026-09-10T10:16:50.875Z"
updated: "2026-08-28T18:41:49.046Z"
resolved: null
tags:
  - {name: "история-полигона"}
customFields:
  "Type": "Task"
  "State": "In Progress"
links:
  "subtask of":
    - {idReadable: "DEV-4", summary: "Родительская задача"}
description: |-
  Шаги:
  1. открыть
  2. войти
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
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "show", tc.id)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
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
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "show", tc.id)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueShowPrintsTheFieldsAskedInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, issueInProgress()))

	got := runWith(t, server.env(), "issue", "show", "DEV-1")

	assert.Equal(t, outcome{stdout: printedInProgress}, got)
	requests := server.requests()
	require.Len(t, requests, 1)
	request := requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "/api/issues/DEV-1", request.URL.Path)
	assert.Equal(t, url.Values{"fields": {sentIssueFields}}, request.URL.Query())
	assert.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
	assert.Equal(t, "application/json", request.Header.Get("Accept"))
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
			server := serve(t, respondWith(http.StatusOK, issueInProgress()))

			got := runWith(t, server.env(), "issue", "show", tc.id)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			requests := server.requests()
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
