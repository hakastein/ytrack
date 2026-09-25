package youtrack_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func writtenBody(field string) string {
	return `{"project":{"id":"0-1"},"summary":"First","customFields":[` + field + `]}`
}

func writtenElement(name string) string {
	return `{"$type":"EnumBundleElement","name":"` + name + `"}`
}

func writtenProjectDetail() render.Pair {
	return render.Pair{Key: "project", Value: render.NewString("DEV")}
}

func writtenMetadataRequest(server *fake.Server) render.Pair {
	return requestTo(http.MethodGet, server, writtenProjectPath+"?fields="+writtenMetadataFields)
}

func writtenReadRequest(server *fake.Server) render.Pair {
	return requestTo(http.MethodGet, server, writtenIssuePath+"?fields="+writtenReadFields)
}

func writtenCondition(watched string, forNothing bool, values ...string) string {
	return `{"$type":"FieldBasedCondition","showForNullValue":` + strconv.FormatBool(forNothing) +
		`,"field":{"$type":"ProjectCustomField","id":` + strconv.Quote(watched) + `},"values":` + writtenNames(values...) + `}`
}

func TestIssueWriteSendsAValueUnderTheClassAndTheKeyOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		filled    []string
		sent      string
		held      string
	}{
		{name: "an enum", valueType: "enum", filled: []string{"Field=First"},
			sent: `{"$type":"SingleEnumIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: writtenElement("First")},
		{name: "enums", valueType: "enum", multi: true, filled: []string{"Field=First", "Field=Second"},
			sent: `{"$type":"MultiEnumIssueCustomField","name":"Field","value":[{"name":"First"},{"name":"Second"}]}`,
			held: `[` + writtenElement("First") + `,` + writtenElement("Second") + `]`},
		{name: "a state", valueType: "state", filled: []string{"Field=First"},
			sent: `{"$type":"StateIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: writtenElement("First")},
		{name: "a version", valueType: "version", filled: []string{"Field=First"},
			sent: `{"$type":"SingleVersionIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: writtenElement("First")},
		{name: "versions", valueType: "version", multi: true, filled: []string{"Field=First"},
			sent: `{"$type":"MultiVersionIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[` + writtenElement("First") + `]`},
		{name: "a build", valueType: "build", filled: []string{"Field=First"},
			sent: `{"$type":"SingleBuildIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: writtenElement("First")},
		{name: "builds", valueType: "build", multi: true, filled: []string{"Field=First"},
			sent: `{"$type":"MultiBuildIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[` + writtenElement("First") + `]`},
		{name: "an owned value", valueType: "ownedField", filled: []string{"Field=First"},
			sent: `{"$type":"SingleOwnedIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: writtenElement("First")},
		{name: "owned values", valueType: "ownedField", multi: true, filled: []string{"Field=First"},
			sent: `{"$type":"MultiOwnedIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[` + writtenElement("First") + `]`},
		{name: "a user", valueType: "user", filled: []string{"Field=first"},
			sent: `{"$type":"SingleUserIssueCustomField","name":"Field","value":{"login":"first"}}`,
			held: `{"$type":"User","login":"first"}`},
		{name: "users", valueType: "user", multi: true, filled: []string{"Field=first", "Field=second"},
			sent: `{"$type":"MultiUserIssueCustomField","name":"Field","value":[{"login":"first"},{"login":"second"}]}`,
			held: `[{"$type":"User","login":"first"},{"$type":"User","login":"second"}]`},
		{name: "a user named me", valueType: "user", filled: []string{"Field=me"},
			sent: `{"$type":"SingleUserIssueCustomField","name":"Field","value":{"login":"me"}}`,
			held: `{"$type":"User","login":"me"}`},
		{name: "a group", valueType: "group", filled: []string{"Field=First"},
			sent: `{"$type":"SingleGroupIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: `{"$type":"UserGroup","name":"First"}`},
		{name: "groups", valueType: "group", multi: true, filled: []string{"Field=First"},
			sent: `{"$type":"MultiGroupIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[{"$type":"UserGroup","name":"First"}]`},
		{name: "a name holding an equals sign", valueType: "enum", filled: []string{"Field=First=Second"},
			sent: `{"$type":"SingleEnumIssueCustomField","name":"Field","value":{"name":"First=Second"}}`,
			held: writtenElement("First=Second")},
		{name: "a period of hours and minutes", valueType: "period", filled: []string{"Field=PT1H30M"},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":90}}`,
			held: `{"$type":"PeriodValue","minutes":90}`},
		{name: "a period of no time at all", valueType: "period", filled: []string{"Field=PT0M"},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":0}}`,
			held: `{"$type":"PeriodValue","minutes":0}`},
		{name: "a period of the most minutes the field holds", valueType: "period",
			filled: []string{"Field=PT2147483647M"},
			sent:   `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":2147483647}}`,
			held:   `{"$type":"PeriodValue","minutes":2147483647}`},
		{name: "a period of hours coming to the most minutes the field holds", valueType: "period",
			filled: []string{"Field=PT35791394H7M"},
			sent:   `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":2147483647}}`,
			held:   `{"$type":"PeriodValue","minutes":2147483647}`},
		{name: "a text holding a CRLF", valueType: "text", filled: []string{"Field=First\r\nSecond"},
			sent: `{"$type":"TextIssueCustomField","name":"Field","value":{"text":"First\r\nSecond"}}`,
			held: `{"$type":"TextFieldValue","text":"First\r\nSecond"}`},
		{name: "a day, at noon UTC", valueType: "date", filled: []string{"Field=2026-09-16"},
			sent: `{"$type":"DateIssueCustomField","name":"Field","value":1789560000000}`,
			held: `1789560000000`},
		{name: "a moment in an offset of its own", valueType: "date and time",
			filled: []string{"Field=2026-08-31T03:00:00.123+03:00"},
			sent:   `{"$type":"SimpleIssueCustomField","name":"Field","value":1788134400123}`,
			held:   `1788134400123`},
		{name: "a whole number written with leading zeroes", valueType: "integer", filled: []string{"Field=007"},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":7}`,
			held: `7`},
		{name: "a number written with an exponent", valueType: "float", filled: []string{"Field=1e3"},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":1000}`,
			held: `1000`},
		{name: "a string", valueType: "string", filled: []string{"Field=First"},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":"First"}`,
			held: `"First"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := writtenProject(writtenField{id: "1-1", name: "Field", valueType: tc.valueType, multi: tc.multi})
			held := writtenValues(writtenValue{name: "Field", valueType: tc.valueType, multi: tc.multi, value: tc.held})
			answer := writtenIssue(t, map[string]any{"summary": "First", "customFields": held})
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, tc.filled, new("idReadable")))

			require.Nil(t, fault)
			assert.JSONEq(t, writtenBody(tc.sent), server.Last(t).Body)
		})
	}
}

func TestIssueWriteRefusesAValueItsFieldCannotHold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		given     string
		shown     string
	}{
		{name: "a period of days", valueType: "period", given: "P1D", shown: "P1D"},
		{name: "a period of a fraction of an hour", valueType: "period", given: "PT1.5H", shown: "PT1.5H"},
		{name: "a period of seconds", valueType: "period", given: "PT30S", shown: "PT30S"},
		{name: "a period in lower case", valueType: "period", given: "pt1h", shown: "pt1h"},
		{name: "a period of no part at all", valueType: "period", given: "PT", shown: "PT"},
		{name: "a period of hours with no count before the mark", valueType: "period", given: "PTH", shown: "PTH"},
		{name: "a period of minutes with no count before the mark", valueType: "period", given: "PTM", shown: "PTM"},
		{name: "a period of nothing at all", valueType: "period", given: "", shown: ""},
		{name: "a period a minute past what the field holds", valueType: "period", given: "PT2147483648M",
			shown: "PT2147483648M"},
		{name: "a period of hours and minutes a minute past what the field holds", valueType: "period",
			given: "PT35791394H8M", shown: "PT35791394H8M"},
		{name: "a period of hours past what the field holds", valueType: "period", given: "PT35791395H",
			shown: "PT35791395H"},
		{name: "a period of more minutes than twenty digits count", valueType: "period",
			given: "PT99999999999999999999M", shown: "PT99999999999999999999M"},
		{name: "a period of more hours than twenty digits count", valueType: "period",
			given: "PT99999999999999999999H", shown: "PT99999999999999999999H"},
		{name: "a period of more hours than twenty digits count and minutes", valueType: "period",
			given: "PT99999999999999999999H1M", shown: "PT99999999999999999999H1M"},
		{name: "a period of a minute more than a 64-bit count holds", valueType: "period",
			given: "PT18446744073709551617M", shown: "PT18446744073709551617M"},
		{name: "a period of an hour more than a 64-bit count holds", valueType: "period",
			given: "PT18446744073709551617H", shown: "PT18446744073709551617H"},
		{name: "a date of nothing at all", valueType: "date", given: "", shown: ""},
		{name: "a date written the way a human writes one", valueType: "date", given: "16.09.2026", shown: "16.09.2026"},
		{name: "a date carrying a moment of the day", valueType: "date", given: "2026-09-16T00:00:00Z",
			shown: "2026-09-16T00:00:00Z"},
		{name: "a moment with no offset from UTC", valueType: "date and time", given: "2026-08-31T00:00:00",
			shown: "2026-08-31T00:00:00"},
		{name: "a moment finer than a millisecond", valueType: "date and time", given: "2026-08-31T00:00:00.0001Z",
			shown: "2026-08-31T00:00:00.0001Z"},
		{name: "a whole number that is a fraction", valueType: "integer", given: "2.5", shown: "2.5"},
		{name: "a whole number past what the field holds", valueType: "integer", given: "2147483648", shown: "2147483648"},
		{name: "a whole number below what the field holds", valueType: "integer", given: "-2147483649",
			shown: "-2147483649"},
		{name: "a whole number written in hexadecimal", valueType: "integer", given: "0x10", shown: "0x10"},
		{name: "a number that is not one", valueType: "float", given: "NaN", shown: "NaN"},
		{name: "a number past every number", valueType: "float", given: "Inf", shown: "Inf"},
		{name: "a number past the largest a float holds", valueType: "float", given: "1e999", shown: "1e999"},
		{name: "a number written in hexadecimal", valueType: "float", given: "0x1p-2", shown: "0x1p-2"},
		{name: "a number written with a comma", valueType: "float", given: "1,5", shown: "1,5"},
		{name: "a string that begins with a space", valueType: "string", given: " First", shown: " First"},
		{name: "a string that ends with a tab", valueType: "string", given: "First\t", shown: "First\t"},
		{name: "a string that begins with a file separator", valueType: "string", given: "\x1cFirst", shown: "\x1cFirst"},
		{name: "a string that ends with a group separator", valueType: "string", given: "First\x1d", shown: "First\x1d"},
		{name: "a string that begins with a record separator", valueType: "string", given: "\x1eFirst",
			shown: "\x1eFirst"},
		{name: "a string that ends with a unit separator", valueType: "string", given: "First\x1f", shown: "First\x1f"},
		{name: "a string holding a NEL", valueType: "string", given: "a\u0085b", shown: "a\u0085b"},
		{name: "a string holding a line separator", valueType: "string", given: "a b", shown: "a b"},
		{name: "a string holding a paragraph separator", valueType: "string", given: "a b", shown: "a b"},
		{name: "a string that is no UTF-8", valueType: "string", given: "a\xffb", shown: "a�b"},
		{name: "a string of nothing at all", valueType: "string", given: "", shown: ""},
		{name: "a text that is no UTF-8", valueType: "text", given: "a\xffb", shown: "a�b"},
		{name: "a text of nothing at all", valueType: "text", given: "", shown: ""},
		{name: "a name of nothing at all", valueType: "enum", given: "", shown: ""},
		{name: "a login of nothing at all", valueType: "user", given: "", shown: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := writtenProject(writtenField{id: "1-1", name: "Field", valueType: tc.valueType})
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, []string{"Field=" + tc.given}, nil))

			kept, invalid := issueWriteRefusal(t, fault)
			want := diag.Fault{Code: diag.BadUsage, Details: []render.Pair{
				writtenMetadataRequest(server), writtenProjectDetail(), {Key: "invalid"},
			}}
			assert.Equal(t, want, kept)
			assert.Equal(t, []writtenInvalid{{Field: "Field", Value: tc.shown}}, invalid)
			assert.Equal(t, []string{writtenProjectPath}, server.Paths())
		})
	}
}

func TestIssueWriteNamesEveryValueItCannotSendAtOnce(t *testing.T) {
	t.Parallel()
	project := writtenProject(
		writtenField{id: "1-1", name: "First", valueType: "integer"},
		writtenField{id: "1-2", name: "Second", valueType: "period"},
		writtenField{id: "1-3", name: "Third", valueType: "string"},
	)
	server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

	_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil,
		[]string{"Third= x", "Second=P1D", "First=2.5"}, nil))

	kept, invalid := issueWriteRefusal(t, fault)
	assert.Equal(t, diag.Fault{Code: diag.BadUsage, Details: []render.Pair{
		writtenMetadataRequest(server), writtenProjectDetail(), {Key: "invalid"},
	}}, kept)
	assert.Equal(t, []writtenInvalid{
		{Field: "First", Value: "2.5"},
		{Field: "Second", Value: "P1D"},
		{Field: "Third", Value: " x"},
	}, invalid)
}

func TestIssueWriteRefusesMoreThanAFieldTakesInOneWrite(t *testing.T) {
	t.Parallel()
	project := writtenProject(writtenField{id: "1-1", name: "Single", valueType: "enum"})
	tests := []struct {
		name    string
		call    func() (youtrack.Call, *diag.Fault)
		request func(server *fake.Server) render.Pair
		read    string
		invalid writtenInvalid
	}{
		{
			name: "two values of a field that holds one",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, []string{"Single=First", "single=Second"}, nil)
			},
			request: writtenMetadataRequest,
			read:    writtenProjectPath,
			invalid: writtenInvalid{Field: "Single", Value: "Second"},
		},
		{
			name: "a value into a field it empties",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateIssue("DEV-1", nil, nil, []string{"Single=First"}, []string{"single"}, nil)
			},
			request: writtenReadRequest,
			read:    writtenIssuePath,
			invalid: writtenInvalid{Field: "Single", Value: "First"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

			_, fault := callOn(t, server)(tc.call())

			kept, invalid := issueWriteRefusal(t, fault)
			assert.Equal(t, diag.Fault{Code: diag.BadUsage, Details: []render.Pair{
				tc.request(server), writtenProjectDetail(), {Key: "invalid"},
			}}, kept)
			assert.Equal(t, []writtenInvalid{tc.invalid}, invalid)
			assert.Equal(t, []string{tc.read}, server.Paths())
		})
	}
}

func TestIssueWriteResolvesAFieldByItsNameAndSendsTheNameOfTheProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields []writtenField
		filled string
		sent   string
	}{
		{
			name:   "its name in another letter case",
			fields: []writtenField{{id: "1-1", name: "Field", valueType: "string"}},
			filled: "FIELD=First",
			sent:   "Field",
		},
		{
			name:   "its localized name in another letter case",
			fields: []writtenField{{id: "1-1", name: "Field", localized: "Localized", valueType: "string"}},
			filled: "localized=First",
			sent:   "Field",
		},
		{
			name: "a name one field has and another is localized as",
			fields: []writtenField{
				{id: "1-1", name: "Other", localized: "Shared", valueType: "string"},
				{id: "1-2", name: "Shared", valueType: "string"},
			},
			filled: "shared=First",
			sent:   "Shared",
		},
		{
			name:   "a name that ends with a space",
			fields: []writtenField{{id: "1-1", name: "Field ", valueType: "string"}},
			filled: "Field =First",
			sent:   "Field ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := writtenValues(writtenValue{name: tc.sent, valueType: "string", value: `"First"`})
			answer := writtenIssue(t, map[string]any{"summary": "First", "customFields": held})
			server := servingIssueWrite(t, writtenProject(tc.fields...), "[]", fake.JSON(http.StatusOK, answer))

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, []string{tc.filled}, new("idReadable")))

			require.Nil(t, fault)
			assert.JSONEq(t, writtenBody(`{"$type":"SimpleIssueCustomField","name":"`+tc.sent+`","value":"First"}`),
				server.Last(t).Body)
		})
	}
}

func TestIssueWriteRefusesANameNoSingleFieldAnswersTo(t *testing.T) {
	t.Parallel()
	project := writtenProject(
		writtenField{id: "1-1", name: "First", localized: "Shared", valueType: "enum"},
		writtenField{id: "1-2", name: "Second", localized: "Shared", valueType: "enum"},
		writtenField{id: "1-3", name: "Third", valueType: "enum"},
	)
	unknown := func(field string, nearest ...string) *render.Node {
		return render.NewMap(
			render.Pair{Key: "field", Value: render.NewString(field)},
			render.Pair{Key: "nearest", Value: texts(nearest...)})
	}
	tests := []struct {
		name    string
		call    func() (youtrack.Call, *diag.Fault)
		request func(server *fake.Server) render.Pair
		read    string
		key     string
		named   []*render.Node
	}{
		{
			name: "a name close to one field",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, []string{"Thrid=x"}, nil)
			},
			request: writtenMetadataRequest,
			read:    writtenProjectPath,
			key:     "unknown",
			named:   []*render.Node{unknown("Thrid", "Third")},
		},
		{
			name: "a name close to no field",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, []string{"Nothing=x"}, nil)
			},
			request: writtenMetadataRequest,
			read:    writtenProjectPath,
			key:     "unknown",
			named:   []*render.Node{unknown("Nothing", "First", "Second", "Third")},
		},
		{
			name: "a name with a space before it",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, []string{" Third=x"}, nil)
			},
			request: writtenMetadataRequest,
			read:    writtenProjectPath,
			key:     "unknown",
			named:   []*render.Node{unknown(" Third", "Third")},
		},
		{
			name: "two names of no field, each once",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, []string{"Thrid=x", "Nothing=y", "Thrid=z"}, nil)
			},
			request: writtenMetadataRequest,
			read:    writtenProjectPath,
			key:     "unknown",
			named:   []*render.Node{unknown("Thrid", "Third"), unknown("Nothing", "First", "Second", "Third")},
		},
		{
			name: "a name of no field written and emptied",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateIssue("DEV-1", nil, nil, []string{"Thrid=x"}, []string{"Thrid"}, nil)
			},
			request: writtenReadRequest,
			read:    writtenIssuePath,
			key:     "unknown",
			named:   []*render.Node{unknown("Thrid", "Third")},
		},
		{
			name: "a name of two fields, written twice",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, []string{"shared=x", "shared=y"}, nil)
			},
			request: writtenMetadataRequest,
			read:    writtenProjectPath,
			key:     "ambiguous",
			named: []*render.Node{render.NewMap(
				render.Pair{Key: "field", Value: render.NewString("shared")},
				render.Pair{Key: "candidates", Value: texts("First", "Second")})},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

			_, fault := callOn(t, server)(tc.call())

			want := diag.Fault{Code: diag.UnknownName, Details: []render.Pair{
				tc.request(server), writtenProjectDetail(), {Key: tc.key, Value: render.NewList(tc.named...)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{tc.read}, server.Paths())
		})
	}
}

func TestIssueWriteRefusesMetadataOfAnotherShape(t *testing.T) {
	t.Parallel()
	field := func(members string) string {
		return `{"$type":"ProjectCustomField","id":"1-1",` + members + `,"field":{"$type":"CustomField","name":"Field",` +
			`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}`
	}
	projectOf := func(field string) string {
		return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + field + `]}`
	}
	tests := []struct {
		name    string
		project string
	}{
		{name: "the short name of the project", project: `{"$type":"Project","id":"0-1","shortName":7,"customFields":[]}`},
		{name: "the custom fields of the project", project: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":null}`},
		{name: "whether the field may stand empty",
			project: projectOf(field(`"canBeEmpty":null,"defaultValues":[],"condition":null`))},
		{name: "a value the project fills the field with unasked",
			project: projectOf(field(`"canBeEmpty":true,"defaultValues":[{"$type":"EnumBundleElement","name":7}],"condition":null`))},
		{name: "whether the condition of the field shows it for nothing",
			project: projectOf(field(`"canBeEmpty":true,"defaultValues":[],"condition":{"$type":"FieldBasedCondition",` +
				`"showForNullValue":null,"field":null,"values":[]}`))},
		{name: "the field the condition of the field watches",
			project: projectOf(field(`"canBeEmpty":true,"defaultValues":[],"condition":{"$type":"FieldBasedCondition",` +
				`"showForNullValue":false,"field":{"$type":"ProjectCustomField","id":7},"values":[]}`))},
		{name: "a type ytrack does not model",
			project: writtenProject(writtenField{id: "1-1", name: "Field", valueType: "quantum"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, tc.project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, nil)))

			_, fault := callOn(t, server)(youtrack.CreateIssue("DEV", "First", nil, []string{"Field=First"}, nil))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				writtenMetadataRequest(server),
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(tc.project)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{writtenProjectPath}, server.Paths())
		})
	}
}

func TestUpdateIssueEmptiesAFieldTheWayItsTypeHoldsNothing(t *testing.T) {
	t.Parallel()
	project := writtenProject(
		writtenField{id: "1-1", name: "Single", valueType: "user"},
		writtenField{id: "1-2", name: "Multi", valueType: "enum", multi: true},
	)
	tests := []struct {
		name    string
		cleared string
		held    []writtenValue
		sent    string
	}{
		{
			name:    "a field that holds one value",
			cleared: "Single",
			held:    []writtenValue{{name: "Single", valueType: "user"}},
			sent:    `{"$type":"SingleUserIssueCustomField","name":"Single","value":null}`,
		},
		{
			name:    "a field that holds several",
			cleared: "Multi",
			held:    []writtenValue{{name: "Multi", valueType: "enum", multi: true, value: "[]"}},
			sent:    `{"$type":"MultiEnumIssueCustomField","name":"Multi","value":[]}`,
		},
		{
			name:    "a field the answer does not hold at all",
			cleared: "Single",
			sent:    `{"$type":"SingleUserIssueCustomField","name":"Single","value":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := writtenIssue(t, map[string]any{"customFields": writtenValues(tc.held...)})
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))

			_, fault := callOn(t, server)(youtrack.UpdateIssue("DEV-1", nil, nil, nil, []string{tc.cleared}, new("idReadable")))

			require.Nil(t, fault)
			assert.JSONEq(t, `{"customFields":[`+tc.sent+`]}`, server.Last(t).Body)
		})
	}
}

func TestUpdateIssueWritesAFieldUnderTheClassTheIssueHoldsItIn(t *testing.T) {
	t.Parallel()
	project := writtenProject(
		writtenField{id: "1-1", name: "Held", valueType: "state"},
		writtenField{id: "1-2", name: "Unheld", valueType: "enum"},
	)
	classes := writtenClasses(writtenClass{name: "Held", class: "StateMachineIssueCustomField", binding: "1-1"})
	answer := writtenIssue(t, map[string]any{"customFields": writtenValues(
		writtenValue{name: "Held", valueType: "state", value: writtenElement("First")},
		writtenValue{name: "Unheld", valueType: "enum", value: writtenElement("Second")},
	)})
	server := servingIssueWrite(t, project, classes, fake.JSON(http.StatusOK, answer))

	_, fault := callOn(t, server)(youtrack.UpdateIssue("DEV-1", nil, nil, []string{"Held=First", "Unheld=Second"}, nil,
		new("idReadable")))

	require.Nil(t, fault)
	assert.JSONEq(t, `{"customFields":[`+
		`{"$type":"StateMachineIssueCustomField","name":"Held","value":{"name":"First"}},`+
		`{"$type":"SingleEnumIssueCustomField","name":"Unheld","value":{"name":"Second"}}]}`, server.Last(t).Body)
}
