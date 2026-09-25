package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The catalogue of custom fields of the instance, which is the only thing a name of the caller's is resolved
// against, and the one request that goes out before the issue itself.
const (
	cataloguePath   = "/api/admin/customFieldSettings/customFields"
	catalogueFields = "name,localizedName"
)

// A custom field of the instance as the catalogue sends it: a project that calls it nothing of its own sends
// null in place of the translation.
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

// serveNamedFields is the instance the named mode reaches: the catalogue of custom fields, then the issue.
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

// catalogueRequest is the request the reading of the catalogue names in a refusal.
func catalogueRequest(address string) string {
	return "GET " + address + cataloguePath + "?fields=" + catalogueFields + "&$top=-1"
}

// showNamedFields is the block one answer prints for the names of an expression.
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

// A name is written bare where the grammar of an expression can carry it and in double quotes otherwise, and
// nothing stands under it. Every shape outside that is refused before any request, the catalogue included.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// Each name goes out as a parameter of its own, under the name the instance keeps the field by, in the order
// of the expression; what the fields= carries is the tool's own composition and no name of a project's.
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

// A name that holds a quote or a backslash carries it behind a backslash, and the name the server is sent is
// the name itself.
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

// The order is the order of the names, not the order the server answers in.
func TestIssueShowPrintsNamedCustomFieldsInTheOrderTheyWereNamed(t *testing.T) {
	t.Parallel()
	body := issueWithFields(
		receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
		receivedField{name: "State", valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
	)

	got, block := showNamedFields(t, "customFields(State,Type)", body)

	assert.Equal(t, []detail{{"State", "In Progress"}, {"Type", "Task"}}, block, "stdout: %q", got.stdout)
}

// A field the caller named is printed however empty it is: null where its type holds one value, an empty list
// where it holds more than one. A field the issue does not hold at all gets no key, since null would say the
// issue has the field and holds nothing in it.
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

// The instance matches a name the way the server does: letter case aside, against the name of a field first
// and the translation of it after. Two names of one field are one key, where the first of them stood.
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

// A name no field of the instance answers to is the end of the call: nothing is asked of the issue, and the
// refusal names the catalogue it was held against and the names nearest to what was written.
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
	}, requireRefusal(t, got))
	assert.Equal(t, []string{cataloguePath}, server.sentPaths())
}

// A name is measured against what a project calls a field as well as against the name the instance keeps it
// under, so a typo in the translation is answered the field it is a typo of. A name near none of them at all
// is answered every name of the catalogue, since a caller nowhere near a field has nothing to correct.
func TestIssueShowSuggestsTheNamesNearestAMisspeltCustomField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		nearest []any
	}{
		{name: "a typo of the translation", written: "Состояни", nearest: []any{"State"}},
		{
			// The listing reads by name: the order the instance keeps its catalogue in is nobody's.
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
			}, requireRefusal(t, got))
			assert.Equal(t, []string{cataloguePath}, server.sentPaths())
		})
	}
}

// Where more than one field answers to a name, which of them was meant is nothing ytrack may guess at.
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
	}, requireRefusal(t, got))
	assert.Equal(t, []string{cataloguePath}, server.sentPaths())
}

// A token the catalogue is closed to cannot have a name of its own resolved, and what the server said about
// that passes on as it stands.
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
	}, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

// The block taken whole costs no catalogue: only a name the caller wrote themselves is resolved, so a default
// pays for no request and fails no token.
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

// The parameter cuts down every block of custom fields of an answer, so where the issues at the other end of
// a link carry one of their own it is not sent at all: each block arrives whole and the names are picked out
// before printing. A legal expression stays legal whatever the server's parameter can reach.
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

func TestIssueShowPrintsTheNamedCustomFieldsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		id         string
		expression string
		printed    []detail
	}{
		{
			name: "a field left empty and one holding a value", id: "DEV-2",
			expression: `customFields(State,"Причина отклонения")`,
			printed:    []detail{{"State", "Отклонена"}, {"Причина отклонения", nil}},
		},
		{
			name: "a field the issue does not hold at all", id: "DEV-1",
			expression: `customFields(State,"Причина отклонения")`,
			printed:    []detail{{"State", "In Progress"}},
		},
		{
			name: "fields empty of one value and of many and one bound to another project", id: "DEV-8",
			expression: `customFields("Система","Статус разработки","Due Date")`,
			printed:    []detail{{"Система", []any{}}, {"Статус разработки", nil}},
		},
		{
			name: "the order of the names against the order of the server", id: "DEV-1",
			expression: "customFields(State,Type)",
			printed:    []detail{{"State", "In Progress"}, {"Type", "Task"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, who := range devInstanceReaders() {
				t.Run(who.name, func(t *testing.T) {
					t.Parallel()
					dev := devInstance(t)

					got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + who.token(t)},
						"issue", "show", tc.id, "--comments=0", "--fields", tc.expression)

					require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
					assert.Empty(t, got.stderr)
					assert.Equal(t, []detail{{"customFields", tc.printed}}, requireDocument(t, got.stdout))
					assert.Equal(t, []string{cataloguePath, "/api/issues/" + tc.id}, dev.sentPaths())
				})
			}
		})
	}
}

// A name the instance has no custom field for is refused against the catalogue it was read from, and the
// issue is never asked for.
func TestIssueShowRefusesANameTheDevInstanceHasNoCustomFieldFor(t *testing.T) {
	t.Parallel()
	for _, who := range devInstanceReaders() {
		t.Run(who.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + who.token(t)},
				"issue", "show", "DEV-1", "--comments=0", "--fields", `customFields("Статус разрабтки")`)

			found := requireRefusal(t, got)
			assert.Equal(t, "unknown_name", found.code)
			assert.Equal(t, []detail{
				{"request", catalogueRequest(dev.url)},
				{"fields", `customFields("Статус разрабтки")`},
				{"unknown", []any{[]detail{
					{"field", `customFields("Статус разрабтки")`},
					{"nearest", []any{"Статус разработки"}},
				}}},
			}, found.details)
			assert.Equal(t, []string{cataloguePath}, dev.sentPaths())
		})
	}
}

func devInstanceReaders() []struct {
	name  string
	token func(t *testing.T) string
} {
	return []struct {
		name  string
		token func(t *testing.T) string
	}{
		{name: "the admin", token: func(t *testing.T) string { return devTokens(t).admin }},
		{name: "a member of the project", token: func(t *testing.T) string { return devTokens(t).member }},
	}
}
