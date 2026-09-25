package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func TestCreateIssueNamesEveryRequiredFieldItLeavesEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fields  []writtenField
		filled  []string
		missing []string
	}{
		{
			name: "every field that holds one value or several",
			fields: []writtenField{
				{id: "1-2", name: "First", valueType: "enum", required: true},
				{id: "1-3", name: "Second", valueType: "enum", multi: true, required: true},
				{id: "1-4", name: "Optional", valueType: "enum"},
				{id: "1-5", name: "Filled by the project", valueType: "enum", required: true, defaults: []string{"Early"}},
			},
			missing: []string{"First", "Second"},
		},
		{
			name: "a field the call names another",
			fields: []writtenField{
				{id: "1-2", name: "First", valueType: "enum", required: true},
				{id: "1-3", name: "Second", valueType: "enum", required: true},
			},
			filled:  []string{"Second=x"},
			missing: []string{"First"},
		},
		{
			name: "a field a value of the project uncovers",
			fields: []writtenField{
				{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"late"}},
				{id: "1-2", name: "Shown", valueType: "enum", required: true, condition: writtenCondition("1-1", false, "Late")},
			},
			missing: []string{"Shown"},
		},
		{
			name: "a field shown for nothing in the field it watches",
			fields: []writtenField{
				{id: "1-1", name: "Watched", valueType: "state"},
				{id: "1-2", name: "Shown", valueType: "enum", required: true, condition: writtenCondition("1-1", true, "Late")},
			},
			missing: []string{"Shown"},
		},
		{
			name: "a field that watches a field holding several values",
			fields: []writtenField{
				{id: "1-1", name: "Watched", valueType: "enum", multi: true},
				{id: "1-2", name: "Shown", valueType: "enum", required: true, condition: writtenCondition("1-1", false, "Late")},
			},
			missing: []string{"Shown"},
		},
		{
			name: "a field that watches a field the project no longer has",
			fields: []writtenField{
				{id: "1-9", name: "Watched", valueType: "state"},
				{id: "1-2", name: "Shown", valueType: "enum", required: true, condition: writtenCondition("1-1", false, "Late")},
			},
			missing: []string{"Shown"},
		},
		{
			name: "a field the call uncovers",
			fields: []writtenField{
				{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
				{id: "1-2", name: "Shown", valueType: "enum", required: true, condition: writtenCondition("1-1", false, "Late")},
			},
			filled:  []string{"Watched=Late"},
			missing: []string{"Shown"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, writtenProject(tc.fields...), "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, tc.filled, nil))

			want := diag.Fault{Code: diag.MissingRequired, Details: []render.Pair{
				writtenMetadataRequest(server), writtenProjectDetail(), {Key: "missing", Value: texts(tc.missing...)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
			assert.Equal(t, []string{writtenProjectPath}, server.Paths())
		})
	}
}

func TestCreateIssueFilesAnIssueWithoutAFieldNobodyAsksFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields []writtenField
		filled []string
		held   []writtenValue
	}{
		{
			name:   "a field that may stand empty",
			fields: []writtenField{{id: "1-2", name: "Shown", valueType: "enum"}},
		},
		{
			name:   "a required field the project fills itself",
			fields: []writtenField{{id: "1-2", name: "Shown", valueType: "enum", required: true, defaults: []string{"First"}}},
		},
		{
			name:   "a required field the call fills",
			fields: []writtenField{{id: "1-2", name: "Shown", valueType: "enum", required: true}},
			filled: []string{"Shown=First"},
			held:   []writtenValue{{name: "Shown", valueType: "enum", value: writtenElement("First")}},
		},
		{
			name: "a required field a condition hides",
			fields: []writtenField{
				{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
				{id: "1-2", name: "Shown", valueType: "enum", required: true, condition: writtenCondition("1-1", false, "Late")},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := writtenIssue(t, map[string]any{"summary": "First", "customFields": writtenValues(tc.held...)})
			server := servingIssueWrite(t, writtenProject(tc.fields...), "[]", fake.JSON(http.StatusOK, answer))

			node, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, tc.filled, new("idReadable")))

			require.Nil(t, fault)
			assert.Equal(t, writtenID(), node)
		})
	}
}

