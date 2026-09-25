package youtrack_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	issueReadCataloguePath   = "/api/admin/customFieldSettings/customFields"
	issueReadCatalogueTarget = issueReadCataloguePath + "?fields=name,localizedName&$top=-1"
)

func issueReadCatalogue(names ...string) string {
	return "[" + strings.Join(names, ",") + "]"
}

func issueReadName(name, localizedName string) string {
	return `{"$type":"CustomField","name":` + strconv.Quote(name) + `,"localizedName":` + localizedName + `}`
}

func issueReadCustomField(name, localizedName, valueType string, isMultiValue bool, value string) string {
	return `{"$type":"IssueCustomField","name":` + strconv.Quote(name) + `,"value":` + value +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":"1-1","ordinal":1,"field":{"$type":"CustomField",` +
		`"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(valueType) +
		`,"isMultiValue":` + strconv.FormatBool(isMultiValue) + `},"localizedName":` + localizedName + `}}}`
}

func issueReadEnum(name, value string) string {
	return issueReadCustomField(name, "null", "enum", false, `{"$type":"EnumBundleElement","name":`+strconv.Quote(value)+`}`)
}

func issueReadWithFields(fields ...string) string {
	return `{"$type":"Issue","customFields":[` + strings.Join(fields, ",") + `]}`
}

func issueReadCatalogued(t *testing.T, catalogue string, rest http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, fake.Searching(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == issueReadCataloguePath {
			fake.JSON(http.StatusOK, catalogue)(w, r)
			return
		}
		rest(w, r)
	}))
}

func issueReadBlock(pairs ...render.Pair) *render.Node {
	return render.NewMap(render.Pair{Key: "customFields", Value: render.NewMap(pairs...)})
}

func issueReadNamed(name, value string) render.Pair {
	return render.FromData(name, render.NewString(value))
}

func issueReadUnresolved(server *fake.Server, fields, key string, entries ...*render.Node) diag.Fault {
	return diag.Fault{Code: diag.UnknownName, Details: []render.Pair{
		requestTo(http.MethodGet, server, issueReadCatalogueTarget),
		{Key: "fields", Value: render.NewString(fields)},
		{Key: key, Value: render.NewList(entries...)},
	}}
}

func issueReadNearest(key, field string, names ...string) *render.Node {
	listed := []*render.Node{}
	for _, name := range names {
		listed = append(listed, render.NewString(name))
	}
	return render.NewMap(
		render.Pair{Key: "field", Value: render.NewString(field)},
		render.Pair{Key: key, Value: render.NewList(listed...)},
	)
}

func TestShowIssueResolvesACustomFieldNameAgainstTheCatalogue(t *testing.T) {
	t.Parallel()
	catalogue := issueReadCatalogue(issueReadName("Named", `"Translated"`), issueReadName("Other", "null"))
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the name as the catalogue writes it", expression: "customFields(Named)"},
		{name: "the name in another letter case", expression: "customFields(named)"},
		{name: "the localized name", expression: "customFields(Translated)"},
		{name: "the localized name in another letter case", expression: `customFields("translated")`},
		{name: "both names of one field", expression: "customFields(Named,Translated)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueReadCatalogued(t, catalogue, fake.JSON(http.StatusOK, issueReadWithFields(issueReadEnum("Named", "Value"))))

			node, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, issueReadBlock(issueReadNamed("Named", "Value")), node)
			assert.Equal(t, []string{issueReadCataloguePath, issueReadPath}, server.Paths())
			assert.Equal(t, []string{"Named"}, server.Last(t).URL.Query()["customFields"])
		})
	}
}

func TestShowIssueResolvesANameToTheFieldItNamesBeforeTheOneItTranslates(t *testing.T) {
	t.Parallel()
	catalogue := issueReadCatalogue(issueReadName("Translated", "null"), issueReadName("Named", `"Translated"`))
	issue := issueReadWithFields(issueReadEnum("Translated", "Own"), issueReadEnum("Named", "Other"))
	server := issueReadCatalogued(t, catalogue, fake.JSON(http.StatusOK, issue))

	node, fault := issueReadShown(t, server, "customFields(Translated)", youtrack.Comments{})

	require.Nil(t, fault)
	assert.Equal(t, issueReadBlock(issueReadNamed("Translated", "Own")), node)
	assert.Equal(t, []string{"Translated"}, server.Last(t).URL.Query()["customFields"])
}

func TestShowIssueRefusesACustomFieldNameTheCatalogueDoesNotResolve(t *testing.T) {
	t.Parallel()
	named := issueReadCatalogue(issueReadName("Named", `"Translated"`), issueReadName("Other", "null"))
	tests := []struct {
		name       string
		catalogue  string
		expression string
		key        string
		entries    []*render.Node
	}{
		{
			name:       "a name near the name of a field",
			catalogue:  named,
			expression: "customFields(Nmed)",
			key:        "unknown",
			entries:    []*render.Node{issueReadNearest("nearest", "customFields(Nmed)", "Named")},
		},
		{
			name:       "a name near the localized name of a field",
			catalogue:  named,
			expression: "customFields(Translatd)",
			key:        "unknown",
			entries:    []*render.Node{issueReadNearest("nearest", "customFields(Translatd)", "Named")},
		},
		{
			name:       "a name near no field",
			catalogue:  named,
			expression: "customFields(zzzzzzzz)",
			key:        "unknown",
			entries:    []*render.Node{issueReadNearest("nearest", "customFields(zzzzzzzz)", "Named", "Other")},
		},
		{
			name:       "two names, each in the order written",
			catalogue:  named,
			expression: `customFields("Nmed",Othr)`,
			key:        "unknown",
			entries: []*render.Node{
				issueReadNearest("nearest", `customFields("Nmed")`, "Named"),
				issueReadNearest("nearest", "customFields(Othr)", "Other"),
			},
		},
		{
			name: "a name near more than five fields",
			catalogue: issueReadCatalogue(issueReadName("Name12", "null"), issueReadName("Naem", "null"),
				issueReadName("Nme", "null"), issueReadName("Name2", "null"), issueReadName("Nam", "null"),
				issueReadName("Name1", "null"), issueReadName("Other", "null")),
			expression: "customFields(Name)",
			key:        "unknown",
			entries:    []*render.Node{issueReadNearest("nearest", "customFields(Name)", "Nam", "Name1", "Name2", "Nme", "Naem")},
		},
		{
			name:       "a name of two fields",
			catalogue:  issueReadCatalogue(issueReadName("twin", "null"), issueReadName("Twin", "null")),
			expression: "customFields(Twin)",
			key:        "ambiguous",
			entries:    []*render.Node{issueReadNearest("candidates", "customFields(Twin)", "Twin", "twin")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueReadCatalogued(t, tc.catalogue, fake.JSON(http.StatusOK, issueReadWithFields()))

			_, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			assert.Equal(t, issueReadUnresolved(server, tc.expression, tc.key, tc.entries...), faultOf(t, fault))
			assert.Equal(t, []string{issueReadCataloguePath}, server.Paths())
		})
	}
}

func TestShowIssueReadsTheCatalogueOnlyForANameTheCallerWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the block whole", expression: "customFields"},
		{name: "the block whole after a name", expression: "customFields(Named),customFields"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, issueReadWithFields()))

			_, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, []string{issueReadPath}, server.Paths())
			assert.Nil(t, server.Last(t).URL.Query()["customFields"])
		})
	}
}

func TestShowIssueSendsNoNamesWhereAnotherIssueCarriesCustomFieldsToo(t *testing.T) {
	t.Parallel()
	target := `[{"$type":"Issue","customFields":[` + issueReadEnum("Named", "Target") + `]}]`
	issue := `{"$type":"Issue","customFields":[` + issueReadEnum("Named", "Own") + `,` + issueReadEnum("Other", "Also") +
		`],"links":[` + issueReadLink(target, `"OUTWARD"`, issueReadDirected()) + `]}`
	server := issueReadCatalogued(t, issueReadCatalogue(issueReadName("Named", "null")), fake.JSON(http.StatusOK, issue))

	node, fault := issueReadShown(t, server, "customFields(Named),links(issues(customFields))", youtrack.Comments{})

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "customFields", Value: render.NewMap(issueReadNamed("Named", "Own"))},
		render.Pair{Key: "links", Value: issueReadLinked("source to target", issueReadBlock(issueReadNamed("Named", "Target")))},
	), node)
	assert.Nil(t, server.Last(t).URL.Query()["customFields"])
}

func TestShowIssuePrintsTheCustomFieldsAsNamed(t *testing.T) {
	t.Parallel()
	catalogue := issueReadCatalogue(issueReadName("First", "null"), issueReadName("Second", "null"), issueReadName("Many", "null"))
	tests := []struct {
		name       string
		expression string
		received   []string
		printed    *render.Node
	}{
		{
			name:       "in the order named",
			expression: "customFields(Second,First)",
			received:   []string{issueReadEnum("First", "One"), issueReadEnum("Second", "Two")},
			printed:    issueReadBlock(issueReadNamed("Second", "Two"), issueReadNamed("First", "One")),
		},
		{
			name:       "a field of one value that holds none",
			expression: "customFields(First)",
			received:   []string{issueReadCustomField("First", "null", "enum", false, "null")},
			printed:    issueReadBlock(render.FromData("First", render.NewNull())),
		},
		{
			name:       "a field of many values that holds none",
			expression: "customFields(Many)",
			received:   []string{issueReadCustomField("Many", "null", "enum", true, "[]")},
			printed:    issueReadBlock(render.FromData("Many", render.NewList())),
		},
		{
			name:       "a field the issue does not hold",
			expression: "customFields(First,Second)",
			received:   []string{issueReadEnum("First", "One")},
			printed:    issueReadBlock(issueReadNamed("First", "One")),
		},
		{
			name:       "not one of the fields named",
			expression: "customFields(First,Second)",
			printed:    issueReadBlock([]render.Pair{}...),
		},
		{
			name:       "a field nobody named",
			expression: "customFields(First)",
			received:   []string{issueReadEnum("First", "One"), issueReadEnum("Second", "Two")},
			printed:    issueReadBlock(issueReadNamed("First", "One")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueReadCatalogued(t, catalogue, fake.JSON(http.StatusOK, issueReadWithFields(tc.received...)))

			node, fault := issueReadShown(t, server, tc.expression, youtrack.Comments{})

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestListIssuesChecksAgainstTheCatalogueOnlyTheNamesTheCallerWrote(t *testing.T) {
	t.Parallel()
	server := issueReadCatalogued(t, issueReadCatalogue(issueReadName("Named", "null")), fake.JSON(http.StatusOK, `[]`))

	_, _, fault := searchListing(t, server, "field: value", "+customFields(Named)", 50)

	require.Nil(t, fault)
	assert.Equal(t, []string{fake.AssistPath, issueReadCataloguePath, searchIssuesPath}, server.Paths())
	assert.Equal(t, []string{"State", "Type", "Named"}, server.Last(t).URL.Query()["customFields"])
}

func TestListIssuesRefusesACustomFieldNameBeforeTheSearch(t *testing.T) {
	t.Parallel()
	server := issueReadCatalogued(t, issueReadCatalogue(issueReadName("Named", "null")), fake.JSON(http.StatusOK, `[]`))

	_, _, fault := searchListing(t, server, "field: value", "idReadable,customFields(Bogus)", 50)

	want := issueReadUnresolved(server, "idReadable,customFields(Bogus)", "unknown",
		issueReadNearest("nearest", "customFields(Bogus)", "Named"))
	assert.Equal(t, want, faultOf(t, fault))
	assert.Equal(t, []string{fake.AssistPath, issueReadCataloguePath}, server.Paths())
}

func TestListIssuesSendsNoNamesWhereTheBlockIsAskedWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the block of the issue", expression: "+customFields"},
		{name: "the block of an issue it links to", expression: "+links(issues(customFields))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[]`)))

			_, _, fault := searchListing(t, server, "field: value", tc.expression, 50)

			require.Nil(t, fault)
			assert.Equal(t, []string{fake.AssistPath, searchIssuesPath}, server.Paths())
			assert.Nil(t, server.Last(t).URL.Query()["customFields"])
		})
	}
}

