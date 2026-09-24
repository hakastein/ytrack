package cli_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The selection of six fixtures the scenarios of links run, which reaches every slot the polygon binds.
const polygonLinked = "issue id: DEV-1, DEV-2, DEV-3, DEV-4, DEV-5, DEV-6 sort by: {issue id} asc"

// A record the scenario writes the whole of: the keys of the default with a block of custom fields of its own
// and whatever else it asks for.
func listedRecord(id string, fields []arrivedField, keys ...string) string {
	members := []string{
		`"$type":"Issue"`,
		`"idReadable":` + strconv.Quote(id),
		`"summary":"summary of ` + id + `"`,
		`"created":1789035410875`,
		`"customFields":` + arrivedFields(fields...),
	}
	return "{" + strings.Join(append(members, keys...), ",") + "}"
}

// The two custom fields of the default as the server sends them, which is the block every record carries where
// a scenario is about something else.
func namedFieldsOfTheDefault() []arrivedField {
	return []arrivedField{
		{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
		{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
	}
}

// requireRecordsOnLines is the records of a selection as the lines they were written on. The document of a list
// is its three counts, the key of the records and one line for each of them, so a record that grew into a block
// mapping is a line count that does not add up.
func requireRecordsOnLines(t *testing.T, got outcome, count int) []string {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
	require.Len(t, lines, 3+1+count, "stdout: %q", got.stdout)
	for _, line := range lines[4:] {
		assert.True(t, strings.HasPrefix(line, "  - {"), "a record stands on more than its line: %q", line)
	}
	return lines[4:]
}

// A block inside a record takes the shapes it takes wherever an issue stands, and a selection is no exception:
// only the custom fields of a record's own issue are named, and a link is printed as its phrase against the
// issues it holds, so nothing else stands under it.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The custom fields of a record are ordered by the place the project of that record's own issue gave each of
// them, whatever order the server sent them in; a selection crosses projects, so two records need not agree.
func TestIssueListPrintsTheCustomFieldsOfEachRecordInTheOrderOfItsProject(t *testing.T) {
	t.Parallel()
	development := []arrivedField{
		{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
		{name: "Оценка (Back)", valueType: "period", ordinal: "2", binding: "180-16",
			value: `{"$type":"PeriodValue","minutes":90,"presentation":"1ч 30м"}`},
		{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
	}
	// Two bindings of one place, told apart by the number of the binding and not by the text of it.
	operations := []arrivedField{
		{name: `a: b #c "d"`, valueType: "enum", ordinal: "5", binding: "190-10", value: bundleElement("Medium")},
		{name: "Priority", valueType: "enum", ordinal: "5", binding: "190-9", value: bundleElement("High")},
	}
	body := `[` + listedRecord("DEV-1", development) + `,` + listedRecord("OPS-1", operations) + `]`
	server := searching(t, answer(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", "+customFields")

	requireRecordsOnLines(t, got, 2)
	found := records(t, got)
	require.Len(t, found, 2)
	assert.Equal(t, []string{namedType, "Оценка (Back)", namedState}, keysOf(nodeAt(t, found[0], "customFields")))
	assert.Equal(t, []string{"Priority", `a: b #c "d"`}, keysOf(nodeAt(t, found[1], "customFields")))
	for _, hidden := range []string{"$type", "presentation", "1ч 30м"} {
		assert.NotContains(t, got.stdout, hidden)
	}
	// The block taken whole is every field the record's issue holds, so no name is asked of the server either.
	assert.Empty(t, server.sentQueries()[1]["customFields"])
}

// A record is one line, and a literal block stands on lines of its own, so prose inside a record is written by
// the writer of double-quoted strings, which carries the text without losing a byte of it.
func TestIssueListPrintsTheProseOfARecordAsAStringOnItsLine(t *testing.T) {
	t.Parallel()
	// The description of DEV-5 of the polygon, with a line of three dashes and a line separator added: both are
	// text a literal block would have to lay out and a quoted string carries as it is.
	const prose = "\n Первая строка после пустой\n---\nстрока с\xe2\x80\xa8разделителем"
	tests := []struct {
		name       string
		expression string
		body       string
		path       []string
	}{
		{
			name:       "the description of an issue",
			expression: "+description",
			body: listedRecord("DEV-1", namedFieldsOfTheDefault(), `"description":`+strconv.Quote(prose)) +
				`,` + listedRecord("DEV-2", namedFieldsOfTheDefault(), `"description":null`),
			path: []string{"description"},
		},
		{
			name:       "the text of a custom field",
			expression: "+customFields",
			body: listedRecord("DEV-1", []arrivedField{{name: "Описание", valueType: "text",
				value: `{"$type":"TextFieldValue","text":` + strconv.Quote(prose) + `}`}}) +
				`,` + listedRecord("DEV-2", nil),
			path: []string{"customFields", "Описание"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := searching(t, answer(http.StatusOK, `[`+tc.body+`]`))

			got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", tc.expression)

			requireRecordsOnLines(t, got, 2)
			printed := nodeAt(t, records(t, got)[0], tc.path...)
			assert.Equal(t, prose, printed.Value, "stdout: %q", got.stdout)
			assert.Equal(t, yaml.DoubleQuotedStyle, printed.Style, "stdout: %q", got.stdout)
		})
	}
}

// The links of an issue stand inside its record under the phrase each of them goes by, and a slot holding no
// issue is no link: a record whose issue has none prints the block empty rather than a key for every slot.
func TestIssueListPrintsTheLinksOfARecord(t *testing.T) {
	t.Parallel()
	linked := arrivedLinks(slices.Concat(emptySlots(), []arrivedLink{{
		direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on",
		issues: []string{partnerIssue("DEV-3", "Блокирующая задача")},
	}})...)
	body := `[` + listedRecord("DEV-1", namedFieldsOfTheDefault(), `"links":`+linked) + `,` +
		listedRecord("DEV-2", namedFieldsOfTheDefault(), `"links":`+arrivedLinks(emptySlots()...)) + `]`
	server := searching(t, answer(http.StatusOK, body))

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

// The two custom fields of the default are printed in the order the expression names them and not in the order
// the server answers with, and a record whose issue has no field of that name gets no key for it.
func TestIssueListPrintsTheNamedCustomFieldsOfTheDefault(t *testing.T) {
	t.Parallel()
	// The server answers Type before State, which is the order of the prototypes rather than of the names.
	development := []arrivedField{
		{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
		{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
	}
	documentation := []arrivedField{
		{name: namedState, valueType: "state", ordinal: "3", binding: "190-14", value: bundleElement("To do")},
	}
	body := `[` + listedRecord("DEV-1", development) + `,` + listedRecord("DOCS-1", documentation) + `]`
	server := searching(t, answer(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "list", "--query", "")

	requireRecordsOnLines(t, got, 2)
	found := records(t, got)
	require.Len(t, found, 2)
	assert.Equal(t, []string{namedState, namedType}, keysOf(nodeAt(t, found[0], "customFields")))
	assert.Equal(t, []string{namedState}, keysOf(nodeAt(t, found[1], "customFields")))
	assert.Equal(t, "In Progress", nodeAt(t, found[0], "customFields", namedState).Value)
	assert.Equal(t, "To do", nodeAt(t, found[1], "customFields", namedState).Value)
}

// The server applies customFields= to every block of custom fields the answer holds, the block of a link's
// partner included, so where a second block is asked for the parameter goes out for none of them: the answer
// arrives whole and the two names of the default are picked out where the record is printed.
func TestIssueListPicksOutTheNamesOfTheDefaultWhereALinkAsksForCustomFieldsToo(t *testing.T) {
	t.Parallel()
	whole := append(namedFieldsOfTheDefault(),
		arrivedField{name: "Priority", valueType: "enum", ordinal: "2", binding: "180-16", value: bundleElement("Medium")})
	partner := `{"$type":"Issue","idReadable":"DEV-3","customFields":` + arrivedFields(whole...) + `}`
	linked := arrivedLinks(arrivedLink{
		direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on",
		issues: []string{partner},
	})
	body := `[` + listedRecord("DEV-1", whole, `"links":`+linked) + `]`
	server := searching(t, answer(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", "+links(issues(customFields))")

	requireRecordsOnLines(t, got, 1)
	record := records(t, got)[0]
	assert.Equal(t, []string{namedState, namedType}, keysOf(nodeAt(t, record, "customFields")))
	assert.Equal(t, []string{namedType, "Priority", namedState},
		keysOf(nodeAt(t, record, "links", "depends on", "customFields")))
	assert.Empty(t, server.sentQueries()[1]["customFields"])
}

// The blocks of a record cost no request of their own, however many records there are: everything a line holds
// is asked for in the one request the selection already sends.
func TestIssueListPrintsTheLinksOfTheDevInstanceInOneRequest(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", polygonLinked, "--limit", "10",
		"--fields", "+links(issues(idReadable))")

	assert.Empty(t, got.stderr)
	requireRecordsOnLines(t, got, 6)
	assert.Equal(t, 1, sentTo(dev, issuesPath))
	for _, path := range dev.sentPaths() {
		assert.NotRegexp(t, `^/api/issues/`, path)
	}
	found := records(t, got)
	require.Len(t, found, 6)
	for _, record := range found {
		assert.Contains(t, recordKeys(record), "links")
	}
	assert.Contains(t, partnersOf(t, found[0], "depends on"), "DEV-3")
	assert.Contains(t, partnersOf(t, found[3], "parent for"), "DEV-1")
	for _, hidden := range []string{"163-", "INWARD", "OUTWARD"} {
		assert.NotContains(t, got.stdout, hidden)
	}
}

// partnersOf is the issues at the other end of the links a record holds under one phrase, by the id they were
// printed under.
func partnersOf(t *testing.T, record *yaml.Node, phrase string) []string {
	t.Helper()
	slot := nodeAt(t, record, "links")
	for pair := range slices.Chunk(slot.Content, 2) {
		if pair[0].Value != phrase {
			continue
		}
		var ids []string
		for _, partner := range pair[1].Content {
			ids = append(ids, nodeAt(t, partner, "idReadable").Value)
		}
		return ids
	}
	require.Fail(t, "no link goes by that phrase", "%q", phrase)
	return nil
}

// The block taken whole is every field the issue of the record holds something in, in the order its project put
// them in, and a field it holds nothing in is no key at all.
func TestIssueListPrintsTheCustomFieldsOfTheDevInstanceWhole(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "list", "--query", "issue id: DEV-1, DEV-2 sort by: {issue id} asc",
		"--fields", "+customFields")

	assert.Empty(t, got.stderr)
	requireRecordsOnLines(t, got, 2)
	found := records(t, got)
	require.Len(t, found, 2)
	assert.Equal(t, []string{namedType, "Priority", "Категория", "Клиент", "Модуль системы", namedState,
		"Затраченное время"}, keysOf(nodeAt(t, found[0], "customFields")))
	assert.Equal(t, []string{namedType, "Priority", "Категория", "Клиент", "Модуль системы", namedState},
		keysOf(nodeAt(t, found[1], "customFields")))
	assert.Equal(t, "Отклонена", nodeAt(t, found[1], "customFields", namedState).Value)
	assert.NotContains(t, got.stdout, "Причина отклонения")
	assert.NotContains(t, got.stdout, "$type")
}

// The prose of the polygon is held against the body the server sent rather than against a copy of the fixture,
// and inside a record every one of those descriptions is a double-quoted string on the line of its record.
func TestIssueListPrintsTheProseOfTheDevInstanceAsAStringOnItsLine(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	const query = "issue id: DEV-1, DEV-3, DEV-4, DEV-5 sort by: {issue id} asc"

	got := runWith(t, dev.env(), "issue", "list", "--query", query, "--fields", "idReadable,description")

	assert.Empty(t, got.stderr)
	requireRecordsOnLines(t, got, 4)
	sent := sentRecords(t, dev)
	require.Len(t, sent, 4)
	for i, record := range records(t, got) {
		printed := nodeAt(t, record, "description")
		assert.Equal(t, sent[i]["description"], printed.Value, "stdout: %q", got.stdout)
		assert.Equal(t, yaml.DoubleQuotedStyle, printed.Style, "stdout: %q", got.stdout)
		assert.NotEmpty(t, printed.Value)
	}
}

// sentRecords is the page of the selection as the server sent it: the second answer the proxy kept, the first
// being the markup of the search.
func sentRecords(t *testing.T, u *upstream) []map[string]any {
	t.Helper()
	answers := u.answers()
	require.Len(t, answers, 2)
	var page []map[string]any
	require.NoError(t, json.Unmarshal(answers[1], &page))
	return page
}

// The names of the default are sent as parameters of their own and resolved against nothing: the catalogue of
// the instance is never read, so a default costs no request and a project without the field costs no key.
func TestIssueListPrintsTheNamedCustomFieldsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{name: "the admin", token: func(t *testing.T) string { return devTokens(t).admin }},
		{name: "a member of the project", token: func(t *testing.T) string { return devTokens(t).member }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)
			const query = "issue id: DEV-1, DEV-2, DOCS-1 sort by: {issue id} asc"

			got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + tc.token(t)},
				"issue", "list", "--query", query)

			assert.Empty(t, got.stderr)
			requireRecordsOnLines(t, got, 3)
			found := records(t, got)
			require.Len(t, found, 3)
			assert.Equal(t, []detail{{namedState, "In Progress"}, {namedType, "Task"}},
				fieldsOf(t, found[0]), "stdout: %q", got.stdout)
			assert.Equal(t, []detail{{namedState, "Отклонена"}, {namedType, "Task"}}, fieldsOf(t, found[1]))
			assert.Equal(t, []detail{{namedState, "To do"}}, fieldsOf(t, found[2]))
			for _, record := range found {
				assert.Regexp(t, instantForm, nodeAt(t, record, "created").Value)
			}
			assert.Equal(t, []string{assistPath, issuesPath}, dev.sentPaths())
			assert.Equal(t, []string{namedState, namedType}, dev.sentQueries()[1]["customFields"])
		})
	}
}

// fieldsOf is the custom fields of a record as the keys they were printed under beside their values.
func fieldsOf(t *testing.T, record *yaml.Node) []detail {
	t.Helper()
	block := nodeAt(t, record, "customFields")
	printed := []detail{}
	for pair := range slices.Chunk(block.Content, 2) {
		printed = append(printed, detail{key: pair[0].Value, value: requireValue(t, pair[1])})
	}
	return printed
}

// A name the caller wrote themselves is resolved against the catalogue of the instance, and a name no field
// answers to is the end of the call: the selection is never run, and the names nearest it are what is offered.
func TestIssueListRefusesANameNoCustomFieldOfTheDevInstanceAnswersTo(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	const expression = `idReadable,customFields("Статус разрабтки")`

	got := runWith(t, dev.env(), "issue", "list", "--query", "issue id: DEV-1", "--fields", expression)

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, detail{"request", catalogueRequest(dev.url)}, found.details[0])
	assert.Equal(t, detail{"fields", expression}, found.details[1])
	assert.Contains(t, got.stderr, "Статус разработки")
	assert.Equal(t, []string{assistPath, cataloguePath}, dev.sentPaths())
	assert.Equal(t, 0, sentTo(dev, issuesPath))
}

// selectingNamedFields is the instance a selection that names a custom field reaches: the markup of the
// search, the catalogue of custom fields, then the selection itself.
func selectingNamedFields(t *testing.T, catalogue string, page http.HandlerFunc) *upstream {
	t.Helper()
	return searching(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, cataloguePath) {
			answer(http.StatusOK, catalogue)(w, r)
			return
		}
		page(w, r)
	})
}

// A name of the default reached the expression without the caller writing it, so it is held against nothing
// even where a name of theirs stands beside it: an instance whose catalogue has neither State nor Type
// answers the selection all the same, and the refusal the two would earn is one no caller could fix.
func TestIssueListHoldsAgainstTheCatalogueOnlyTheNamesTheCallerWrote(t *testing.T) {
	t.Parallel()
	arrived := append(namedFieldsOfTheDefault(), arrivedField{name: "Priority", valueType: "enum",
		ordinal: "2", binding: "180-16", value: bundleElement("High")})
	server := selectingNamedFields(t, catalogueOf(cataloguedField{name: "Priority", translate: "Приоритет"}),
		answer(http.StatusOK, `[`+listedRecord("DEV-1", arrived)+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "x", "--fields", "+customFields(Priority)")

	requireRecordsOnLines(t, got, 1)
	assert.Equal(t, []string{assistPath, cataloguePath, issuesPath}, server.sentPaths())
	assert.Equal(t, []string{namedState, namedType, "Priority"}, server.sentQueries()[2]["customFields"])
	assert.Equal(t, []string{namedState, namedType, "Priority"},
		keysOf(nodeAt(t, records(t, got)[0], "customFields")))
}

// A name of the default is held against no catalogue, so the block of a record is the one place it meets a
// name of the instance — and the server filters customFields= the way it matches any name: letter case aside,
// by the name of a field and by the name a project gave it. Matched byte for byte, a field the request itself
// asked for would arrive and go unprinted, and the key it was asked under is the one the instance keeps.
func TestIssueListPrintsAFieldOfTheDefaultTheInstanceSpellsAnotherWay(t *testing.T) {
	t.Parallel()
	typeOfTheIssue := arrivedField{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15",
		value: bundleElement("Task")}
	tests := []struct {
		name    string
		arrived []arrivedField
		printed []string
	}{
		{
			name: "the name of a field in another letter case",
			arrived: []arrivedField{
				{name: "state", valueType: "state", ordinal: "8", binding: "180-14",
					value: bundleElement("In Progress")},
				typeOfTheIssue,
			},
			printed: []string{"state", namedType},
		},
		{
			name: "the name of the default as the name a project gave the field",
			arrived: []arrivedField{
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
			server := searching(t, answer(http.StatusOK, `[`+listedRecord("DEV-1", tc.arrived)+`]`))

			got := runWith(t, server.env(), "issue", "list", "--query", "")

			requireRecordsOnLines(t, got, 1)
			record := records(t, got)[0]
			assert.Equal(t, tc.printed, keysOf(nodeAt(t, record, "customFields")))
			assert.Equal(t, "In Progress", nodeAt(t, record, "customFields", tc.printed[0]).Value)
		})
	}
}

// A custom field the caller names on top of the default goes the whole way: the catalogue of the instance says
// what the field is called, that name is what the request is cut down by, and that name is the key the record
// carries — beside the two of the default, which never reached the catalogue at all.
func TestIssueListPrintsACustomFieldNamedOnTopOfTheDefault(t *testing.T) {
	t.Parallel()
	arrived := append(namedFieldsOfTheDefault(), arrivedField{name: "Priority", translate: "Приоритет",
		valueType: "enum", ordinal: "2", binding: "180-16", value: bundleElement("Critical")})
	server := selectingNamedFields(t, devCatalogue(), answer(http.StatusOK, `[`+listedRecord("DEV-1", arrived)+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "", "--fields", `+customFields("приоритет")`)

	requireRecordsOnLines(t, got, 1)
	assert.Equal(t, []string{assistPath, cataloguePath, issuesPath}, server.sentPaths())
	assert.Equal(t, []string{namedState, namedType, "Priority"}, server.sentQueries()[2]["customFields"])
	record := records(t, got)[0]
	assert.Equal(t, []string{namedState, namedType, "Priority"}, keysOf(nodeAt(t, record, "customFields")))
	assert.Equal(t, "Critical", nodeAt(t, record, "customFields", "Priority").Value)
}

// The same way round on the polygon: the name is written as the project translates it, the catalogue is read
// once between the markup and the selection, and the record carries what the instance calls the field.
func TestIssueListPrintsACustomFieldNamedOnTopOfTheDevInstanceDefault(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	const query = "issue id: DEV-1, DEV-2 sort by: {issue id} asc"

	got := runWith(t, dev.env(), "issue", "list", "--query", query, "--fields", `+customFields("Приоритет")`)

	assert.Empty(t, got.stderr)
	requireRecordsOnLines(t, got, 2)
	assert.Equal(t, []string{assistPath, cataloguePath, issuesPath}, dev.sentPaths())
	assert.Equal(t, []string{namedState, namedType, "Priority"}, dev.sentQueries()[2]["customFields"])
	for _, record := range records(t, got) {
		assert.Equal(t, []string{namedState, namedType, "Priority"}, keysOf(nodeAt(t, record, "customFields")))
		assert.NotEmpty(t, nodeAt(t, record, "customFields", "Priority").Value)
	}
}