func TestCreateIssueRefusesAValueAConditionHides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		watched writtenField
		shownAt string
		filled  []string
	}{
		{
			name:    "the project fills the field it watches with a value it does not show at",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
			shownAt: writtenCondition("1-1", false, "Late", "Later"),
		},
		{
			name:    "nothing stands in the field it watches",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state"},
			shownAt: writtenCondition("1-1", false, "Late"),
		},
		{
			name:    "it shows for nothing as well, and the field it watches holds another value",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
			shownAt: writtenCondition("1-1", true, "Late"),
		},
		{
			name:    "it names no value and does not show for nothing either",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
			shownAt: writtenCondition("1-1", false),
		},
		{
			name:    "the call writes a value it does not show at over one it does",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Late"}},
			shownAt: writtenCondition("1-1", false, "Late"),
			filled:  []string{"Watched=Early"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := writtenProject(tc.watched, writtenField{id: "1-2", name: "Shown", valueType: "enum", condition: tc.shownAt})
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, append(tc.filled, "Shown=First"), nil))

			kept, invalid := issueWriteFault(t, fault)
			assert.Equal(t, diag.Fault{Code: diag.BadUsage, Details: []render.Pair{
				writtenMetadataRequest(server), writtenProjectDetail(), {Key: "invalid"},
			}}, kept)
			assert.Equal(t, []writtenInvalid{{Field: "Shown", Value: "First"}}, invalid)
			assert.Equal(t, []string{writtenProjectPath}, server.Paths())
		})
	}
}

func TestCreateIssueSendsAValueAConditionShows(t *testing.T) {
	t.Parallel()
	const sentShown = `{"$type":"SingleEnumIssueCustomField","name":"Shown","value":{"name":"First"}}`
	shown := writtenValue{name: "Shown", valueType: "enum", value: writtenElement("First")}
	tests := []struct {
		name    string
		watched writtenField
		shownAt string
		filled  []string
		held    []writtenValue
		sent    string
	}{
		{
			name:    "the call writes a value it shows at into the field it watches, in another letter case",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
			shownAt: writtenCondition("1-1", false, "Late"),
			filled:  []string{"Watched=late"},
			held:    []writtenValue{{name: "Watched", valueType: "state", value: writtenElement("Late")}, shown},
			sent:    `{"$type":"StateIssueCustomField","name":"Watched","value":{"name":"late"}},` + sentShown,
		},
		{
			name:    "the project fills the field it watches with a value it shows at",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Late"}},
			shownAt: writtenCondition("1-1", false, "Late"),
			held:    []writtenValue{shown},
			sent:    sentShown,
		},
		{
			name:    "nothing stands in the field it watches and it shows for nothing",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state"},
			shownAt: writtenCondition("1-1", true, "Late"),
			held:    []writtenValue{shown},
			sent:    sentShown,
		},
		{
			name:    "the field it watches holds several values",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "enum", multi: true, defaults: []string{"Early"}},
			shownAt: writtenCondition("1-1", false, "Late"),
			held:    []writtenValue{shown},
			sent:    sentShown,
		},
		{
			name:    "the project no longer has the field it watches",
			watched: writtenField{id: "1-9", name: "Watched", valueType: "state", defaults: []string{"Early"}},
			shownAt: writtenCondition("1-1", false, "Late"),
			held:    []writtenValue{shown},
			sent:    sentShown,
		},
		{
			name:    "it watches no field",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
			shownAt: `{"$type":"FieldBasedCondition","showForNullValue":false,"field":null,"values":[]}`,
			held:    []writtenValue{shown},
			sent:    sentShown,
		},
		{
			name:    "it is of a kind ytrack does not evaluate",
			watched: writtenField{id: "1-1", name: "Watched", valueType: "state", defaults: []string{"Early"}},
			shownAt: `{"$type":"CustomFieldCondition","id":"2-1"}`,
			held:    []writtenValue{shown},
			sent:    sentShown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := writtenProject(tc.watched, writtenField{id: "1-2", name: "Shown", valueType: "enum", condition: tc.shownAt})
			answer := writtenIssue(t, map[string]any{"summary": "First", "customFields": writtenValues(tc.held...)})
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, append(tc.filled, "Shown=First"),
				new("idReadable")))

			require.Nil(t, fault)
			assert.JSONEq(t, writtenBody(tc.sent), server.Last(t).Body)
		})
	}
}

func TestUpdateIssueRefusesToEmptyAFieldTheProjectRequires(t *testing.T) {
	t.Parallel()
	project := writtenProject(
		writtenField{id: "1-1", name: "First", valueType: "enum", required: true},
		writtenField{id: "1-2", name: "Second", valueType: "enum", multi: true, required: true},
		writtenField{id: "1-3", name: "Optional", valueType: "enum"},
	)
	server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

	_, fault := callOn(t, server)(youtrack.UpdateIssue("DEV-1", nil, nil, nil, []string{"Optional", "second", "First"}, nil))

	want := diag.Fault{Code: diag.MissingRequired, Details: []render.Pair{
		writtenReadRequest(server), writtenProjectDetail(), {Key: "missing", Value: texts("First", "Second")},
	}}
	assert.Equal(t, want, faultOf(t, fault))
	assert.Equal(t, []string{writtenIssuePath}, server.Paths())
}
