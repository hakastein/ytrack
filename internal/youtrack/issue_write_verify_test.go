package youtrack_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func servingAWrittenField(t *testing.T, valueType string, multi bool, held string) *fake.Server {
	t.Helper()
	project := writtenProject(writtenField{id: "1-1", name: "Field", valueType: valueType, multi: multi})
	answer := writtenIssue(t, map[string]any{"summary": "First", "customFields": writtenValues(
		writtenValue{name: "Field", valueType: valueType, multi: multi, value: held})})
	return servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))
}

func TestCreateIssueRefusesAnAnswerThatDisagreesWithACustomField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		filled    []string
		held      string
		expected  *render.Node
		actual    *render.Node
	}{
		{name: "a name the server resolved to another", valueType: "enum", filled: []string{"Field=First"},
			held:     writtenElement("Second"),
			expected: render.NewString("First"), actual: render.NewString("Second")},
		{name: "a string in another letter case", valueType: "string", filled: []string{"Field=Upper"},
			held:     `"upper"`,
			expected: render.NewString("Upper"), actual: render.NewString("upper")},
		{name: "a text in another letter case", valueType: "text", filled: []string{"Field=Upper"},
			held:     `{"$type":"TextFieldValue","text":"upper"}`,
			expected: render.NewString("Upper"), actual: render.NewString("upper")},
		{name: "a set holding a value more", valueType: "enum", multi: true, filled: []string{"Field=First", "Field=Second"},
			held:     `[` + writtenElement("First") + `,` + writtenElement("Second") + `,` + writtenElement("Third") + `]`,
			expected: texts("First", "Second"), actual: texts("First", "Second", "Third")},
		{name: "a set holding a value fewer", valueType: "enum", multi: true, filled: []string{"Field=First", "Field=Second"},
			held:     `[` + writtenElement("First") + `]`,
			expected: texts("First", "Second"), actual: texts("First")},
		{name: "a set held empty", valueType: "enum", multi: true, filled: []string{"Field=First"},
			held:     `[]`,
			expected: texts("First"), actual: render.NewList([]*render.Node{}...)},
		{name: "a value held as nothing", valueType: "enum", filled: []string{"Field=First"},
			held:     `null`,
			expected: render.NewString("First"), actual: render.NewNull()},
		{name: "a day the server keeps as another", valueType: "date", filled: []string{"Field=2026-09-16"},
			held:     `1789646400000`,
			expected: render.NewString("2026-09-16"), actual: render.NewString("2026-09-17")},
		{name: "a moment the server keeps a millisecond off", valueType: "date and time",
			filled: []string{"Field=2026-08-31T03:00:00.123+03:00"}, held: `1788134400124`,
			expected: render.NewString("2026-08-31T03:00:00.123+03:00"), actual: render.NewString("2026-08-31T00:00:00.124Z")},
		{name: "a number the server keeps as another", valueType: "float", filled: []string{"Field=1.5"},
			held:     `1.75`,
			expected: render.NewString("1.5"), actual: render.NewString("1.75")},
		{name: "a period the server rounded to the hour", valueType: "period", filled: []string{"Field=PT1H30M"},
			held:     `{"$type":"PeriodValue","minutes":60}`,
			expected: render.NewString("PT1H30M"), actual: render.NewString("PT1H")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingAWrittenField(t, tc.valueType, tc.multi, tc.held)

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, tc.filled, new("idReadable")))

			want := writtenMismatchFault(
				requestTo(http.MethodPost, server, "/api/issues?fields=idReadable,summary,"+writtenCustomFields),
				mismatch("Field", tc.expected, tc.actual))
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestCreateIssueTakesAnAnswerThatHoldsWhatWasWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		filled    []string
		held      string
	}{
		{name: "a name in another letter case", valueType: "enum", filled: []string{"Field=first"},
			held: writtenElement("First")},
		{name: "a login in another letter case", valueType: "user", filled: []string{"Field=FIRST"},
			held: `{"$type":"User","login":"first"}`},
		{name: "a set in another order and letter case", valueType: "enum", multi: true,
			filled: []string{"Field=First", "Field=second", "Field=first"},
			held:   `[` + writtenElement("Second") + `,` + writtenElement("First") + `]`},
		{name: "a day the server keeps at midnight UTC", valueType: "date", filled: []string{"Field=2026-09-16"},
			held: `1789516800000`},
		{name: "a moment the server keeps in UTC", valueType: "date and time",
			filled: []string{"Field=2026-08-31T03:00:00.123+03:00"}, held: `1788134400123`},
		{name: "a number the server keeps to the digits a float holds", valueType: "float",
			filled: []string{"Field=123456789.123456789"}, held: `123456789.12345679`},
		{name: "a period written in minutes", valueType: "period", filled: []string{"Field=PT90M"},
			held: `{"$type":"PeriodValue","minutes":90}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingAWrittenField(t, tc.valueType, tc.multi, tc.held)

			node, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, tc.filled, new("idReadable")))

			require.Nil(t, fault)
			assert.Equal(t, writtenID(), node)
		})
	}
}

func TestUpdateIssueTakesAnAnswerWhereAnEmptiedFieldHoldsNothing(t *testing.T) {
	t.Parallel()
	project := writtenProject(writtenField{id: "1-1", name: "Field", valueType: "enum"})
	tests := []struct {
		name string
		held json.RawMessage
	}{
		{name: "the field held with no value", held: writtenValues(writtenValue{name: "Field", valueType: "enum"})},
		{name: "the field not held at all", held: writtenValues()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := writtenIssue(t, map[string]any{"customFields": tc.held})
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))

			node, fault := callOn(t, server)(youtrack.UpdateIssue("DEV-1", nil, nil, nil, []string{"Field"}, new("idReadable")))

			require.Nil(t, fault)
			assert.Equal(t, writtenID(), node)
		})
	}
}

func TestUpdateIssueRefusesAnAnswerWhereAnEmptiedFieldHoldsAValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		multi    bool
		held     string
		expected *render.Node
		actual   *render.Node
	}{
		{name: "a field that holds one value", held: writtenElement("First"),
			expected: render.NewNull(), actual: render.NewString("First")},
		{name: "a field that holds several", multi: true, held: `[` + writtenElement("First") + `]`,
			expected: render.NewList([]*render.Node{}...), actual: texts("First")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingAWrittenField(t, "enum", tc.multi, tc.held)

			_, fault := callOn(t, server)(youtrack.UpdateIssue("DEV-1", nil, nil, nil, []string{"Field"}, new("idReadable")))

			want := writtenMismatchFault(
				requestTo(http.MethodPost, server, writtenIssuePath+"?fields=idReadable,"+writtenCustomFields),
				mismatch("Field", tc.expected, tc.actual))
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}
