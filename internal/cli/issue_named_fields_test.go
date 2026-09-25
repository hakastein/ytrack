package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cataloguePath   = "/api/admin/customFieldSettings/customFields"
	catalogueFields = "name,localizedName"
)

type cataloguedField struct {
	name      string
	translate string
}

func (f cataloguedField) sent() string {
	translated := "null"
	if f.translate != "" {
		translated = strconv.Quote(f.translate)
	}
	return `{"$type":"CustomField","name":` + strconv.Quote(f.name) + `,"localizedName":` + translated + `}`
}

func catalogueOf(fields ...cataloguedField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func devCatalogue() string {
	return catalogueOf(
		cataloguedField{name: "Priority", translate: "Приоритет"},
		cataloguedField{name: "Type", translate: "Тип"},
		cataloguedField{name: "State", translate: "Состояние"},
		cataloguedField{name: "Статус разработки"},
		cataloguedField{name: "Модуль системы"},
	)
}

func serveNamedFields(t *testing.T, catalogue string, issue http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, cataloguePath) {
			respondWith(http.StatusOK, catalogue)(w, r)
			return
		}
		issue(w, r)
	})
}

func catalogueRequest(address string) string {
	return "GET " + address + cataloguePath + "?fields=" + catalogueFields + "&$top=-1"
}

func showNamedFields(t *testing.T, expression, body string) (outcome, []detail) {
	t.Helper()
	server := serveNamedFields(t, devCatalogue(), respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", expression)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{cataloguePath, "/api/issues/DEV-1"}, server.sentPaths())
	printed := requireDocument(t, got.stdout)
	require.Len(t, printed, 1, "stdout: %q", got.stdout)
	require.Equal(t, "customFields", printed[0].key)
	block, isMapping := printed[0].value.([]detail)
	require.True(t, isMapping, "stdout: %q", got.stdout)
	return got, block
}

func TestIssueShowRefusesAnExpressionNoCustomFieldNameFits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "a name of nothing at all", expression: `customFields("")`},
		{name: "a quote left open", expression: `customFields("State`},
		{name: "a backslash before anything else", expression: `customFields("a\b")`},
		{name: "a name asked something of", expression: "customFields(State(name))"},
		{name: "the members of the block", expression: "customFields(name,value(name))"},
		{name: "a name in quotes at the root", expression: `"State"`},
		{name: "a name in quotes under a link", expression: `links(issues("State"))`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--fields", tc.expression)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueShowSendsEveryNamedCustomFieldAsAParameter(t *testing.T) {
	t.Parallel()
	expression := `customFields("Модуль системы",state,"Статус разработки")`
	server := serveNamedFields(t, devCatalogue(), respondWith(http.StatusOK, issueWithFields()))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", expression)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	queries := server.sentQueries()
	require.Len(t, queries, 2)
	assert.Equal(t, []string{catalogueFields}, queries[0]["fields"])
	assert.Equal(t, []string{"-1"}, queries[0]["$top"])
	assert.Equal(t, []string{customFieldsFields}, queries[1]["fields"])
	assert.Equal(t, []string{"Модуль системы", "State", "Статус разработки"}, queries[1]["customFields"])
}

func TestIssueShowSendsANamedCustomFieldWhoseNameContainsAQuote(t *testing.T) {
	t.Parallel()
	quoted := `a: b #c "d"`
	catalogue := catalogueOf(cataloguedField{name: quoted})
	server := serveNamedFields(t, catalogue, respondWith(http.StatusOK, issueWithFields()))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", `customFields("a: b #c \"d\"")`)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	queries := server.sentQueries()
	require.Len(t, queries, 2)
	assert.Equal(t, []string{quoted}, queries[1]["customFields"])
}

