package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

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