func TestListIssuesPrintsAFieldOfTheDefaultByEitherNameOfIt(t *testing.T) {
	t.Parallel()
	typeOfIssue := issueReadEnum("Type", "Task")
	tests := []struct {
		name     string
		received []string
		printed  []render.Pair
	}{
		{
			name:     "the name in another letter case",
			received: []string{issueReadCustomField("state", "null", "state", false, `{"$type":"StateBundleElement","name":"Open"}`), typeOfIssue},
			printed:  []render.Pair{issueReadNamed("state", "Open"), issueReadNamed("Type", "Task")},
		},
		{
			name:     "a field whose localized name is the name",
			received: []string{issueReadCustomField("Status", `"State"`, "state", false, `{"$type":"StateBundleElement","name":"Open"}`), typeOfIssue},
			printed:  []render.Pair{issueReadNamed("Status", "Open"), issueReadNamed("Type", "Task")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			record := `{"$type":"Issue","idReadable":"DEV-1","summary":"First","created":0,"customFields":[` +
				strings.Join(tc.received, ",") + `]}`
			server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[`+record+`]`)))
			call, fault := youtrack.ListIssues("field: value", nil, youtrack.Page{Limit: 50}, func(*diag.Warning) {})
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(
				render.Pair{Key: "total", Value: render.NewNumber("1")},
				render.Pair{Key: "returned", Value: render.NewNumber("1")},
				render.Pair{Key: "truncated", Value: render.NewBool(false)},
				render.Pair{Key: "issues", Value: render.NewList(render.NewMap(
					render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
					render.Pair{Key: "summary", Value: render.NewString("First")},
					render.Pair{Key: "customFields", Value: render.NewMap(tc.printed...)},
					render.Pair{Key: "created", Value: render.NewString("1970-01-01T00:00:00Z")},
				))},
			), node)
		})
	}
}
