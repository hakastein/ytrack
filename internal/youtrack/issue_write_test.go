package youtrack_test

import (
	"cmp"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	writtenProjectPath    = "/api/admin/projects/DEV"
	writtenIssuePath      = "/api/issues/DEV-1"
	writtenMetadataFields = "id,shortName,customFields(id,canBeEmpty,defaultValues(name)," +
		"condition($type,showForNullValue,field(id),values(name))," +
		"field(name,localizedName,fieldType(valueType,isMultiValue)))"
	writtenReadFields   = "idReadable,customFields($type,name,projectCustomField(id)),project(" + writtenMetadataFields + ")"
	writtenCustomFields = "customFields(name,value(name,login,minutes,text)," +
		"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue))))"
)

type writtenField struct {
	id        string
	name      string
	localized string
	valueType string
	multi     bool
	required  bool
	defaults  []string
	condition string
}

func (f writtenField) metadata() string {
	localized := "null"
	if f.localized != "" {
		localized = strconv.Quote(f.localized)
	}
	return `{"$type":"ProjectCustomField","id":` + strconv.Quote(f.id) +
		`,"canBeEmpty":` + strconv.FormatBool(!f.required) +
		`,"defaultValues":` + writtenNames(f.defaults...) +
		`,"condition":` + cmp.Or(f.condition, "null") +
		`,"field":{"$type":"CustomField","name":` + strconv.Quote(f.name) + `,"localizedName":` + localized +
		`,"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(f.valueType) +
		`,"isMultiValue":` + strconv.FormatBool(f.multi) + `}}}`
}

func writtenNames(names ...string) string {
	elements := make([]string, 0, len(names))
	for _, name := range names {
		elements = append(elements, `{"$type":"EnumBundleElement","name":`+strconv.Quote(name)+`}`)
	}
	return "[" + strings.Join(elements, ",") + "]"
}

func writtenProject(fields ...writtenField) string {
	metadata := make([]string, 0, len(fields))
	for _, field := range fields {
		metadata = append(metadata, field.metadata())
	}
	return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + strings.Join(metadata, ",") + `]}`
}

type writtenValue struct {
	name      string
	valueType string
	multi     bool
	value     string
	ordinal   string
	binding   string
}

func (f writtenValue) held() string {
	return `{"$type":"IssueCustomField","name":` + strconv.Quote(f.name) + `,"value":` + cmp.Or(f.value, "null") +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(cmp.Or(f.binding, "1-1")) +
		`,"ordinal":` + cmp.Or(f.ordinal, "0") +
		`,"field":{"$type":"CustomField","localizedName":null,"fieldType":{"$type":"FieldType","valueType":` +
		strconv.Quote(f.valueType) + `,"isMultiValue":` + strconv.FormatBool(f.multi) + `}}}}`
}

func writtenValues(fields ...writtenValue) json.RawMessage {
	held := make([]string, 0, len(fields))
	for _, field := range fields {
		held = append(held, field.held())
	}
	return json.RawMessage("[" + strings.Join(held, ",") + "]")
}

func writtenIssue(t *testing.T, members map[string]any) string {
	t.Helper()
	issue := map[string]any{"$type": "Issue", "idReadable": "DEV-1"}
	maps.Copy(issue, members)
	answer, err := json.Marshal(issue)
	require.NoError(t, err)
	return string(answer)
}

type writtenClass struct {
	name    string
	class   string
	binding string
}

func writtenClasses(held ...writtenClass) string {
	fields := make([]string, 0, len(held))
	for _, field := range held {
		fields = append(fields, `{"$type":`+strconv.Quote(field.class)+`,"name":`+strconv.Quote(field.name)+
			`,"projectCustomField":{"$type":"ProjectCustomField","id":`+strconv.Quote(field.binding)+`}}`)
	}
	return "[" + strings.Join(fields, ",") + "]"
}

func issueToWrite(project, classes string) string {
	return `{"$type":"Issue","idReadable":"DEV-1","customFields":` + classes + `,"project":` + project + `}`
}

func servingIssueWrite(t *testing.T, project, classes string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+writtenProjectPath, fake.JSON(http.StatusOK, project))
	mux.HandleFunc("GET "+writtenIssuePath, fake.JSON(http.StatusOK, issueToWrite(project, classes)))
	mux.HandleFunc("POST /api/issues", write)
	mux.HandleFunc("POST "+writtenIssuePath, write)
	return fake.Serve(t, mux.ServeHTTP)
}

func writingIssueTo(t *testing.T, server *fake.Server) func(youtrack.Call, *diag.Fault) (*render.Node, *diag.Fault) {
	t.Helper()
	return func(call youtrack.Call, fault *diag.Fault) (*render.Node, *diag.Fault) {
		t.Helper()
		require.Nil(t, fault)
		return call(t.Context(), client(t, server))
	}
}

func writtenID() *render.Node {
	return render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")})
}

func writtenRequest(method string, server *fake.Server, target string) render.Pair {
	return render.Pair{Key: "request", Value: render.NewString(method + " " + server.URL + target)}
}

func writtenMismatch(field string, expected, actual *render.Node) *render.Node {
	return render.NewMap(
		render.Pair{Key: "field", Value: render.NewString(field)},
		render.Pair{Key: "expected", Value: expected},
		render.Pair{Key: "actual", Value: actual})
}

func writtenMismatchFault(request render.Pair, mismatch ...*render.Node) diag.Fault {
	return diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
		request,
		{Key: "issue", Value: render.NewString("DEV-1")},
		{Key: "mismatch", Value: render.NewList(mismatch...)},
	}}
}

type writtenInvalid struct {
	Field string
	Value string
}

// issueWriteRefusal leaves the entries under invalid without their reason: it is prose, and only the field and the
// value name what was refused. A node has no accessors, so the entries are read back from the document they print.
func issueWriteRefusal(t *testing.T, fault *diag.Fault) (diag.Fault, []writtenInvalid) {
	t.Helper()
	kept := refusal(t, fault)
	kept.Details = slices.Clone(kept.Details)
	at := slices.IndexFunc(kept.Details, func(pair render.Pair) bool { return pair.Key == "invalid" })
	require.GreaterOrEqual(t, at, 0, "the refusal names nothing under invalid: %v", kept.Details)
	var printed strings.Builder
	require.NoError(t, render.YAML{}.Render(&printed, render.NewMap(kept.Details[at])))
	var document struct {
		Invalid []struct {
			Field  string `yaml:"field"`
			Value  string `yaml:"value"`
			Reason string `yaml:"reason"`
		} `yaml:"invalid"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(printed.String()), &document))
	entries := make([]writtenInvalid, 0, len(document.Invalid))
	for _, entry := range document.Invalid {
		assert.NotEmpty(t, entry.Reason)
		entries = append(entries, writtenInvalid{Field: entry.Field, Value: entry.Value})
	}
	kept.Details[at].Value = nil
	return kept, entries
}

func TestCreateIssueRefusesACallBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		summary     string
		description *string
		filled      []string
		expression  *string
	}{
		{name: "an empty title", summary: ""},
		{name: "a line feed in the title", summary: "a\nb"},
		{name: "a carriage return in the title", summary: "a\rb"},
		{name: "a NEL in the title", summary: "a\u0085b"},
		{name: "a line separator in the title", summary: "a b"},
		{name: "a paragraph separator in the title", summary: "a b"},
		{name: "a title that is no UTF-8", summary: "a\xffb"},
		{name: "a carriage return in the description", summary: "First", description: new("a\rb")},
		{name: "a description that is no UTF-8", summary: "First", description: new("a\xffb")},
		{name: "an empty description", summary: "First", description: new("")},
		{name: "a field with no equals sign", summary: "First", filled: []string{"Field"}},
		{name: "a field with no name before the equals sign", summary: "First", filled: []string{"=First"}},
		{name: "the title written as a field", summary: "First", filled: []string{"summary=First"}},
		{name: "the description written as a field in another letter case", summary: "First",
			filled: []string{"DESCRIPTION=First"}},
		{name: "the comments of the issue", summary: "First", expression: new("+comments(text)")},
		{name: "a name under the custom fields of a linked issue", summary: "First",
			expression: new("links(issues(customFields(name)))")},
		{name: "a name under a custom field", summary: "First", expression: new("customFields(Field(name))")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, fault := youtrack.CreateIssue("DEV", tc.summary, tc.description, tc.filled, tc.expression)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestUpdateIssueRefusesACallBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		summary     *string
		description *string
		filled      []string
		cleared     []string
		expression  *string
	}{
		{name: "nothing to write"},
		{name: "an empty title", summary: new("")},
		{name: "a line feed in the title", summary: new("a\nb")},
		{name: "a carriage return in the description", description: new("a\rb")},
		{name: "an empty description", description: new("")},
		{name: "a description written and emptied at once", description: new("First"), cleared: []string{"description"}},
		{name: "the title emptied", cleared: []string{"summary"}},
		{name: "the title emptied in another letter case", cleared: []string{"SUMMARY"}},
		{name: "a name of nothing emptied", cleared: []string{""}},
		{name: "a field with no equals sign", filled: []string{"Field"}},
		{name: "the comments of the issue", summary: new("First"), expression: new("+comments(text)")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, fault := youtrack.UpdateIssue("DEV-1", tc.summary, tc.description, tc.filled, tc.cleared, tc.expression)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCreateIssueSendsTheTitleAndTheDescriptionAsWritten(t *testing.T) {
	t.Parallel()
	const title, description = "\t[First] ", "Second  \n---\n~~~\n😀"
	answer := writtenIssue(t, map[string]any{"summary": title, "description": description})
	server := servingIssueWrite(t, writtenProject(), "[]", fake.JSON(http.StatusOK, answer))

	_, fault := writingIssueTo(t, server)(youtrack.CreateIssue("DEV", title, new(description), nil, new("idReadable")))

	require.Nil(t, fault)
	assert.Equal(t, []string{writtenProjectPath, "/api/issues"}, server.Paths())
	assert.JSONEq(t, `{"project":{"id":"0-1"},"summary":"\t[First] ","description":"Second  \n---\n~~~\n😀"}`,
		server.Last(t).Body)
}

func TestUpdateIssueSendsOnlyThePartsItWrites(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		summary     *string
		description *string
		cleared     []string
		answer      map[string]any
		body        string
	}{
		{
			name:    "the title",
			summary: new("First"),
			answer:  map[string]any{"summary": "First"},
			body:    `{"summary":"First"}`,
		},
		{
			name:        "the description",
			description: new("Second"),
			answer:      map[string]any{"description": "Second"},
			body:        `{"description":"Second"}`,
		},
		{
			name:    "the description emptied, named in another letter case",
			cleared: []string{"Description"},
			answer:  map[string]any{"description": nil},
			body:    `{"description":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, writtenProject(), "[]", fake.JSON(http.StatusOK, writtenIssue(t, tc.answer)))

			_, fault := writingIssueTo(t, server)(youtrack.UpdateIssue("DEV-1", tc.summary, tc.description, nil, tc.cleared,
				new("idReadable")))

			require.Nil(t, fault)
			assert.Equal(t, []string{writtenIssuePath, writtenIssuePath}, server.Paths())
			assert.JSONEq(t, tc.body, server.Last(t).Body)
		})
	}
}

func TestIssueWriteAsksForWhatItChecks(t *testing.T) {
	t.Parallel()
	project := writtenProject(writtenField{id: "1-1", name: "Field", valueType: "enum"})
	filled := writtenValues(writtenValue{name: "Field", valueType: "enum", value: `{"$type":"EnumBundleElement","name":"First"}`})
	tests := []struct {
		name   string
		call   func() (youtrack.Call, *diag.Fault)
		answer map[string]any
		fields string
	}{
		{
			name: "a title",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, nil, new("idReadable"))
			},
			answer: map[string]any{"summary": "First"},
			fields: "idReadable,summary",
		},
		{
			name: "a title and a description",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", new("Second"), nil, new("idReadable"))
			},
			answer: map[string]any{"summary": "First", "description": "Second"},
			fields: "idReadable,summary,description",
		},
		{
			name: "a title and a custom field",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", nil, []string{"Field=First"}, new("idReadable"))
			},
			answer: map[string]any{"summary": "First", "customFields": filled},
			fields: "idReadable,summary," + writtenCustomFields,
		},
		{
			name: "an emptied description",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateIssue("DEV-1", nil, nil, nil, []string{"description"}, new("idReadable"))
			},
			answer: map[string]any{"description": nil},
			fields: "idReadable,description",
		},
		{
			name: "an emptied custom field",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateIssue("DEV-1", nil, nil, nil, []string{"Field"}, new("idReadable"))
			},
			answer: map[string]any{"customFields": writtenValues()},
			fields: "idReadable," + writtenCustomFields,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, project, "[]", fake.JSON(http.StatusOK, writtenIssue(t, tc.answer)))

			_, fault := writingIssueTo(t, server)(tc.call())

			require.Nil(t, fault)
			assert.Equal(t, tc.fields, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestIssueWritePrintsWhatTheCallerAskedFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		printed    *render.Node
	}{
		{name: "less than the write checks", expression: "idReadable", printed: writtenID()},
		{
			name:       "a part the write checks",
			expression: "idReadable,description",
			printed: render.NewMap(
				render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
				render.Pair{Key: "description", Value: render.NewText("Second")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := writtenIssue(t, map[string]any{"summary": "First", "description": "Second"})
			server := servingIssueWrite(t, writtenProject(), "[]", fake.JSON(http.StatusOK, answer))

			node, fault := writingIssueTo(t, server)(youtrack.CreateIssue("DEV", "First", new("Second"), nil, new(tc.expression)))

			require.Nil(t, fault)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestIssueWriteRefusesAnAnswerThatDisagreesWithTheText(t *testing.T) {
	t.Parallel()
	const created = "/api/issues?fields=idReadable,summary,description"
	tests := []struct {
		name     string
		call     func() (youtrack.Call, *diag.Fault)
		answer   map[string]any
		target   string
		mismatch []*render.Node
	}{
		{
			name: "a title in another letter case",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "Upper", nil, nil, new("idReadable"))
			},
			answer:   map[string]any{"summary": "upper"},
			target:   "/api/issues?fields=idReadable,summary",
			mismatch: []*render.Node{writtenMismatch("summary", render.NewString("Upper"), render.NewString("upper"))},
		},
		{
			name: "a title with a space the server doubled",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateIssue("DEV-1", new("a b"), nil, nil, nil, new("idReadable"))
			},
			answer:   map[string]any{"summary": "a  b"},
			target:   writtenIssuePath + "?fields=idReadable,summary",
			mismatch: []*render.Node{writtenMismatch("summary", render.NewString("a b"), render.NewString("a  b"))},
		},
		{
			name: "a description in another letter case",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", new("Upper"), nil, new("idReadable"))
			},
			answer:   map[string]any{"summary": "First", "description": "upper"},
			target:   created,
			mismatch: []*render.Node{writtenMismatch("description", render.NewString("Upper"), render.NewString("upper"))},
		},
		{
			name: "a description the server kept none of",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", new("Second"), nil, new("idReadable"))
			},
			answer:   map[string]any{"summary": "First", "description": nil},
			target:   created,
			mismatch: []*render.Node{writtenMismatch("description", render.NewString("Second"), render.NewNull())},
		},
		{
			name: "a title and a description both",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateIssue("DEV", "First", new("Second"), nil, new("idReadable"))
			},
			answer: map[string]any{"summary": "Third", "description": "Fourth"},
			target: created,
			mismatch: []*render.Node{
				writtenMismatch("summary", render.NewString("First"), render.NewString("Third")),
				writtenMismatch("description", render.NewString("Second"), render.NewString("Fourth")),
			},
		},
		{
			name: "an emptied description that came back",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateIssue("DEV-1", nil, nil, nil, []string{"description"}, new("idReadable"))
			},
			answer:   map[string]any{"description": "Kept"},
			target:   writtenIssuePath + "?fields=idReadable,description",
			mismatch: []*render.Node{writtenMismatch("description", render.NewNull(), render.NewString("Kept"))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, writtenProject(), "[]", fake.JSON(http.StatusOK, writtenIssue(t, tc.answer)))

			_, fault := writingIssueTo(t, server)(tc.call())

			want := writtenMismatchFault(writtenRequest(http.MethodPost, server, tc.target), tc.mismatch...)
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestIssueWriteRefusesAnAnswerItCannotPrintAfterTheWrite(t *testing.T) {
	t.Parallel()
	answer := writtenIssue(t, map[string]any{"summary": "First", "customFields": writtenValues(
		writtenValue{name: "Field", valueType: "string", value: "42"})})
	server := servingIssueWrite(t, writtenProject(), "[]", fake.JSON(http.StatusOK, answer))

	_, fault := writingIssueTo(t, server)(youtrack.CreateIssue("DEV", "First", nil, nil, new("idReadable,customFields")))

	want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
		writtenRequest(http.MethodPost, server, "/api/issues?fields=idReadable,"+writtenCustomFields+",summary"),
		{Key: "upstream_status", Value: render.NewNumber("200")},
		{Key: "upstream_body", Value: render.NewString(answer)},
	}}
	assert.Equal(t, want, refusal(t, fault))
}

func TestIssueWritePrintsACustomFieldTheExpressionNamesAndChecksThemAll(t *testing.T) {
	t.Parallel()
	answer := writtenIssue(t, map[string]any{"summary": "First", "customFields": writtenValues(
		writtenValue{name: "Named", valueType: "string", value: `"First"`},
		writtenValue{name: "Unnamed", valueType: "string", value: `"Second"`})})
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+writtenProjectPath, fake.JSON(http.StatusOK, writtenProject()))
	mux.HandleFunc("GET /api/admin/customFieldSettings/customFields", fake.JSON(http.StatusOK,
		`[{"$type":"CustomField","name":"Named","localizedName":"Localized"},{"$type":"CustomField","name":"Unnamed","localizedName":null}]`))
	mux.HandleFunc("POST /api/issues", fake.JSON(http.StatusOK, answer))
	server := fake.Serve(t, mux.ServeHTTP)

	node, fault := writingIssueTo(t, server)(youtrack.CreateIssue("DEV", "First", nil, nil, new(`idReadable,customFields("localized")`)))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
		render.Pair{Key: "customFields", Value: render.NewMap(render.FromData("Named", render.NewString("First")))},
	), node)
	assert.Equal(t, url.Values{"fields": {"idReadable," + writtenCustomFields + ",summary"}}, server.Last(t).URL.Query())
}

func issueWriteBrokenOff(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error"`)
	}
}

func TestIssueWriteIsUncertainOfATruncatedAnswerOnlyWhereItMayHaveWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		code    diag.Code
		details []render.Pair
	}{
		{name: "a refusal of the request", status: http.StatusBadRequest, code: diag.UpstreamFailed},
		{name: "a refusal of the token", status: http.StatusForbidden, code: diag.UpstreamFailed},
		{name: "an issue the server does not find", status: http.StatusNotFound, code: diag.UpstreamFailed},
		{
			name:    "a success",
			status:  http.StatusOK,
			code:    diag.WriteUncertain,
			details: []render.Pair{{Key: "upstream_body", Value: render.NewString(`{"error"`)}},
		},
		{
			name:    "a failure of the server",
			status:  http.StatusBadGateway,
			code:    diag.WriteUncertain,
			details: []render.Pair{{Key: "upstream_body", Value: render.NewString(`{"error"`)}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingIssueWrite(t, writtenProject(), "[]", issueWriteBrokenOff(tc.status))

			_, fault := writingIssueTo(t, server)(youtrack.UpdateIssue("DEV-1", new("First"), nil, nil, nil, new("idReadable")))

			want := diag.Fault{Code: tc.code, Details: append([]render.Pair{
				writtenRequest(http.MethodPost, server, writtenIssuePath+"?fields=idReadable,summary"),
				{Key: "upstream_status", Value: render.NewNumber(json.Number(strconv.Itoa(tc.status)))},
			}, tc.details...)}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestUpdateIssueRefusesAnIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	project := writtenProject(writtenField{id: "1-1", name: "Field", valueType: "enum"})
	tests := []struct {
		name string
		read string
	}{
		{
			name: "a readable id it cannot write by",
			read: `{"$type":"Issue","idReadable":"DEV-1/..","customFields":[],"project":` + project + `}`,
		},
		{
			name: "no project",
			read: `{"$type":"Issue","idReadable":"DEV-1","customFields":[],"project":null}`,
		},
		{
			name: "no custom fields",
			read: `{"$type":"Issue","idReadable":"DEV-1","customFields":null,"project":` + project + `}`,
		},
		{
			name: "the class of a field that is not text",
			read: `{"$type":"Issue","idReadable":"DEV-1","customFields":[{"$type":7,"name":"Field",` +
				`"projectCustomField":{"$type":"ProjectCustomField","id":"1-1"}}],"project":` + project + `}`,
		},
		{
			name: "the name of a field that is not text",
			read: `{"$type":"Issue","idReadable":"DEV-1","customFields":[{"$type":"SingleEnumIssueCustomField","name":7,` +
				`"projectCustomField":{"$type":"ProjectCustomField","id":"1-1"}}],"project":` + project + `}`,
		},
		{
			name: "the binding of a field named by no text",
			read: `{"$type":"Issue","idReadable":"DEV-1","customFields":[{"$type":"SingleEnumIssueCustomField",` +
				`"name":"Field","projectCustomField":{"$type":"ProjectCustomField","id":7}}],"project":` + project + `}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("GET "+writtenIssuePath, fake.JSON(http.StatusOK, tc.read))
			server := fake.Serve(t, mux.ServeHTTP)

			_, fault := writingIssueTo(t, server)(youtrack.UpdateIssue("DEV-1", nil, nil, []string{"Field=First"}, nil, nil))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				writtenRequest(http.MethodGet, server, writtenIssuePath+"?fields="+writtenReadFields),
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(tc.read)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{writtenIssuePath}, server.Paths())
		})
	}
}

func servingIssueDeletion(t *testing.T, read string, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/issues/dev-7", fake.JSON(http.StatusOK, read))
	mux.HandleFunc("DELETE /api/issues/DEV-7", deletion)
	return fake.Serve(t, mux.ServeHTTP)
}

func TestDeleteIssueDeletesByTheIDTheReadAnswers(t *testing.T) {
	t.Parallel()
	server := servingIssueDeletion(t, `{"$type":"Issue","idReadable":"DEV-7"}`, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	node, fault := writingIssueTo(t, server)(youtrack.DeleteIssue("dev-7"))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-7")}), node)
	assert.Equal(t, []string{"GET /api/issues/dev-7?fields=idReadable", "DELETE /api/issues/DEV-7?"},
		sentToDeleteIssue(server))
}

func sentToDeleteIssue(server *fake.Server) []string {
	var sent []string
	for _, request := range server.Requests() {
		sent = append(sent, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery)
	}
	return sent
}

func TestDeleteIssueRefusesAReadableIDItCannotDeleteBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		readable string
	}{
		{name: "two dots", readable: `".."`},
		{name: "a path after the id", readable: `"DEV-7/.."`},
		{name: "the id of an article", readable: `"DEV-A-7"`},
		{name: "a number", readable: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := `{"$type":"Issue","idReadable":` + tc.readable + `}`
			server := servingIssueDeletion(t, read, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

			_, fault := writingIssueTo(t, server)(youtrack.DeleteIssue("dev-7"))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				writtenRequest(http.MethodGet, server, "/api/issues/dev-7?fields=idReadable"),
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(read)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{"/api/issues/dev-7"}, server.Paths())
		})
	}
}

func TestDeleteIssueRefusesADeletionAnsweredWithABody(t *testing.T) {
	t.Parallel()
	server := servingIssueDeletion(t, `{"$type":"Issue","idReadable":"DEV-7"}`, fake.JSON(http.StatusOK, `{"x":1}`))

	_, fault := writingIssueTo(t, server)(youtrack.DeleteIssue("dev-7"))

	want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
		writtenRequest(http.MethodDelete, server, "/api/issues/DEV-7"),
		{Key: "upstream_status", Value: render.NewNumber("200")},
		{Key: "upstream_body", Value: render.NewString(`{"x":1}`)},
	}}
	assert.Equal(t, want, refusal(t, fault))
}