func TestIssueShowPrintsNamedCustomFieldsInTheOrderTheyWereNamed(t *testing.T) {
	t.Parallel()
	body := issueWithFields(
		receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
		receivedField{name: "State", valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
	)

	got, block := showNamedFields(t, "customFields(State,Type)", body)

	assert.Equal(t, []detail{{"State", "In Progress"}, {"Type", "Task"}}, block, "stdout: %q", got.stdout)
}

func TestIssueShowPrintsANamedCustomFieldTheIssueLeavesEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		received   []receivedField
		printed    []detail
	}{
		{
			name:       "one value and none of it",
			expression: "customFields(State)",
			received:   []receivedField{{name: "State", valueType: "state", value: "null"}},
			printed:    []detail{{"State", nil}},
		},
		{
			name:       "more than one value and none of them",
			expression: `customFields("Модуль системы")`,
			received:   []receivedField{{name: "Модуль системы", valueType: "enum", isMultiValue: true, value: "[]"}},
			printed:    []detail{{"Модуль системы", []any{}}},
		},
		{
			name:       "a field the issue does not hold",
			expression: "customFields(State,Type)",
			received: []receivedField{{
				name: "State", valueType: "state", value: bundleElement("In Progress"),
			}},
			printed: []detail{{"State", "In Progress"}},
		},
		{
			name:       "not one of the fields named",
			expression: "customFields(State,Type)",
			received:   nil,
			printed:    []detail{},
		},
		{
			name:       "a field nobody named",
			expression: "customFields(State)",
			received: []receivedField{
				{name: "State", valueType: "state", value: bundleElement("In Progress")},
				{name: "Type", valueType: "enum", binding: "180-15", value: bundleElement("Task")},
			},
			printed: []detail{{"State", "In Progress"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, block := showNamedFields(t, tc.expression, issueWithFields(tc.received...))

			assert.Equal(t, tc.printed, block, "stdout: %q", got.stdout)
		})
	}
}

func TestIssueShowResolvesANameAgainstBothNamesAFieldAnswersTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		sent       []string
		printed    []detail
	}{
		{
			name:       "the name in another letter case",
			expression: "customFields(state)",
			sent:       []string{"State"},
			printed:    []detail{{"State", "In Progress"}},
		},
		{
			name:       "the translation of the name",
			expression: `customFields("Состояние")`,
			sent:       []string{"State"},
			printed:    []detail{{"State", "In Progress"}},
		},
		{
			name:       "the translation in another letter case",
			expression: `customFields("состояние")`,
			sent:       []string{"State"},
			printed:    []detail{{"State", "In Progress"}},
		},
		{
			name:       "both names of one field",
			expression: `customFields(State,"Состояние")`,
			sent:       []string{"State"},
			printed:    []detail{{"State", "In Progress"}},
		},
	}
	received := issueWithFields(receivedField{
		name: "State", valueType: "state", value: bundleElement("In Progress"),
	})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNamedFields(t, devCatalogue(), respondWith(http.StatusOK, received))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", tc.expression)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			queries := server.sentQueries()
			require.Len(t, queries, 2)
			assert.Equal(t, tc.sent, queries[1]["customFields"])
			assert.Equal(t, []detail{{"customFields", tc.printed}}, requireDocument(t, got.stdout))
		})
	}
}

func TestIssueShowResolvesANameToTheFieldItNamesRatherThanTheOneItTranslates(t *testing.T) {
	t.Parallel()
	catalogue := catalogueOf(
		cataloguedField{name: "Состояние"},
		cataloguedField{name: "State", translate: "Состояние"},
	)
	received := issueWithFields(
		receivedField{name: "Состояние", valueType: "state", value: bundleElement("Новая")},
		receivedField{name: "State", valueType: "state", ordinal: "8", binding: "180-14",
			value: bundleElement("In Progress")},
	)
	server := serveNamedFields(t, catalogue, respondWith(http.StatusOK, received))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", `customFields("Состояние")`)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	queries := server.sentQueries()
	require.Len(t, queries, 2)
	assert.Equal(t, []string{"Состояние"}, queries[1]["customFields"])
	assert.Equal(t, []detail{{"customFields", []detail{{"Состояние", "Новая"}}}}, requireDocument(t, got.stdout))
}

func TestIssueShowRefusesANameNoCustomFieldOfTheInstanceAnswersTo(t *testing.T) {
	t.Parallel()
	server := serveNamedFields(t, devCatalogue(), func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "the issue was asked for", "%s %s", r.Method, r.URL)
	})

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", `customFields("Статус разрабтки",Stat)`)

	assert.Equal(t, faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", catalogueRequest(server.url)},
			{"fields", `customFields("Статус разрабтки",Stat)`},
			{"unknown", []any{
				[]detail{
					{"field", `customFields("Статус разрабтки")`},
					{"nearest", []any{"Статус разработки"}},
				},
				[]detail{{"field", "customFields(Stat)"}, {"nearest", []any{"State"}}},
			}},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{cataloguePath}, server.sentPaths())
}

