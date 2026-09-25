package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func showingWrittenFields(t *testing.T, block string) (*fake.Server, *render.Node, *diag.Fault) {
	t.Helper()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","customFields":`+block+`}`))
	call, fault := youtrack.ShowIssue("DEV-1", new("customFields"), youtrack.Comments{})
	require.Nil(t, fault)
	node, fault := call(t.Context(), client(t, server))
	return server, node, fault
}

func printedCustomFields(pairs ...render.Pair) *render.Node {
	return render.NewMap(render.Pair{Key: "customFields", Value: render.NewMap(pairs...)})
}

func TestShowIssuePrintsACustomFieldByTheValueKeyOfItsType(t *testing.T) {
	t.Parallel()
	nothing := printedCustomFields([]render.Pair{}...)
	printed := func(value *render.Node) *render.Node { return printedCustomFields(render.FromData("Field", value)) }
	tests := []struct {
		name      string
		valueType string
		multi     bool
		value     string
		printed   *render.Node
	}{
		{name: "an enum", valueType: "enum",
			value:   `{"$type":"EnumBundleElement","name":"First","localizedName":"Localized","presentation":"Presented"}`,
			printed: printed(render.NewString("First"))},
		{name: "enums", valueType: "enum", multi: true,
			value:   `[` + writtenElement("First") + `,` + writtenElement("Second") + `]`,
			printed: printed(texts("First", "Second"))},
		{name: "a state", valueType: "state",
			value:   `{"$type":"StateBundleElement","name":"First","isResolved":false,"localizedName":"Localized"}`,
			printed: printed(render.NewString("First"))},
		{name: "a version", valueType: "version", value: `{"$type":"VersionBundleElement","name":"First","released":true}`,
			printed: printed(render.NewString("First"))},
		{name: "versions", valueType: "version", multi: true, value: `[{"$type":"VersionBundleElement","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "a build", valueType: "build", value: `{"$type":"BuildBundleElement","name":"First"}`,
			printed: printed(render.NewString("First"))},
		{name: "builds", valueType: "build", multi: true, value: `[{"$type":"BuildBundleElement","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "an owned value", valueType: "ownedField",
			value:   `{"$type":"OwnedBundleElement","name":"First","owner":{"$type":"User","login":"second"}}`,
			printed: printed(render.NewString("First"))},
		{name: "owned values", valueType: "ownedField", multi: true, value: `[{"$type":"OwnedBundleElement","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "a user", valueType: "user", value: `{"$type":"User","login":"first","name":"Named","fullName":"Named"}`,
			printed: printed(render.NewString("first"))},
		{name: "users", valueType: "user", multi: true, value: `[{"$type":"User","login":"first","name":"Named"}]`,
			printed: printed(texts("first"))},
		{name: "a group", valueType: "group", value: `{"$type":"UserGroup","name":"First"}`,
			printed: printed(render.NewString("First"))},
		{name: "groups", valueType: "group", multi: true, value: `[{"$type":"UserGroup","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "a period", valueType: "period", value: `{"$type":"PeriodValue","minutes":90,"presentation":"Presented"}`,
			printed: printed(render.NewString("PT1H30M"))},
		{name: "a period the server counts in working days", valueType: "period",
			value:   `{"$type":"PeriodValue","minutes":1635,"id":"P3DT3H15M"}`,
			printed: printed(render.NewString("PT27H15M"))},
		{name: "a period of whole hours", valueType: "period", value: `{"$type":"PeriodValue","minutes":60}`,
			printed: printed(render.NewString("PT1H"))},
		{name: "a period of no time at all", valueType: "period", value: `{"$type":"PeriodValue","minutes":0}`,
			printed: printed(render.NewString("PT0M"))},
		{name: "a date", valueType: "date", value: `1789560000000`,
			printed: printed(render.NewString("2026-09-16"))},
		{name: "a date and time", valueType: "date and time", value: `1788134400000`,
			printed: printed(render.NewString("2026-08-31T00:00:00Z"))},
		{name: "an integer", valueType: "integer", value: `1`,
			printed: printed(render.NewNumber("1"))},
		{name: "a float", valueType: "float", value: `1.5`,
			printed: printed(render.NewNumber("1.5"))},
		{name: "a string", valueType: "string", value: `"First"`,
			printed: printed(render.NewString("First"))},
		{name: "a text", valueType: "text",
			value:   `{"$type":"TextFieldValue","text":"\n  First\nSecond","markdownText":"<div>First</div>"}`,
			printed: printed(render.NewText("\n  First\nSecond"))},
		{name: "a field holding nothing", valueType: "enum", value: `null`, printed: nothing},
		{name: "a field holding no value of several", valueType: "enum", multi: true, value: `[]`, printed: nothing},
		{name: "a text field holding no text", valueType: "text", value: `{"$type":"TextFieldValue","text":null}`,
			printed: nothing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			block := writtenValues(writtenValue{name: "Field", valueType: tc.valueType, multi: tc.multi, value: tc.value})

			_, node, fault := showingWrittenFields(t, string(block))

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestShowIssuePrintsCustomFieldsInTheOrderOfTheProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		held    []writtenValue
		printed *render.Node
	}{
		{
			name: "by their place in the project",
			held: []writtenValue{
				{name: "Second", valueType: "string", value: `"b"`, ordinal: "2", binding: "1-1"},
				{name: "First", valueType: "string", value: `"a"`, ordinal: "1", binding: "1-2"},
				{name: "Third", valueType: "string", value: `"c"`, ordinal: "8", binding: "1-3"},
			},
			printed: printedCustomFields(
				render.FromData("First", render.NewString("a")),
				render.FromData("Second", render.NewString("b")),
				render.FromData("Third", render.NewString("c"))),
		},
		{
			name: "of one place, by the numbers of their bindings",
			held: []writtenValue{
				{name: "Tenth", valueType: "string", value: `"b"`, ordinal: "3", binding: "1-10"},
				{name: "Ninth", valueType: "string", value: `"a"`, ordinal: "3", binding: "1-9"},
			},
			printed: printedCustomFields(
				render.FromData("Ninth", render.NewString("a")),
				render.FromData("Tenth", render.NewString("b"))),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, node, fault := showingWrittenFields(t, string(writtenValues(tc.held...)))

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestShowIssueRefusesCustomFieldsOfAnotherShape(t *testing.T) {
	t.Parallel()
	field := func(name, binding string) string {
		return `[{"$type":"IssueCustomField","name":` + name + `,"value":null,"projectCustomField":` + binding + `}]`
	}
	binding := func(id, valueType string) string {
		return `{"$type":"ProjectCustomField","id":` + id + `,"ordinal":1,"field":{"$type":"CustomField",` +
			`"fieldType":{"$type":"FieldType","valueType":` + valueType + `,"isMultiValue":false}}}`
	}
	held := func(valueType string, multi bool, value string) string {
		return string(writtenValues(writtenValue{name: "Field", valueType: valueType, multi: multi, value: value}))
	}
	tests := []struct {
		name  string
		block string
	}{
		{name: "the block is no array", block: `null`},
		{name: "a field is no object", block: `[null]`},
		{name: "a name is no text", block: field(`5`, binding(`"1-1"`, `"enum"`))},
		{name: "the field of the project is no object", block: field(`"Field"`, `[`+binding(`"1-1"`, `"enum"`)+`]`)},
		{name: "the binding to the project is named by no text", block: field(`"Field"`, binding(`5`, `"enum"`))},
		{name: "the type of the field is no text", block: field(`"Field"`, binding(`"1-1"`, `5`))},
		{name: "a type ytrack does not model", block: held("quantum", false, writtenElement("First"))},
		{name: "one value by the type and a list in the answer", block: held("enum", false, `[`+writtenElement("First")+`]`)},
		{name: "several values by the type and one in the answer", block: held("enum", true, writtenElement("First"))},
		{name: "a value carrying nothing its type names it by", block: held("user", false, `{"$type":"PeriodValue","minutes":90}`)},
		{name: "a whole number that is text", block: held("integer", false, `"42"`)},
		{name: "a number that is text", block: held("float", false, `"1.5"`)},
		{name: "a string that is a number", block: held("string", false, `42`)},
		{name: "a day that is a fraction", block: held("date", false, `1.5`)},
		{name: "minutes that are text", block: held("period", false, `{"$type":"PeriodValue","minutes":"90"}`)},
		{
			name: "no place among the fields of the project",
			block: `[{"$type":"IssueCustomField","name":"Field","value":null,"projectCustomField":{"$type":"ProjectCustomField",` +
				`"id":"1-1","ordinal":null,"field":{"$type":"CustomField","fieldType":{"$type":"FieldType",` +
				`"valueType":"enum","isMultiValue":false}}}}]`,
		},
		{
			name: "two fields of one name",
			block: string(writtenValues(
				writtenValue{name: "Field", valueType: "enum", binding: "1-1"},
				writtenValue{name: "Field", valueType: "state", binding: "1-2"})),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server, _, fault := showingWrittenFields(t, tc.block)

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				requestTo(http.MethodGet, server, writtenIssuePath+"?fields="+writtenCustomFields),
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(`{"$type":"Issue","idReadable":"DEV-1","customFields":` + tc.block + `}`)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestShowIssuePrintsTextForAHumanToReadAsText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		answer     string
		printed    *render.Node
	}{
		{
			name:       "the description",
			expression: "description",
			answer:     `{"$type":"Issue","description":"First\nSecond"}`,
			printed:    render.NewMap(render.Pair{Key: "description", Value: render.NewText("First\nSecond")}),
		},
		{
			name:       "a description that is not there",
			expression: "description",
			answer:     `{"$type":"Issue","description":null}`,
			printed:    render.NewMap(render.Pair{Key: "description", Value: render.NewNull()}),
		},
		{
			name:       "the title",
			expression: "summary",
			answer:     `{"$type":"Issue","summary":"First\nSecond"}`,
			printed:    render.NewMap(render.Pair{Key: "summary", Value: render.NewString("First\nSecond")}),
		},
		{
			name:       "the description of the project, under a nested key",
			expression: "project(description)",
			answer:     `{"$type":"Issue","project":{"$type":"Project","description":"First"}}`,
			printed: render.NewMap(render.Pair{Key: "project", Value: render.NewMap(
				render.Pair{Key: "description", Value: render.NewText("First")})}),
		},
		{
			name:       "the text of a comment, inside a record of a list",
			expression: "pinnedComments(text)",
			answer:     `{"$type":"Issue","pinnedComments":[{"$type":"IssueComment","id":"1-1","text":"First"}]}`,
			printed: render.NewMap(render.Pair{Key: "pinnedComments", Value: render.NewList(render.NewMap(
				render.Pair{Key: "text", Value: render.NewText("First")}))}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))
			call, fault := youtrack.ShowIssue("DEV-1", new(tc.expression), youtrack.Comments{})
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
		})
	}
}
