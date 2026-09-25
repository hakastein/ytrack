package cli_test

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const customFieldsFields = "customFields(name,value(name,login,minutes,text)," +
	"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue))))"

const translatedCustomFieldsFields = "customFields(name,value(name,login,minutes,text)," +
	"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue),localizedName)))"

type receivedField struct {
	name         string
	translate    string
	valueType    string
	isMultiValue bool
	ordinal      string
	binding      string
	value        string
}

func (f receivedField) sent() string {
	ordinal, binding := f.ordinal, f.binding
	if ordinal == "" {
		ordinal = "1"
	}
	if binding == "" {
		binding = "180-1"
	}
	return `{"$type":"IssueCustomField","name":` + strconv.Quote(f.name) +
		`,"value":` + cmp.Or(f.value, "null") +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(binding) +
		`,"ordinal":` + ordinal +
		`,"field":{"$type":"CustomField","fieldType":{"$type":"FieldType","valueType":` +
		strconv.Quote(f.valueType) + `,"isMultiValue":` + strconv.FormatBool(f.isMultiValue) + `},` +
		`"localizedName":` + localizedNameOrNull(f.translate) + `}}}`
}

func bundleElement(name string) string {
	return `{"$type":"EnumBundleElement","name":` + strconv.Quote(name) +
		`,"localizedName":null,"presentation":` + strconv.Quote(name+" (presentation)") + `}`
}

func receivedFields(fields ...receivedField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func issueWithFields(fields ...receivedField) string {
	return `{"$type":"Issue","idReadable":"DEV-1","customFields":` + receivedFields(fields...) + `}`
}

func showCustomFields(t *testing.T, body string) (outcome, *yaml.Node) {
	t.Helper()
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{customFieldsFields}, server.sentFields())
	block := nodeAt(t, requireMapping(t, "stdout", got.stdout), "customFields")
	require.Equal(t, yaml.MappingNode, block.Kind, "stdout: %q", got.stdout)
	return got, block
}

type identityCase struct {
	name         string
	valueType    string
	isMultiValue bool
	value        string
	printed      []string
	style        yaml.Style
	tag          string
}

func (c identityCase) written() yaml.Style {
	if c.style == 0 && c.tag == "" {
		return yaml.DoubleQuotedStyle
	}
	return c.style
}

func (c identityCase) read() string {
	return cmp.Or(c.tag, "!!str")
}