func TestIssueShowSuggestsTheNamesNearestAMisspeltCustomField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		nearest []any
	}{
		{name: "a typo of the translation", written: "Состояни", nearest: []any{"State"}},
		{
			name:    "a name near none of them",
			written: "zzzzzzzz",
			nearest: []any{"Priority", "State", "Type", "Модуль системы", "Статус разработки"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNamedFields(t, devCatalogue(), func(_ http.ResponseWriter, r *http.Request) {
				assert.Fail(t, "the issue was asked for", "%s %s", r.Method, r.URL)
			})
			expression := `customFields("` + tc.written + `")`

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", expression)

			assert.Equal(t, faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", catalogueRequest(server.url)},
					{"fields", expression},
					{"unknown", []any{[]detail{{"field", expression}, {"nearest", tc.nearest}}}},
				},
			}, requireFault(t, got))
			assert.Equal(t, []string{cataloguePath}, server.sentPaths())
		})
	}
}

func TestIssueShowRefusesANameMoreThanOneCustomFieldAnswersTo(t *testing.T) {
	t.Parallel()
	catalogue := catalogueOf(
		cataloguedField{name: "Оценка"},
		cataloguedField{name: "оценка"},
	)
	server := serveNamedFields(t, catalogue, func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "the issue was asked for", "%s %s", r.Method, r.URL)
	})

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", `customFields("Оценка")`)

	assert.Equal(t, faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", catalogueRequest(server.url)},
			{"fields", `customFields("Оценка")`},
			{"ambiguous", []any{[]detail{
				{"field", `customFields("Оценка")`},
				{"candidates", []any{"Оценка", "оценка"}},
			}}},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{cataloguePath}, server.sentPaths())
}

func TestIssueShowRefusesTheNamesTheCatalogueIsClosedTo(t *testing.T) {
	t.Parallel()
	body := `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		require.True(t, strings.HasPrefix(r.URL.Path, cataloguePath), "the issue was asked for")
		respondWith(http.StatusForbidden, body)(w, r)
	})

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields(State)")

	assert.Equal(t, faultDocument{
		code: "denied",
		details: []detail{
			{"request", catalogueRequest(server.url)},
			{"upstream_status", 403},
			{"upstream_error", "Forbidden"},
			{"upstream_message", "HTTP 403 Forbidden"},
			authFromEnv(),
		},
	}, requireFault(t, got))
	assert.Len(t, server.requests(), 1)
}

func TestIssueShowReadsNoCatalogueWhereNoCustomFieldWasNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the block whole", expression: "customFields"},
		{name: "the whole of it after names", expression: "customFields(State),customFields"},
		{name: "the default", expression: "+idReadable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueInProgress()
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", tc.expression)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.sentPaths())
			assert.Empty(t, server.sentQueries()[0]["customFields"])
		})
	}
}

func TestIssueShowSendsNoNamesWhereAnotherIssueCarriesCustomFieldsToo(t *testing.T) {
	t.Parallel()
	body := `{"$type":"Issue","customFields":` + receivedFields(
		receivedField{name: "State", valueType: "state", ordinal: "8", binding: "180-14",
			value: bundleElement("In Progress")},
		receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15",
			value: bundleElement("Task")},
	) + `,"links":` + receivedLinks(receivedLink{
		direction: "OUTWARD", sourceToTarget: "relates to", targetToSource: "relates to",
		issues: []string{`{"$type":"Issue","customFields":` + receivedFields(
			receivedField{name: "State", valueType: "state", ordinal: "8", binding: "180-14",
				value: bundleElement("Open")},
			receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15",
				value: bundleElement("Bug")},
		) + `}`},
	}) + `}`
	server := serveNamedFields(t, devCatalogue(), respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", "customFields(State),links(issues(customFields))")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	queries := server.sentQueries()
	require.Len(t, queries, 2)
	assert.Empty(t, queries[1]["customFields"], "the parameter would cut down the links as well")
	assert.Equal(t, []detail{
		{"customFields", []detail{{"State", "In Progress"}}},
		{"links", []detail{{"relates to", []any{[]detail{
			{"customFields", []detail{{"Type", "Bug"}, {"State", "Open"}}},
		}}}}},
	}, requireDocument(t, got.stdout))
}
