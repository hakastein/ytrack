package cli_test

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const projectWriteFields = "id,shortName,customFields(id,canBeEmpty,defaultValues(name)," +
	"condition($type,showForNullValue,field(id),values(name))," +
	"field(name,localizedName,fieldType(valueType,isMultiValue)))"

func withoutDefaults() []string {
	return []string{"PeriodProjectCustomField", "SimpleProjectCustomField", "TextProjectCustomField"}
}

type writableField struct {
	id           string
	kind         string
	name         string
	translate    string
	valueType    string
	isMultiValue bool
	canBeEmpty   bool
	defaults     []string
	condition    string
}

func (f writableField) sent() string {
	kind := cmp.Or(f.kind, "EnumProjectCustomField")
	translated := "null"
	if f.translate != "" {
		translated = strconv.Quote(f.translate)
	}
	parts := []string{
		`"$type":` + strconv.Quote(kind),
		`"id":` + strconv.Quote(cmp.Or(f.id, "180-1")),
		`"canBeEmpty":` + strconv.FormatBool(f.canBeEmpty),
		`"condition":` + cmp.Or(f.condition, "null"),
		`"field":{"$type":"CustomField","name":` + strconv.Quote(f.name) + `,"localizedName":` + translated + `,` +
			`"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(f.valueType) +
			`,"isMultiValue":` + strconv.FormatBool(f.isMultiValue) + `}}`,
	}
	if !slices.Contains(withoutDefaults(), kind) {
		parts = append(parts, `"defaultValues":`+bundleNames(f.defaults))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func bundleNames(names []string) string {
	sent := make([]string, 0, len(names))
	for _, name := range names {
		sent = append(sent, `{"$type":"EnumBundleElement","name":`+strconv.Quote(name)+`}`)
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func onlyWhen(controls string, showForNullValue bool, values ...string) string {
	return `{"$type":"FieldBasedCondition","showForNullValue":` + strconv.FormatBool(showForNullValue) +
		`,"field":{"$type":"StateProjectCustomField","id":` + strconv.Quote(controls) + `}` +
		`,"values":` + bundleNames(values) + `}`
}

func projectResponse(fields ...writableField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + strings.Join(sent, ",") + `]}`
}

func projectRequiringNothing() string {
	return projectResponse(
		writableField{id: "180-14", kind: "StateProjectCustomField", name: "State", valueType: "state",
			canBeEmpty: true, defaults: []string{"Новая"}},
		writableField{id: "187-2", kind: "PeriodProjectCustomField", name: "Оценка", valueType: "period",
			canBeEmpty: true},
	)
}

func createdIssue(readable, summary, descriptionJSON string) string {
	return createdIssueWith(readable, summary, descriptionJSON, "[]")
}

func createdIssueWith(readable, summary, descriptionJSON, fields string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) +
		`,"summary":` + asJSON(summary) +
		`,"reporter":{"$type":"User","login":"admin"},"created":1789035410875,"updated":1789035410875,` +
		`"resolved":null,"tags":[],"customFields":` + fields + `,"links":[],"description":` + descriptionJSON + `}`
}

func asJSON(text string) string {
	written, _ := json.Marshal(text)
	return string(written)
}

func creating(t *testing.T, metadata, creation http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creation(w, r)
			return
		}
		metadata(w, r)
	})
}

func noCreation(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a creation reached the server", "%s %s", r.Method, r.URL)
	}
}

func writeMetadataRequest(address, code string) string {
	return "GET " + address + "/api/admin/projects/" + code + "?fields=" + projectWriteFields
}

func creationRequest(address, fields string) string {
	return "POST " + address + "/api/issues?fields=" + fields
}

func TestIssueCreateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no project", argv: []string{"issue", "create"}},
		{name: "no title", argv: []string{"issue", "create", "DEV"}},
		{name: "the title as an argument", argv: []string{"issue", "create", "DEV", "a", "b"}},
		{name: "two dots for a project", argv: []string{"issue", "create", "..", "--summary", "x"}},
		{name: "an empty title", argv: []string{"issue", "create", "DEV", "--summary", ""}},
		{name: "a title twice", argv: []string{"issue", "create", "DEV", "--summary", "a", "--summary", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueCreateRefusesTextTheServerWouldRewrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a line feed in the title", argv: []string{"--summary", "a\nb"}},
		{name: "a carriage return in the title", argv: []string{"--summary", "a\rb"}},
		{name: "a NEL in the title", argv: []string{"--summary", "a\xc2\x85b"}},
		{name: "a line separator in the title", argv: []string{"--summary", "a\xe2\x80\xa8b"}},
		{name: "a paragraph separator in the title", argv: []string{"--summary", "a\xe2\x80\xa9b"}},
		{name: "a title that is no UTF-8", argv: []string{"--summary", "a\xffb"}},
		{name: "a carriage return in the prose", argv: []string{"--summary", "x", "--description", "a\rb"}},
		{name: "prose that is no UTF-8", argv: []string{"--summary", "x", "--description", "a\xffb"}},
		{name: "an empty prose", argv: []string{"--summary", "x", "--description", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"issue", "create", "DEV"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueCreateHasNoFlagsBesidesItsOwn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		flag string
	}{
		{name: "prose out of a file", flag: "--description-file"},
		{name: "a title out of a file", flag: "--summary-file"},
		{name: "clearing a field", flag: "--clear"},
		{name: "comments", flag: "--comments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", tc.flag, "y")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestIssueCreatePrintsNoComments(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--fields", "+comments(text)")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestIssueCreateHelpNamesTheDefaultAndNoFile(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"issue", "create", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "ytrack issue create")
	assert.Contains(t, got.stdout, issueShowFields)
	assert.NotContains(t, got.stdout, "-file")
}

func TestIssueCreateReadsTheProjectAndFilesTheIssue(t *testing.T) {
	t.Parallel()
	const title = "[bug] fix login"
	text := longDescription()
	server := creating(t, respondWith(http.StatusOK, projectRequiringNothing()),
		respondWith(http.StatusOK, createdIssue("DEV-7", title, asJSON(text))))

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", title, "--description", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/admin/projects/DEV", "/api/issues"}, server.sentPaths())
	assert.Equal(t, []string{projectWriteFields, askedIssueFields}, server.sentFields())

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(server.asks()[1]), &body))
	assert.Equal(t, map[string]any{
		"project":     map[string]any{"id": "0-1"},
		"summary":     title,
		"description": text,
	}, body)

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"idReadable", "summary", "reporter", "created", "updated", "resolved", "tags",
		"customFields", "links", "description"}, keysOf(mapping))
	assert.Equal(t, "DEV-7", nodeAt(t, mapping, "idReadable").Value)
	assert.Equal(t, text, nodeAt(t, mapping, "description").Value)
}

func longDescription() string {
	var text strings.Builder
	for text.Len() < 81_000 {
		text.WriteString("Шаги:  \n1. открыть   \n---\nи ещё 😀\n")
	}
	return strings.TrimSuffix(text.String(), "\n")
}

func TestIssueCreateWritesTextThatStartsWithADash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want map[string]any
	}{
		{
			name: "prose of one dash",
			argv: []string{"--summary", "x", "--description", "-"},
			want: map[string]any{"project": map[string]any{"id": "0-1"}, "summary": "x", "description": "-"},
		},
		{
			name: "a title that starts with a dash",
			argv: []string{"--summary", "-x"},
			want: map[string]any{"project": map[string]any{"id": "0-1"}, "summary": "-x"},
		},
		{
			name: "a title the shell would take for the help",
			argv: []string{"--summary", "--help"},
			want: map[string]any{"project": map[string]any{"id": "0-1"}, "summary": "--help"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary, _ := tc.want["summary"].(string)
			description := "null"
			if written, given := tc.want["description"].(string); given {
				description = asJSON(written)
			}
			server := creating(t, respondWith(http.StatusOK, projectRequiringNothing()),
				respondWith(http.StatusOK, createdIssue("DEV-7", summary, description)))

			got := runWith(t, server.env(), append([]string{"issue", "create", "DEV"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(server.asks()[1]), &body))
			assert.Equal(t, tc.want, body)
		})
	}
}

func TestIssueCreateNamesEveryRequiredFieldAtOnce(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(
		writableField{id: "180-15", name: "Type", valueType: "enum"},
		writableField{id: "180-16", name: "Priority", valueType: "enum", defaults: []string{"Low"}},
		writableField{id: "180-18", name: "Клиент", valueType: "enum", isMultiValue: true},
		writableField{id: "180-20", name: "Система", valueType: "enum", isMultiValue: true, canBeEmpty: true},
	)
	server := creating(t, respondWith(http.StatusOK, metadata), noCreation(t))

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x")

	want := faultDocument{
		code: "missing_required",
		details: []detail{
			{"request", writeMetadataRequest(server.url, "DEV")},
			{"project", "DEV"},
			{"missing", []any{"Type", "Клиент"}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestIssueCreateRequiresNoFieldAConditionHides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		state     writableField
		condition string
		missing   []any
	}{
		{
			name:      "the default of the field it watches is not among its values",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Новая"}},
			condition: onlyWhen("180-14", false, "Отклонена"),
			missing:   nil,
		},
		{
			name:      "the default of the field it watches is among its values",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"отклонена"}},
			condition: onlyWhen("180-14", false, "Отклонена"),
			missing:   []any{"Причина отклонения"},
		},
		{
			name:      "the field it watches is filled with nothing and null shows it",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true},
			condition: onlyWhen("180-14", true, "Отклонена"),
			missing:   []any{"Причина отклонения"},
		},
		{
			name:      "the field it watches holds more than one value",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, isMultiValue: true},
			condition: onlyWhen("180-14", false, "Отклонена"),
			missing:   []any{"Причина отклонения"},
		},
		{
			name:      "the project no longer has the field it watches",
			state:     writableField{id: "180-99", name: "State", valueType: "state", canBeEmpty: true},
			condition: onlyWhen("180-14", false, "Отклонена"),
			missing:   []any{"Причина отклонения"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := projectResponse(tc.state, writableField{id: "180-23", name: "Причина отклонения",
				valueType: "enum", condition: tc.condition})
			issue := respondWith(http.StatusOK, createdIssue("DEV-7", "x", "null"))
			if tc.missing != nil {
				issue = noCreation(t)
			}
			server := creating(t, respondWith(http.StatusOK, metadata), issue)

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x")

			if tc.missing == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				return
			}
			assert.Equal(t, detail{"missing", tc.missing}, requireFault(t, got).details[2])
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestIssueCreateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		argv        []string
		summary     string
		description string
		mismatch    []any
	}{
		{
			name:        "a title the server stored otherwise",
			argv:        []string{"--summary", "a b"},
			summary:     "a  b",
			description: "null",
			mismatch:    []any{[]detail{{"field", "summary"}, {"expected", "a b"}, {"actual", "a  b"}}},
		},
		{
			name:        "prose the server kept none of",
			argv:        []string{"--summary", "x", "--description", "первая"},
			summary:     "x",
			description: "null",
			mismatch:    []any{[]detail{{"field", "description"}, {"expected", "первая"}, {"actual", nil}}},
		},
		{
			name:        "both of them",
			argv:        []string{"--summary", "a b", "--description", "первая"},
			summary:     "a  b",
			description: `"вторая"`,
			mismatch: []any{
				[]detail{{"field", "summary"}, {"expected", "a b"}, {"actual", "a  b"}},
				[]detail{{"field", "description"}, {"expected", "первая"}, {"actual", "вторая"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creating(t, respondWith(http.StatusOK, projectRequiringNothing()),
				respondWith(http.StatusOK, createdIssue("DEV-7", tc.summary, tc.description)))

			got := runWith(t, server.env(), append([]string{"issue", "create", "DEV"}, tc.argv...)...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", creationRequest(server.url, askedIssueFields)},
					{"issue", "DEV-7"},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
		})
	}
}

func TestIssueCreateRefusesMetadataOfTheProjectOfAnotherShape(t *testing.T) {
	t.Parallel()
	field := func(members ...string) string {
		return `{"$type":"EnumProjectCustomField","id":"180-15",` + strings.Join(members, ",") +
			`,"field":{"$type":"CustomField","name":"Type","localizedName":null,` +
			`"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}`
	}
	const (
		empty     = `"canBeEmpty":true`
		defaults  = `"defaultValues":[]`
		condition = `"condition":null`
	)
	tests := []struct {
		name    string
		project string
	}{
		{
			name:    "the short name of the project",
			project: `{"$type":"Project","id":"0-1","shortName":7,"customFields":[]}`,
		},
		{
			name:    "whether the project lets the field stand empty",
			project: projectWith(field(`"canBeEmpty":null`, defaults, condition)),
		},
		{
			name:    "a value the project fills the field with unasked",
			project: projectWith(field(empty, `"defaultValues":[{"$type":"EnumBundleElement","name":7}]`, condition)),
		},
		{
			name: "the null the condition of the field shows it for",
			project: projectWith(field(empty, defaults,
				`"condition":{"$type":"FieldBasedCondition","showForNullValue":null,"field":null,"values":[]}`)),
		},
		{
			name: "the field the condition of the field watches",
			project: projectWith(field(empty, defaults,
				`"condition":{"$type":"FieldBasedCondition","showForNullValue":false,`+
					`"field":{"$type":"StateProjectCustomField","id":7},"values":[]}`)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creating(t, respondWith(http.StatusOK, tc.project), noCreation(t))

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", writeMetadataRequest(server.url, "DEV")},
					{"upstream_status", 200},
					{"upstream_body", tc.project},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func projectWith(field string) string {
	return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + field + `]}`
}

func TestIssueCreateIsUncertainWhereTheAnswerCannotBePrinted(t *testing.T) {
	t.Parallel()
	unwrittenStringHoldingANumber := receivedFields(
		receivedField{name: "Примечание", valueType: "string", binding: "187-10", value: "42"})
	server := creating(t, respondWith(http.StatusOK, projectRequiringNothing()),
		respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", unwrittenStringHoldingANumber)))

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, detail{"upstream_status", 200}, found.details[1])
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
}

func TestIssueCreateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	const said = `{"error":"Field required","error_description":"Поле Тип обязательно","error_field":"Тип",` +
		`"error_type":"workflow","error_workflow_type":"require"}`
	server := creating(t, respondWith(http.StatusOK, projectRequiringNothing()), respondWith(http.StatusBadRequest, said))

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x")

	want := faultDocument{
		code: "rejected",
		details: []detail{
			{"request", creationRequest(server.url, askedIssueFields)},
			{"upstream_status", 400},
			{"upstream_error", "Field required"},
			{"upstream_message", "Поле Тип обязательно"},
			{"upstream_body", said},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
}

func TestIssueCreateRefusesWhatTheTokenMayNotReachAt(t *testing.T) {
	t.Parallel()
	t.Run("a project the read does not find", func(t *testing.T) {
		t.Parallel()
		said := `{"error":"Not Found","error_description":"Entity with id NOPE not found"}`
		server := creating(t, respondWith(http.StatusNotFound, said), noCreation(t))

		got := runWith(t, server.env(), "issue", "create", "NOPE", "--summary", "x")

		want := faultDocument{
			code: "not_found",
			details: []detail{
				{"request", writeMetadataRequest(server.url, "NOPE")},
				{"upstream_status", 404},
				{"upstream_error", "Not Found"},
				{"upstream_message", "Entity with id NOPE not found"},
			},
		}
		assert.Equal(t, want, requireFault(t, got))
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
	t.Run("a token that may read the project and not write in it", func(t *testing.T) {
		t.Parallel()
		said := `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`
		server := creating(t, respondWith(http.StatusOK, projectRequiringNothing()), respondWith(http.StatusForbidden, said))

		got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x")

		want := faultDocument{
			code: "denied",
			details: []detail{
				{"request", creationRequest(server.url, askedIssueFields)},
				{"upstream_status", 403},
				{"upstream_error", "Forbidden"},
				{"upstream_message", "HTTP 403 Forbidden"},
				authFromEnv(),
			},
		}
		assert.Equal(t, want, requireFault(t, got))
		assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	})
}

func TestIssueCreateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := creating(t, respondWith(http.StatusOK, projectRequiringNothing()),
		respondWith(http.StatusOK, createdIssue("DEV-7", "x", `"первая"`)))

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x", "--description", "первая",
		"--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "idReadable: \"DEV-7\"\n", got.stdout)
	assert.Equal(t, []string{projectWriteFields, "idReadable,summary,description"}, server.sentFields())
}

func TestIssueCreatePrintsTheCustomFieldsItWasAskedFor(t *testing.T) {
	t.Parallel()
	issue := `{"$type":"Issue","idReadable":"DEV-7","summary":"x","customFields":` + receivedFields(
		receivedField{name: "Priority", valueType: "enum", ordinal: "2", binding: "180-16",
			value: bundleElement("Low")},
		receivedField{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15",
			value: bundleElement("Task")},
	) + `}`
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, cataloguePath):
			respondWith(http.StatusOK, devCatalogue())(w, r)
		case r.Method == http.MethodPost:
			respondWith(http.StatusOK, issue)(w, r)
		default:
			respondWith(http.StatusOK, projectRequiringNothing())(w, r)
		}
	})

	got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x",
		"--fields", `idReadable,customFields("приоритет")`)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "idReadable: \"DEV-7\"\ncustomFields:\n  \"Priority\": \"Low\"\n", got.stdout)
	assert.Equal(t, []string{"/api/admin/projects/DEV", cataloguePath, "/api/issues"}, server.sentPaths())
	for _, query := range server.sentQueries() {
		assert.Empty(t, query["customFields"], "a write never cuts the answer down by name")
	}
}