func identityCases() []identityCase {
	return []identityCase{
		{
			name: "an enum", valueType: "enum",
			value: bundleElement("Task"), printed: []string{"Task"},
		},
		{
			name: "enums", valueType: "enum", isMultiValue: true,
			value:   `[` + bundleElement("ACME") + `,` + bundleElement("Инфраструктура. DevOps") + `]`,
			printed: []string{"ACME", "Инфраструктура. DevOps"},
		},
		{
			name: "a state", valueType: "state",
			value:   `{"$type":"StateBundleElement","name":"In Progress","isResolved":false,"localizedName":"В работе"}`,
			printed: []string{"In Progress"},
		},
		{
			name: "a version", valueType: "version",
			value:   `{"$type":"VersionBundleElement","name":"2026.1","released":true}`,
			printed: []string{"2026.1"},
		},
		{
			name: "versions", valueType: "version", isMultiValue: true,
			value:   `[{"$type":"VersionBundleElement","name":"SPR-92"}]`,
			printed: []string{"SPR-92"},
		},
		{
			name: "a build", valueType: "build",
			value:   `{"$type":"BuildBundleElement","name":"13757"}`,
			printed: []string{"13757"},
		},
		{
			name: "builds", valueType: "build", isMultiValue: true,
			value:   `[{"$type":"BuildBundleElement","name":"2026.1.1"}]`,
			printed: []string{"2026.1.1"},
		},
		{
			name: "an owned value", valueType: "ownedField",
			value:   `{"$type":"OwnedBundleElement","name":"Ядро","owner":{"$type":"User","login":"admin"}}`,
			printed: []string{"Ядро"},
		},
		{
			name: "owned values", valueType: "ownedField", isMultiValue: true,
			value:   `[{"$type":"OwnedBundleElement","name":"Биллинг"}]`,
			printed: []string{"Биллинг"},
		},
		{
			name: "a user", valueType: "user",
			value:   `{"$type":"User","login":"ivan.ivanov","name":"Иван Иванов","fullName":"Иван Иванов"}`,
			printed: []string{"ivan.ivanov"},
		},
		{
			name: "users", valueType: "user", isMultiValue: true,
			value:   `[{"$type":"User","login":"dev.member","name":"Участник","fullName":"Участник"}]`,
			printed: []string{"dev.member"},
		},
		{
			name: "a group", valueType: "group",
			value:   `{"$type":"ProjectTeam","name":"DEVELOPMENT Team"}`,
			printed: []string{"DEVELOPMENT Team"},
		},
		{
			name: "groups", valueType: "group", isMultiValue: true,
			value:   `[{"$type":"NestedGroup","name":"Участники полигона"}]`,
			printed: []string{"Участники полигона"},
		},
		{
			name: "a period", valueType: "period",
			value:   `{"$type":"PeriodValue","minutes":90,"id":"PT1H30M","presentation":"1ч 30м"}`,
			printed: []string{"PT1H30M"},
		},
		{
			name: "a period the server counts in working days", valueType: "period",
			value:   `{"$type":"PeriodValue","minutes":1635,"id":"P3DT3H15M","presentation":"3д 3ч 15м"}`,
			printed: []string{"PT27H15M"},
		},
		{
			name: "a period of whole hours", valueType: "period",
			value:   `{"$type":"PeriodValue","minutes":60,"id":"PT1H","presentation":"1ч"}`,
			printed: []string{"PT1H"},
		},
		{
			name: "a period of no time at all", valueType: "period",
			value:   `{"$type":"PeriodValue","minutes":0,"id":"PT0S","presentation":"0м"}`,
			printed: []string{"PT0M"},
		},
		{
			name: "a date", valueType: "date",
			value: `1789560000000`, printed: []string{"2026-09-16"},
		},
		{
			name: "a date and time", valueType: "date and time",
			value: `1788134400000`, printed: []string{"2026-08-31T00:00:00Z"},
		},
		{
			name: "an integer", valueType: "integer",
			value: `1`, printed: []string{"1"}, tag: "!!int",
		},
		{
			name: "a float", valueType: "float",
			value: `1.5`, printed: []string{"1.5"}, tag: "!!float",
		},
		{
			name: "a string", valueType: "string",
			value: `"EXT-1"`, printed: []string{"EXT-1"},
		},
		{
			name:      "a text",
			valueType: "text",
			value: `{"$type":"TextFieldValue","id":"text","text":"\n  а\nб",` +
				`"markdownText":"<div class=\"wiki\">а</div>"}`,
			printed: []string{"\n  а\nб"}, style: yaml.LiteralStyle,
		},
		{name: "a field holding nothing", valueType: "enum", value: `null`},
		{name: "a field holding no value at all", valueType: "enum", isMultiValue: true, value: `[]`},
		{
			name: "a text field with no text", valueType: "text",
			value: `{"$type":"TextFieldValue","id":"text","text":null,"markdownText":null}`,
		},
	}
}

