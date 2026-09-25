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

func listedRecord(id string, fields []receivedField, keys ...string) string {
	members := []string{
		`"$type":"Issue"`,
		`"idReadable":` + strconv.Quote(id),
		`"summary":"summary of ` + id + `"`,
		`"created":1789035410875`,
		`"customFields":` + receivedFields(fields...),
	}
	return "{" + strings.Join(append(members, keys...), ",") + "}"
}

func namedFieldsOfTheDefault() []receivedField {
	return []receivedField{
		{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
		{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
	}
}

func requireRecordsOnLines(t *testing.T, got outcome, count int) []string {
	t.Helper()
	const countLines, issuesKeyLine = 3, 1
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
	require.Len(t, lines, countLines+issuesKeyLine+count, "stdout: %q", got.stdout)
	recordLines := lines[countLines+issuesKeyLine:]
	for _, line := range recordLines {
		assert.True(t, strings.HasPrefix(line, "  - {"), "a record stands on more than its line: %q", line)
	}
	return recordLines
}

func TestIssueListRefusesABlockOfARecordWrittenInAShapeItDoesNotTake(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "a custom field named under the issues of a link",
			expression: "links(issues(customFields(State)))",
		},
		{
			name:       "a part of a link other than the issues at its other end",
			expression: "+links(direction)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", tc.expression)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueListPrintsTheCustomFieldsOfEachRecordInTheOrderOfItsProject(t *testing.T) {
	t.Parallel()
	development := []receivedField{
		{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
		{name: "Оценка (Back)", valueType: "period", ordinal: "2", binding: "180-16",
			value: `{"$type":"PeriodValue","minutes":90,"presentation":"1ч 30м"}`},
		{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
	}
	operationsTiedOnOrdinal := []receivedField{
		{name: `a: b #c "d"`, valueType: "enum", ordinal: "5", binding: "190-10", value: bundleElement("Medium")},
		{name: "Priority", valueType: "enum", ordinal: "5", binding: "190-9", value: bundleElement("High")},
	}
	body := `[` + listedRecord("DEV-1", development) + `,` + listedRecord("OPS-1", operationsTiedOnOrdinal) + `]`
	server := searching(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", "+customFields")

	requireRecordsOnLines(t, got, 2)
	found := records(t, got)
	require.Len(t, found, 2)
	assert.Equal(t, []string{namedType, "Оценка (Back)", namedState}, keysOf(nodeAt(t, found[0], "customFields")))
	assert.Equal(t, []string{"Priority", `a: b #c "d"`}, keysOf(nodeAt(t, found[1], "customFields")))
	for _, hidden := range []string{"$type", "presentation", "1ч 30м"} {
		assert.NotContains(t, got.stdout, hidden)
	}
	assert.Empty(t, server.sentQueries()[1]["customFields"])
}

func TestIssueListPrintsTheTextOfARecordAsAStringOnItsLine(t *testing.T) {
	t.Parallel()
	const text = "\n Первая строка после пустой\n---\nстрока с\xe2\x80\xa8разделителем"
	tests := []struct {
		name       string
		expression string
		body       string
		path       []string
	}{
		{
			name:       "the description of an issue",
			expression: "+description",
			body: listedRecord("DEV-1", namedFieldsOfTheDefault(), `"description":`+strconv.Quote(text)) +
				`,` + listedRecord("DEV-2", namedFieldsOfTheDefault(), `"description":null`),
			path: []string{"description"},
		},
		{
			name:       "the text of a custom field",
			expression: "+customFields",
			body: listedRecord("DEV-1", []receivedField{{name: "Описание", valueType: "text",
				value: `{"$type":"TextFieldValue","text":` + strconv.Quote(text) + `}`}}) +
				`,` + listedRecord("DEV-2", nil),
			path: []string{"customFields", "Описание"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, respondWith(http.StatusOK, `[`+tc.body+`]`))

			got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", tc.expression)

			requireRecordsOnLines(t, got, 2)
			printed := nodeAt(t, records(t, got)[0], tc.path...)
			assert.Equal(t, text, printed.Value, "stdout: %q", got.stdout)
			assert.Equal(t, yaml.DoubleQuotedStyle, printed.Style, "stdout: %q", got.stdout)
		})
	}
}

func TestIssueListPrintsTheLinksOfARecord(t *testing.T) {
	t.Parallel()
	linked := receivedLinks(slices.Concat(emptyIssueLinks(), []receivedLink{{
		direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on",
		issues: []string{targetIssue("DEV-3", "Блокирующая задача")},
	}})...)
	body := `[` + listedRecord("DEV-1", namedFieldsOfTheDefault(), `"links":`+linked) + `,` +
		listedRecord("DEV-2", namedFieldsOfTheDefault(), `"links":`+receivedLinks(emptyIssueLinks()...)) + `]`
	server := searching(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", "+links(issues(idReadable))")

	requireRecordsOnLines(t, got, 2)
	found := records(t, got)
	require.Len(t, found, 2)
	assert.Equal(t, []string{"depends on"}, keysOf(nodeAt(t, found[0], "links")))
	assert.Equal(t, "DEV-3", nodeAt(t, found[0], "links", "depends on", "idReadable").Value)
	assert.Empty(t, keysOf(nodeAt(t, found[1], "links")))
	assert.Contains(t, got.stdout, "links: {}")
	for _, hidden := range []string{"163-", "INWARD", "OUTWARD", "is required for"} {
		assert.NotContains(t, got.stdout, hidden)
	}
}

func TestIssueListPrintsTheNamedCustomFieldsOfTheDefault(t *testing.T) {
	t.Parallel()
	development := []receivedField{
		{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
		{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
	}
	documentation := []receivedField{
		{name: namedState, valueType: "state", ordinal: "3", binding: "190-14", value: bundleElement("To do")},
	}
	body := `[` + listedRecord("DEV-1", development) + `,` + listedRecord("DOCS-1", documentation) + `]`
	server := searching(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "list", "--query", "")

	requireRecordsOnLines(t, got, 2)
	found := records(t, got)
	require.Len(t, found, 2)
	assert.Equal(t, []string{namedState, namedType}, keysOf(nodeAt(t, found[0], "customFields")))
	assert.Equal(t, []string{namedState}, keysOf(nodeAt(t, found[1], "customFields")))
	assert.Equal(t, "In Progress", nodeAt(t, found[0], "customFields", namedState).Value)
	assert.Equal(t, "To do", nodeAt(t, found[1], "customFields", namedState).Value)
}

func TestIssueListPicksOutTheNamesOfTheDefaultWhereALinkAsksForCustomFieldsToo(t *testing.T) {
	t.Parallel()
	whole := append(namedFieldsOfTheDefault(),
		receivedField{name: "Priority", valueType: "enum", ordinal: "2", binding: "180-16", value: bundleElement("Medium")})
	target := `{"$type":"Issue","idReadable":"DEV-3","customFields":` + receivedFields(whole...) + `}`
	linked := receivedLinks(receivedLink{
		direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on",
		issues: []string{target},
	})
	body := `[` + listedRecord("DEV-1", whole, `"links":`+linked) + `]`
	server := searching(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", "+links(issues(customFields))")

	requireRecordsOnLines(t, got, 1)
	record := records(t, got)[0]
	assert.Equal(t, []string{namedState, namedType}, keysOf(nodeAt(t, record, "customFields")))
	assert.Equal(t, []string{namedType, "Priority", namedState},
		keysOf(nodeAt(t, record, "links", "depends on", "customFields")))
	assert.Empty(t, server.sentQueries()[1]["customFields"])
}

func selectingNamedFields(t *testing.T, catalogue string, page http.HandlerFunc) *upstream {
	t.Helper()
	return searching(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, cataloguePath) {
			respondWith(http.StatusOK, catalogue)(w, r)
			return
		}
		page(w, r)
	})
}

func TestIssueListChecksAgainstTheCatalogueOnlyTheNamesTheCallerWrote(t *testing.T) {
	t.Parallel()
	received := append(namedFieldsOfTheDefault(), receivedField{name: "Priority", valueType: "enum",
		ordinal: "2", binding: "180-16", value: bundleElement("High")})
	catalogueLackingTheDefault := catalogueOf(cataloguedField{name: "Priority", translate: "Приоритет"})
	server := selectingNamedFields(t, catalogueLackingTheDefault,
		respondWith(http.StatusOK, `[`+listedRecord("DEV-1", received)+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "x", "--fields", "+customFields(Priority)")

	requireRecordsOnLines(t, got, 1)
	assert.Equal(t, []string{assistPath, cataloguePath, issuesPath}, server.sentPaths())
	assert.Equal(t, []string{namedState, namedType, "Priority"}, server.sentQueries()[2]["customFields"])
	assert.Equal(t, []string{namedState, namedType, "Priority"},
		keysOf(nodeAt(t, records(t, got)[0], "customFields")))
}

func TestIssueListPrintsAFieldOfTheDefaultTheInstanceSpellsAnotherWay(t *testing.T) {
	t.Parallel()
	typeOfTheIssue := receivedField{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15",
		value: bundleElement("Task")}
	tests := []struct {
		name     string
		received []receivedField
		printed  []string
	}{
		{
			name: "the name of a field in another letter case",
			received: []receivedField{
				{name: "state", valueType: "state", ordinal: "8", binding: "180-14",
					value: bundleElement("In Progress")},
				typeOfTheIssue,
			},
			printed: []string{"state", namedType},
		},
		{
			name: "the name of the default as the name a project gave the field",
			received: []receivedField{
				{name: "Состояние", translate: namedState, valueType: "state", ordinal: "8", binding: "180-14",
					value: bundleElement("In Progress")},
				typeOfTheIssue,
			},
			printed: []string{"Состояние", namedType},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, respondWith(http.StatusOK, `[`+listedRecord("DEV-1", tc.received)+`]`))

			got := runWith(t, server.env(), "issue", "list", "--query", "")

			requireRecordsOnLines(t, got, 1)
			record := records(t, got)[0]
			assert.Equal(t, tc.printed, keysOf(nodeAt(t, record, "customFields")))
			assert.Equal(t, "In Progress", nodeAt(t, record, "customFields", tc.printed[0]).Value)
		})
	}
}

func TestIssueListPrintsACustomFieldNamedOnTopOfTheDefault(t *testing.T) {
	t.Parallel()
	received := append(namedFieldsOfTheDefault(), receivedField{name: "Priority", translate: "Приоритет",
		valueType: "enum", ordinal: "2", binding: "180-16", value: bundleElement("Critical")})
	server := selectingNamedFields(t, devCatalogue(), respondWith(http.StatusOK, `[`+listedRecord("DEV-1", received)+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", `+customFields("приоритет")`)

	requireRecordsOnLines(t, got, 1)
	assert.Equal(t, []string{assistPath, cataloguePath, issuesPath}, server.sentPaths())
	assert.Equal(t, []string{namedState, namedType, "Priority"}, server.sentQueries()[2]["customFields"])
	record := records(t, got)[0]
	assert.Equal(t, []string{namedState, namedType, "Priority"}, keysOf(nodeAt(t, record, "customFields")))
	assert.Equal(t, "Critical", nodeAt(t, record, "customFields", "Priority").Value)
}