func contractTitle(t *testing.T) string {
	t.Helper()
	return "ytrack contract " + t.Name()
}

func removeIssue(t *testing.T, dev *upstream, readable string) {
	t.Helper()
	outlivesTheTest := context.Background()
	deleted := runInContext(t, outlivesTheTest, dev.env(), "issue", "delete", readable)
	assert.Equal(t, outcome{stdout: "idReadable: " + strconv.Quote(readable) + "\n"}, deleted)

	gone := runInContext(t, outlivesTheTest, dev.env(), "issue", "show", readable, "--comments=0")
	assert.Equal(t, "not_found", requireFaultDocument(t, gone).code)
}

func TestIssueCreateNamesWhatTheDevProjectRequires(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "create", "DEV", "--summary", contractTitle(t))

	want := faultDocument{
		code: "missing_required",
		details: []detail{
			{"request", writeMetadataRequest(dev.url, "DEV")},
			{"project", "DEV"},
			{"missing", []any{"Type", "Категория", "Клиент", "Модуль системы"}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}

func TestIssueCreateFilesAnIssueTheDemoProjectFillsItself(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	title := contractTitle(t)

	got := runWith(t, dev.env(), "issue", "create", "DEMO", "--summary", title)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	readable := nodeAt(t, mapping, "idReadable").Value
	require.Regexp(t, `^DEMO-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })

	assert.Equal(t, title, nodeAt(t, mapping, "summary").Value)
	for _, filled := range []struct{ field, value string }{
		{field: "Priority", value: "Normal"},
		{field: "Type", value: "Bug"},
		{field: "State", value: "To do"},
	} {
		assert.Equal(t, filled.value, nodeAt(t, mapping, "customFields", filled.field).Value)
	}
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(dev))
}