func TestIssueShowPrintsACustomFieldByTheIdentityOfItsType(t *testing.T) {
	t.Parallel()
	const named = "Field"
	types := map[string]bool{}
	for _, tc := range identityCases() {
		types[tc.valueType+strconv.FormatBool(tc.isMultiValue)] = true
	}
	assert.Len(t, types, 20, "the cases cover fewer than the twenty types of the table")
	for _, tc := range identityCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithFields(receivedField{
				name: named, valueType: tc.valueType, isMultiValue: tc.isMultiValue, value: tc.value,
			})

			got, block := showCustomFields(t, body)

			for _, hidden := range []string{"presentation", "markdownText", "fullName", "$type", "localizedName"} {
				assert.NotContains(t, got.stdout, hidden)
			}
			if len(tc.printed) == 0 {
				assert.Empty(t, block.Content, "stdout: %q", got.stdout)
				return
			}
			require.Len(t, block.Content, 2, "stdout: %q", got.stdout)
			assert.Equal(t, named, block.Content[0].Value)
			printed := []*yaml.Node{block.Content[1]}
			if tc.isMultiValue {
				require.Equal(t, yaml.SequenceNode, block.Content[1].Kind, "stdout: %q", got.stdout)
				printed = block.Content[1].Content
			}
			require.Len(t, printed, len(tc.printed), "stdout: %q", got.stdout)
			for i, want := range tc.printed {
				assert.Equal(t, want, printed[i].Value, "stdout: %q", got.stdout)
				assert.Equal(t, tc.written(), printed[i].Style, "stdout: %q", got.stdout)
				assert.Equal(t, tc.read(), printed[i].ShortTag(), "stdout: %q", got.stdout)
			}
		})
	}
}

func TestIssueShowPrintsCustomFieldsInTheOrderOfTheProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received []receivedField
		printed  []string
	}{
		{
			name: "the order the project gave them",
			received: []receivedField{
				{name: "Priority", valueType: "enum", ordinal: "2", binding: "180-16", value: bundleElement("Medium")},
				{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
				{name: "State", valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
			},
			printed: []string{"Type", "Priority", "State"},
		},
		{
			name: "two fields of one place, by the number of the binding",
			received: []receivedField{
				{name: "Ten", valueType: "enum", ordinal: "3", binding: "180-10", value: bundleElement("ten")},
				{name: "Nine", valueType: "enum", ordinal: "3", binding: "180-9", value: bundleElement("nine")},
			},
			printed: []string{"Nine", "Ten"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, block := showCustomFields(t, issueWithFields(tc.received...))

			keys := []string{}
			for pair := range slices.Chunk(block.Content, 2) {
				keys = append(keys, pair[0].Value)
			}
			assert.Equal(t, tc.printed, keys, "stdout: %q", got.stdout)
		})
	}
}

func TestIssueShowQuotesTheNameOfEveryCustomField(t *testing.T) {
	t.Parallel()
	names := []string{"Оценка (Back)", "Утв. начала работы", "_________________________",
		"Внешний номер", `a: b #c "d"`, "State"}
	received := make([]receivedField, 0, len(names))
	for i, name := range names {
		received = append(received, receivedField{
			name: name, valueType: "enum", ordinal: strconv.Itoa(i), binding: "180-" + strconv.Itoa(i),
			value: bundleElement("Medium"),
		})
	}

	got, block := showCustomFields(t, issueWithFields(received...))

	keys := []string{}
	for pair := range slices.Chunk(block.Content, 2) {
		keys = append(keys, pair[0].Value)
		assert.Equal(t, yaml.DoubleQuotedStyle, pair[0].Style, "the key %q stands bare", pair[0].Value)
	}
	assert.Equal(t, names, keys, "stdout: %q", got.stdout)
}

func TestIssueShowRefusesCustomFieldsTheServerContradictsItselfAbout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received []receivedField
	}{
		{
			name:     "a type the catalogue does not hold",
			received: []receivedField{{name: "Field", valueType: "bogus", value: bundleElement("Task")}},
		},
		{
			name: "one value by the type and a list in the answer",
			received: []receivedField{{
				name: "Field", valueType: "enum", value: `[` + bundleElement("Task") + `]`,
			}},
		},
		{
			name: "more than one value by the type and one of them in the answer",
			received: []receivedField{{
				name: "Field", valueType: "enum", isMultiValue: true, value: bundleElement("Task"),
			}},
		},
		{
			name: "a value carrying nothing the type names it by",
			received: []receivedField{{
				name: "Field", valueType: "user", value: `{"$type":"PeriodValue","minutes":90}`,
			}},
		},
		{
			name: "no place among the fields of the project",
			received: []receivedField{{
				name: "Field", valueType: "enum", ordinal: "null", value: bundleElement("Task"),
			}},
		},
		{
			name: "two fields of one name",
			received: []receivedField{
				{name: "Field", valueType: "enum", ordinal: "1", binding: "180-1", value: bundleElement("Task")},
				{name: "Field", valueType: "state", ordinal: "2", binding: "180-2", value: bundleElement("Open")},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithFields(tc.received...)
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", customFieldsFields)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireFault(t, got))
		})
	}
}

