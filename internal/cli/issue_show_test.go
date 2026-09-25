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

const issueShowFields = "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields," +
	"links(issues(idReadable,summary)),description"

const askedIssueFields = "idReadable,summary,reporter(login),created,updated,resolved,tags(name)," +
	customFieldsFields + ",links(issues(idReadable,summary)," + linkParts + "),description"

const sentIssueFields = askedIssueFields + "," + commentFields

const instantForm = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?Z$`

func issueRequest(address, id, fields string) string {
	return "GET " + address + "/api/issues/" + url.PathEscape(id) + "?fields=" + fields
}

func noSuchIssue(address, id string) faultDocument {
	return faultDocument{
		code: "not_found",
		details: []detail{
			{"request", issueRequest(address, id, sentIssueFields)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id " + id + " not found"},
		},
	}
}

func TestIssueRefusesACallThatNamesNoCommandOfIts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the command alone", argv: []string{"issue"}},
		{name: "a command it does not have", argv: []string{"issue", "bogus"}},
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

func TestIssueShowTakesExactlyOneID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no id", argv: []string{"issue", "show"}},
		{name: "two ids", argv: []string{"issue", "show", "DEV-1", "DEV-2"}},
		{name: "one id twice", argv: []string{"issue", "show", "DEV-1", "DEV-1"}},
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

func TestIssueShowRefusesANumberWithoutACode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "on its own", argv: []string{"issue", "show", "-1"}},
		{name: "after the separator", argv: []string{"issue", "show", "--", "-1"}},
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

func TestIssueShowHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"issue", "show", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, issueShowFields)
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

func TestIssueShowPrintsAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	printed := requireDocument(t, got.stdout)
	require.Len(t, printed, 11)
	assert.Equal(t, []detail{
		{"idReadable", "DEV-1"},
		{"summary", "Задача в работе"},
		{"reporter", []detail{{"login", "admin"}}},
	}, printed[:3])
	assert.Equal(t, []string{"created", "updated"}, []string{printed[3].key, printed[4].key})
	assert.Regexp(t, instantForm, printed[3].value)
	assert.Regexp(t, instantForm, printed[4].value)
	assert.Equal(t, []detail{{"resolved", nil}, {"tags", []any{}}}, printed[5:7])
	assert.Equal(t, []string{"customFields", "links", "description", "comments"},
		[]string{printed[7].key, printed[8].key, printed[9].key, printed[10].key})
	assert.Equal(t, []detail{
		{"Type", "Task"},
		{"Priority", "Medium"},
		{"Категория", "Развитие технологий"},
		{"Клиент", []any{"ACME"}},
		{"Модуль системы", []any{"Инфраструктура. DevOps"}},
		{"State", "In Progress"},
		{"Затраченное время", "PT1H30M"},
	}, printed[7].value)
	assert.Equal(t, []detail{
		{"relates to", []any{[]detail{{"idReadable", "DEV-2"}, {"summary", "Отклонённая задача"}}}},
		{"depends on", []any{[]detail{{"idReadable", "DEV-3"}, {"summary", "Блокирующая задача"}}}},
		{"is duplicated by", []any{[]detail{{"idReadable", "DEV-5"}, {"summary", "Дубль задачи в работе"}}}},
		{"subtask of", []any{[]detail{{"idReadable", "DEV-4"}, {"summary", "Родительская задача"}}}},
		{"Скопирована в", []any{[]detail{{"idReadable", "DEV-6"}, {"summary", "Копия задачи в работе"}}}},
	}, printed[8].value)
	assert.Equal(t, sentDescription(t, dev), printed[9].value)
	root := requireMapping(t, "stdout", got.stdout)
	assert.Regexp(t, commentIDForm, nodeAt(t, root, "comments", "id").Value)
	assert.Equal(t, "admin", nodeAt(t, root, "comments", "author", "login").Value)
	assert.Equal(t, sentComments(t, dev)[0]["text"], nodeAt(t, root, "comments", "text").Value)
	for _, hidden := range []string{"$type", "163-", "INWARD", "OUTWARD", "BOTH", "presentation"} {
		assert.NotContains(t, got.stdout, hidden)
	}
	assert.Equal(t, []string{sentIssueFields}, dev.sentFields())
	assert.Len(t, dev.requests(), 1)
}

func requireDocument(t *testing.T, stdout string) []detail {
	t.Helper()
	pairs := []detail{}
	for pair := range slices.Chunk(requireMapping(t, "stdout", stdout).Content, 2) {
		pairs = append(pairs, detail{key: pair[0].Value, value: requireValue(t, pair[1])})
	}
	return pairs
}

func TestIssueShowRefusesAnIssueTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-99999")

	assert.Equal(t, noSuchIssue(dev.url, "DEV-99999"), requireFault(t, got))
	assert.Len(t, dev.requests(), 1)
}

func TestIssueShowRefusesTheIssueTheLimitedUserCannotSee(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}, "issue", "show", "DEV-1")

	assert.Equal(t, noSuchIssue(dev.url, "DEV-1"), requireFault(t, got))
	assert.Len(t, dev.requests(), 1)
}
