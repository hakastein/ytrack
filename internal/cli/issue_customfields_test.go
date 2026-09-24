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

// What the tool asks of every custom field of an issue, whatever the caller asked of the issue: the members an
// identity is read out of, and the binding to the project, which carries the type and the place in the order.
const customFieldsFields = "customFields(name,value(name,login,minutes,text)," +
	"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue))))"

// The same block with the name a project gave each field, which is asked for where a name of a default stands
// among the names asked: such a name reached no catalogue, so the block is where it meets a name of the
// instance.
const translatedCustomFieldsFields = "customFields(name,value(name,login,minutes,text)," +
	"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue),localizedName)))"

// A custom field as the server sends it, with $type on every object: the value and the ordinal are written as
// JSON already, so a scenario may send a shape the specification does not allow.
type arrivedField struct {
	name         string
	translate    string
	valueType    string
	isMultiValue bool
	ordinal      string
	binding      string
	value        string
}

func (f arrivedField) sent() string {
	ordinal, binding := f.ordinal, f.binding
	if ordinal == "" {
		ordinal = "1"
	}
	if binding == "" {
		binding = "180-1"
	}
	// A project that calls the field nothing of its own sends null rather than leaving the name out; a scenario
	// the tool never asks it of is answered it all the same, which is what an unasked-for key of the server is.
	translated := "null"
	if f.translate != "" {
		translated = strconv.Quote(f.translate)
	}
	return `{"$type":"IssueCustomField","name":` + strconv.Quote(f.name) +
		`,"value":` + cmp.Or(f.value, "null") +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(binding) +
		`,"ordinal":` + ordinal +
		`,"field":{"$type":"CustomField","fieldType":{"$type":"FieldType","valueType":` +
		strconv.Quote(f.valueType) + `,"isMultiValue":` + strconv.FormatBool(f.isMultiValue) + `},` +
		`"localizedName":` + translated + `}}}`
}

// The element of a bundle enum, state, version, build and ownedField fields hold, with the presentation the
// server sends beside the name.
func bundleElement(name string) string {
	return `{"$type":"EnumBundleElement","name":` + strconv.Quote(name) +
		`,"localizedName":null,"presentation":` + strconv.Quote(name+" (presentation)") + `}`
}