func bindingOf(id, valueType string) string {
	return `{"$type":"ProjectCustomField","id":` + id + `,"ordinal":1,"field":{"$type":"CustomField",` +
		`"fieldType":{"$type":"FieldType","valueType":` + valueType + `,"isMultiValue":false}}}`
}

func fieldWith(name, binding string) string {
	return `{"$type":"IssueCustomField","name":` + name + `,"value":null,"projectCustomField":` + binding + `}`
}

func TestIssueShowRefusesCustomFieldsOfAShapeTheSpecificationDoesNotGive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		block string
	}{
		{name: "the block is no array", block: `null`},
		{name: "a field is no object", block: `[null]`},
		{name: "a name is no text", block: `[` + fieldWith(`5`, bindingOf(`"180-1"`, `"enum"`)) + `]`},
		{
			name:  "the field of the project is no object",
			block: `[` + fieldWith(`"Field"`, `[`+bindingOf(`"180-1"`, `"enum"`)+`]`) + `]`,
		},
		{
			name:  "the binding to the project is named by no text",
			block: `[` + fieldWith(`"Field"`, bindingOf(`5`, `"enum"`)) + `]`,
		},
		{
			name:  "the type of the field of the project is no text",
			block: `[` + fieldWith(`"Field"`, bindingOf(`"180-1"`, `5`)) + `]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","idReadable":"DEV-1","customFields":` + tc.block + `}`
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", customFieldsFields)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireFault(t, got))
		})
	}
}

func TestIssueShowRefusesNamesWrittenUnderTheCustomFieldsOfAnotherIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "under the issues of a link", expression: "links(issues(customFields(name)))"},
		{name: "under a parent", expression: "parent(issues(customFields(State)))"},
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

func TestIssueShowPrintsTheCustomFieldsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	printed := []detail{
		{"Type", "Task"},
		{"Priority", "Medium"},
		{"Категория", "Развитие технологий"},
		{"Клиент", []any{"ACME"}},
		{"Модуль системы", []any{"Инфраструктура. DevOps"}},
		{"State", "In Progress"},
		{"Затраченное время", "PT1H30M"},
	}
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

			got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + tc.token(t)},
				"issue", "show", "DEV-1", "--fields", "customFields", "--comments=0")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []detail{{"customFields", printed}}, requireDocument(t, got.stdout))
			for _, hidden := range []string{"$type", "1ч 30м", "presentation"} {
				assert.NotContains(t, got.stdout, hidden)
			}
			assert.Equal(t, []string{customFieldsFields}, dev.sentFields())
			assert.Len(t, dev.requests(), 1)
		})
	}
}

func TestIssueShowLeavesOutTheEmptyCustomFieldsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-2", "--fields", "customFields", "--comments=0")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []detail{{"customFields", []detail{
		{"Type", "Task"},
		{"Priority", "Medium"},
		{"Категория", "Развитие технологий"},
		{"Клиент", []any{"ACME"}},
		{"Модуль системы", []any{"Инфраструктура. DevOps"}},
		{"State", "Отклонена"},
	}}}, requireDocument(t, got.stdout))
	assert.NotContains(t, got.stdout, "Причина отклонения")
}