// arrivedFields is the array of custom fields of one answer, as JSON.
func arrivedFields(fields ...arrivedField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func issueWithFields(fields ...arrivedField) string {
	return `{"$type":"Issue","idReadable":"DEV-1","customFields":` + arrivedFields(fields...) + `}`
}

// showCustomFields is the block one answer prints, as the mapping under customFields.
func showCustomFields(t *testing.T, body string) (outcome, *yaml.Node) {
	t.Helper()
	server := serve(t, answer(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{customFieldsFields}, server.sentFields())
	block := nodeAt(t, requireMapping(t, "stdout", got.stdout), "customFields")
	require.Equal(t, yaml.MappingNode, block.Kind, "stdout: %q", got.stdout)
	return got, block
}

// A value of one type as the server sends it, beside the identity it is printed by: none where the field
// holds nothing.
type identityCase struct {
	name         string
	valueType    string
	isMultiValue bool
	value        string
	printed      []string
	// Text in double quotes unless the row says otherwise.
	style yaml.Style
	tag   string
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

// The twenty types the table of custom-field types holds, in the forms measured on the working instance: what
// is printed is the one member that names the value, and a field holding nothing gets no key at all.
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

// The identity of a value is settled by the type of its field and by nothing else: what the server sends
// beside it to show the value to a human — a presentation, a full name, HTML of the same text — is not
// printed, and neither is the type it arrived under.
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
			body := issueWithFields(arrivedField{
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

// The order is the one the project put its fields in, which the server sends beside each of them and does not
// arrange the array by: the array of an issue is ordered by the prototype of each field, and two installations
// of one polygon need not agree on that.
func TestIssueShowPrintsCustomFieldsInTheOrderOfTheProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrived []arrivedField
		printed []string
	}{
		{
			name: "the order the project gave them",
			arrived: []arrivedField{
				{name: "Priority", valueType: "enum", ordinal: "2", binding: "180-16", value: bundleElement("Medium")},
				{name: "Type", valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
				{name: "State", valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
			},
			printed: []string{"Type", "Priority", "State"},
		},
		{
			name: "two fields of one place, by the number of the binding",
			arrived: []arrivedField{
				{name: "Ten", valueType: "enum", ordinal: "3", binding: "180-10", value: bundleElement("ten")},
				{name: "Nine", valueType: "enum", ordinal: "3", binding: "180-9", value: bundleElement("nine")},
			},
			printed: []string{"Nine", "Ten"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, block := showCustomFields(t, issueWithFields(tc.arrived...))

			keys := []string{}
			for pair := range slices.Chunk(block.Content, 2) {
				keys = append(keys, pair[0].Value)
			}
			assert.Equal(t, tc.printed, keys, "stdout: %q", got.stdout)
		})
	}
}

// A key that comes from the data is held to no grammar of ytrack's: whatever the project called a field, the
// name reads back as itself, and the writer of double-quoted strings is what makes that so.
func TestIssueShowQuotesTheNameOfEveryCustomField(t *testing.T) {
	t.Parallel()
	names := []string{"Оценка (Back)", "Утв. начала работы", "_________________________",
		"Внешний номер", `a: b #c "d"`, "State"}
	arrived := make([]arrivedField, 0, len(names))
	for i, name := range names {
		arrived = append(arrived, arrivedField{
			name: name, valueType: "enum", ordinal: strconv.Itoa(i), binding: "180-" + strconv.Itoa(i),
			value: bundleElement("Medium"),
		})
	}

	got, block := showCustomFields(t, issueWithFields(arrived...))

	keys := []string{}
	for pair := range slices.Chunk(block.Content, 2) {
		keys = append(keys, pair[0].Value)
		assert.Equal(t, yaml.DoubleQuotedStyle, pair[0].Style, "the key %q stands bare", pair[0].Value)
	}
	assert.Equal(t, names, keys, "stdout: %q", got.stdout)
}

// What the server says about a custom field is held against the catalogue of types it publishes itself, and a
// block that disagrees with it is refused whole rather than printed in part.
func TestIssueShowRefusesCustomFieldsTheServerContradictsItselfAbout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrived []arrivedField
	}{
		{
			name:    "a type the catalogue does not hold",
			arrived: []arrivedField{{name: "Field", valueType: "bogus", value: bundleElement("Task")}},
		},
		{
			name: "one value by the type and a list in the answer",
			arrived: []arrivedField{{
				name: "Field", valueType: "enum", value: `[` + bundleElement("Task") + `]`,
			}},
		},
		{
			name: "more than one value by the type and one of them in the answer",
			arrived: []arrivedField{{
				name: "Field", valueType: "enum", isMultiValue: true, value: bundleElement("Task"),
			}},
		},
		{
			// The judgment of names lets this value through: the place holds values of many shapes, and a
			// PeriodValue declaring no login is one of them; what the field's own type says it holds is
			// ytrack's to check.
			name: "a value carrying nothing the type names it by",
			arrived: []arrivedField{{
				name: "Field", valueType: "user", value: `{"$type":"PeriodValue","minutes":90}`,
			}},
		},
		{
			name: "no place among the fields of the project",
			arrived: []arrivedField{{
				name: "Field", valueType: "enum", ordinal: "null", value: bundleElement("Task"),
			}},
		},
		{
			name: "two fields of one name",
			arrived: []arrivedField{
				{name: "Field", valueType: "enum", ordinal: "1", binding: "180-1", value: bundleElement("Task")},
				{name: "Field", valueType: "state", ordinal: "2", binding: "180-2", value: bundleElement("Open")},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithFields(tc.arrived...)
			server := serve(t, answer(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

			assert.Equal(t, refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", customFieldsFields)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireRefusal(t, got))
		})
	}
}

// The place of a custom field among the fields of its project, with every member the tool asks of it.
func bindingOf(id, valueType string) string {
	return `{"$type":"ProjectCustomField","id":` + id + `,"ordinal":1,"field":{"$type":"CustomField",` +
		`"fieldType":{"$type":"FieldType","valueType":` + valueType + `,"isMultiValue":false}}}`
}

// One custom field with each member written as it stands, so that a scenario may send a shape the
// specification does not allow while every name asked for is there: a name the answer lacks altogether is the
// judgment's to refuse, and what is left for the block to hold against the specification is the shape.
func fieldHolding(name, binding string) string {
	return `{"$type":"IssueCustomField","name":` + name + `,"value":null,"projectCustomField":` + binding + `}`
}

// The block is read whole before any of it is printed, so a part of it standing in a shape the specification
// does not give it ends the call rather than printing a document with that field missing or bare.
func TestIssueShowRefusesCustomFieldsOfAShapeTheSpecificationDoesNotGive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// What stands under customFields, as JSON.
		block string
	}{
		{name: "the block is no array", block: `null`},
		{name: "a field is no object", block: `[null]`},
		{name: "a name is no text", block: `[` + fieldHolding(`5`, bindingOf(`"180-1"`, `"enum"`)) + `]`},
		{
			name:  "the field of the project is no object",
			block: `[` + fieldHolding(`"Field"`, `[`+bindingOf(`"180-1"`, `"enum"`)+`]`) + `]`,
		},
		{
			name:  "the binding to the project is named by no text",
			block: `[` + fieldHolding(`"Field"`, bindingOf(`5`, `"enum"`)) + `]`,
		},
		{
			name:  "the type of the field of the project is no text",
			block: `[` + fieldHolding(`"Field"`, bindingOf(`"180-1"`, `5`)) + `]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","idReadable":"DEV-1","customFields":` + tc.block + `}`
			server := serve(t, answer(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

			assert.Equal(t, refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", customFieldsFields)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}, requireRefusal(t, got))
		})
	}
}

// The custom fields of an issue other than the one asked for are the block whole: a name there would pick
// fields out of whatever project that issue belongs to, so it is refused before any request.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The seven fields DEV-1 of the polygon holds something in, out of twenty-seven bound to the project: an empty
// one is no key at all, and the order is the project's, not the one the array arrived in.
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
		// A member sees every custom field of an issue and the place of each among the project's fields,
		// while the fields of the project itself come to them empty.
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

// A field the project marks as one that may not be empty is still left out where it is: what is printed is
// what the issue holds, and DEV-2 holds no reason for the state it was rejected with.
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
