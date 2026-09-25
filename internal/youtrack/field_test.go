package youtrack_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	fieldMetaPath        = "/api/admin/projects/DEV"
	fieldMetaBindingPath = fieldMetaPath + "/customFields/"
)

const (
	fieldMetaNaming     = "field(name,localizedName,fieldType(valueType,isMultiValue))"
	fieldMetaBare       = fieldMetaNaming + ",canBeEmpty"
	fieldMetaWithValues = fieldMetaBare + ",bundle(values(name,archived))"
	fieldMetaWithUsers  = fieldMetaBare + ",bundle(aggregatedUsers(login))"
)

func fieldMetaOf(name, localizedName, valueType string, isMultiValue bool) string {
	return fmt.Sprintf(`{"$type":"CustomField","name":%q,"localizedName":%s,`+
		`"fieldType":{"$type":"FieldType","valueType":%q,"isMultiValue":%t}}`,
		name, localizedName, valueType, isMultiValue)
}

func fieldMetaEnum(name, localizedName string) string {
	return fieldMetaOf(name, localizedName, "enum", false)
}

func fieldMetaBinding(id, naming string) string {
	return fmt.Sprintf(`{"$type":"ProjectCustomField","id":%q,"field":%s}`, id, naming)
}

func fieldMetaProject(bindings ...string) string {
	return `{"$type":"Project","customFields":[` + strings.Join(bindings, ",") + `]}`
}

func fieldMetaAnswer(naming string) string {
	return `{"$type":"ProjectCustomField","field":` + naming + `,"canBeEmpty":true,"ordinal":0,"bundle":null}`
}

func fieldMetaServer(t *testing.T, metadata string, answers map[string]string) *fake.Server {
	t.Helper()
	routes := http.NewServeMux()
	routes.Handle("GET "+fieldMetaPath, fake.JSON(http.StatusOK, metadata))
	routes.HandleFunc("GET "+fieldMetaBindingPath+"{id}", func(w http.ResponseWriter, r *http.Request) {
		fake.JSON(http.StatusOK, answers[r.PathValue("id")])(w, r)
	})
	return fake.Serve(t, routes.ServeHTTP)
}

func fieldMetaShow(t *testing.T, c *youtrack.Client, name string, expression *string) (*render.Node, *diag.Fault) {
	t.Helper()
	call, fault := youtrack.ShowField("DEV", name, expression)
	require.Nil(t, fault)
	return call(t.Context(), c)
}

func fieldMetaList(t *testing.T, server *fake.Server, expression string) (*render.Node, *diag.Fault) {
	t.Helper()
	call, fault := youtrack.ListFields("DEV", expression)
	require.Nil(t, fault)
	return call(t.Context(), client(t, server))
}

func fieldMetaNamed(name string) *render.Node {
	return render.NewMap(render.Pair{Key: "field", Value: render.NewMap(render.Pair{Key: "name", Value: render.NewString(name)})})
}

func fieldMetaListed(records ...*render.Node) *render.Node {
	count := render.NewNumber(json.Number(strconv.Itoa(len(records))))
	return render.NewMap(
		render.Pair{Key: "total", Value: count},
		render.Pair{Key: "returned", Value: count},
		render.Pair{Key: "truncated", Value: render.NewBool(false)},
		render.Pair{Key: "fields", Value: render.NewList(records...)})
}

func fieldMetaDenied(t *testing.T, server *fake.Server) diag.Fault {
	t.Helper()
	return diag.Fault{Code: diag.Denied, Details: []render.Pair{
		lastRequest(t, server),
		{Key: "project", Value: render.NewString("DEV")},
		{Key: "permission", Value: render.NewString("jetbrains.jetpass.project-read")},
	}}
}

func fieldMetaUnknown(t *testing.T, server *fake.Server, asked string, nearest ...string) diag.Fault {
	t.Helper()
	return diag.Fault{Code: diag.UnknownName, Details: []render.Pair{
		lastRequest(t, server),
		{Key: "project", Value: render.NewString("DEV")},
		{Key: "unknown", Value: render.NewList(withNearest("field", asked, nearest...))},
	}}
}

func TestListFieldsRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

	_, fault := fieldMetaList(t, server, youtrack.FieldListFields)

	assert.Equal(t, fieldMetaDenied(t, server), refusal(t, fault))
}

func TestShowFieldRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fieldMetaServer(t, fieldMetaProject(), nil)

	_, fault := fieldMetaShow(t, client(t, server), "First", nil)

	assert.Equal(t, fieldMetaDenied(t, server), refusal(t, fault))
	assert.Equal(t, []string{fieldMetaPath}, server.Paths())
}

func fieldMetaPlaced(ordinal, name string) string {
	return fmt.Sprintf(`{"$type":"ProjectCustomField","ordinal":%s,"field":{"$type":"CustomField","name":%q}}`, ordinal, name)
}

// Go sorts up to twelve elements by insertion, which keeps their order whether the sort is stable or not.
func fieldMetaOfOneOrdinal() (fields []string, printed []*render.Node) {
	for at := range 13 {
		name := fmt.Sprintf("Unplaced %02d", at)
		fields = append(fields, fieldMetaPlaced("0", name))
		printed = append(printed, fieldMetaNamed(name))
	}
	return fields, printed
}

func TestListFieldsPrintsTheFieldsByOrdinal(t *testing.T) {
	t.Parallel()
	unplaced, unplacedPrinted := fieldMetaOfOneOrdinal()
	tests := []struct {
		name       string
		fields     string
		expression string
		want       *render.Node
	}{
		{
			name:       "fields the server sent out of order",
			fields:     `[` + fieldMetaPlaced("3", "Third") + `,` + fieldMetaPlaced("1", "First") + `,` + fieldMetaPlaced("2", "Second") + `]`,
			expression: "field(name)",
			want:       fieldMetaListed(fieldMetaNamed("First"), fieldMetaNamed("Second"), fieldMetaNamed("Third")),
		},
		{
			name:       "fields of one ordinal, in the order the server sent them",
			fields:     `[` + fieldMetaPlaced("2", "Second") + `,` + strings.Join(unplaced, ",") + `,` + fieldMetaPlaced("1", "First") + `]`,
			expression: "field(name)",
			want:       fieldMetaListed(append(unplacedPrinted, fieldMetaNamed("First"), fieldMetaNamed("Second"))...),
		},
		{
			name:       "the ordinal asked for",
			fields:     `[` + fieldMetaPlaced("2", "Second") + `,` + fieldMetaPlaced("1", "First") + `]`,
			expression: "field(name),ordinal",
			want: fieldMetaListed(
				render.NewMap(
					render.Pair{Key: "field", Value: render.NewMap(render.Pair{Key: "name", Value: render.NewString("First")})},
					render.Pair{Key: "ordinal", Value: render.NewNumber("1")}),
				render.NewMap(
					render.Pair{Key: "field", Value: render.NewMap(render.Pair{Key: "name", Value: render.NewString("Second")})},
					render.Pair{Key: "ordinal", Value: render.NewNumber("2")})),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.fields))

			got, fault := fieldMetaList(t, server, tc.expression)

			require.Nil(t, fault)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestListFieldsRefusesAnOrdinalItCannotOrderBy(t *testing.T) {
	t.Parallel()
	for _, ordinal := range []string{`"1"`, `null`, `1.5`} {
		t.Run(ordinal, func(t *testing.T) {
			t.Parallel()
			fields := `[` + fieldMetaPlaced(ordinal, "First") + `]`
			server := fake.Serve(t, fake.JSON(http.StatusOK, fields))

			_, fault := fieldMetaList(t, server, "field(name)")

			assert.Equal(t, unreadable(lastRequest(t, server), fields), refusal(t, fault))
		})
	}
}

func TestShowFieldRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		field      string
		expression *string
	}{
		{name: "a field of no name", field: ""},
		{name: "fields of nothing at all", field: "First", expression: new("")},
		{name: "fields that close nothing", field: "First", expression: new("field(")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ShowField("DEV", tc.field, tc.expression)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestShowFieldAddressesTheFieldItsNameResolvesTo(t *testing.T) {
	t.Parallel()
	namings := map[string]string{
		"1-1": fieldMetaEnum("First", `null`),
		"1-2": fieldMetaEnum("Second", `"Translated"`),
		"1-3": fieldMetaEnum("Shared", `null`),
		"1-4": fieldMetaEnum("Other", `"Shared"`),
	}
	metadata := fieldMetaProject(
		fieldMetaBinding("1-1", namings["1-1"]), fieldMetaBinding("1-2", namings["1-2"]),
		fieldMetaBinding("1-3", namings["1-3"]), fieldMetaBinding("1-4", namings["1-4"]))
	answers := map[string]string{
		"1-1": fieldMetaAnswer(namings["1-1"]),
		"1-2": fieldMetaAnswer(namings["1-2"]),
		"1-3": fieldMetaAnswer(namings["1-3"]),
		"1-4": fieldMetaAnswer(namings["1-4"]),
	}
	tests := []struct {
		name  string
		asked string
		id    string
	}{
		{name: "a name", asked: "First", id: "1-1"},
		{name: "a name in another letter case", asked: "FIRST", id: "1-1"},
		{name: "a translation in another letter case", asked: "translated", id: "1-2"},
		{name: "a name that is also the translation of another field", asked: "shared", id: "1-3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fieldMetaServer(t, metadata, answers)

			_, fault := fieldMetaShow(t, client(t, server), tc.asked, new("canBeEmpty"))

			require.Nil(t, fault)
			assert.Equal(t, []string{fieldMetaPath, fieldMetaBindingPath + tc.id}, server.Paths())
		})
	}
}

func TestShowFieldAsksForTheFieldsOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		valueType    string
		isMultiValue bool
		expression   *string
		sent         string
	}{
		{name: "enum of one value", valueType: "enum", sent: fieldMetaWithValues},
		{name: "enum of many values", valueType: "enum", isMultiValue: true, sent: fieldMetaWithValues},
		{name: "state of one value", valueType: "state", sent: fieldMetaWithValues},
		{name: "version of one value", valueType: "version", sent: fieldMetaWithValues},
		{name: "version of many values", valueType: "version", isMultiValue: true, sent: fieldMetaWithValues},
		{name: "build of one value", valueType: "build", sent: fieldMetaWithValues},
		{name: "build of many values", valueType: "build", isMultiValue: true, sent: fieldMetaWithValues},
		{name: "ownedField of one value", valueType: "ownedField", sent: fieldMetaWithValues},
		{name: "ownedField of many values", valueType: "ownedField", isMultiValue: true, sent: fieldMetaWithValues},
		{name: "user of one value", valueType: "user", sent: fieldMetaWithUsers},
		{name: "user of many values", valueType: "user", isMultiValue: true, sent: fieldMetaWithUsers},
		{name: "group of one value", valueType: "group", sent: fieldMetaBare},
		{name: "group of many values", valueType: "group", isMultiValue: true, sent: fieldMetaBare},
		{name: "period of one value", valueType: "period", sent: fieldMetaBare},
		{name: "text of one value", valueType: "text", sent: fieldMetaBare},
		{name: "date of one value", valueType: "date", sent: fieldMetaBare},
		{name: "date and time of one value", valueType: "date and time", sent: fieldMetaBare},
		{name: "integer of one value", valueType: "integer", sent: fieldMetaBare},
		{name: "float of one value", valueType: "float", sent: fieldMetaBare},
		{name: "string of one value", valueType: "string", sent: fieldMetaBare},
		{name: "enum of one value with fields +ordinal", valueType: "enum", expression: new("+ordinal"), sent: fieldMetaWithValues + ",ordinal"},
		{name: "user of one value with fields +ordinal", valueType: "user", expression: new("+ordinal"), sent: fieldMetaWithUsers + ",ordinal"},
		{name: "string of one value with fields +ordinal", valueType: "string", expression: new("+ordinal"), sent: fieldMetaBare + ",ordinal"},
		{name: "enum of one value with fields field(name)", valueType: "enum", expression: new("field(name)"), sent: fieldMetaNaming},
		{name: "state of many values with fields field(name)", valueType: "state", isMultiValue: true, expression: new("field(name)"), sent: fieldMetaNaming},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			naming := fieldMetaOf("Field", `null`, tc.valueType, tc.isMultiValue)
			server := fieldMetaServer(t, fieldMetaProject(fieldMetaBinding("1-1", naming)),
				map[string]string{"1-1": fieldMetaAnswer(naming)})

			_, fault := fieldMetaShow(t, client(t, server), "Field", tc.expression)

			require.Nil(t, fault)
			assert.Equal(t, tc.sent, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestShowFieldRefusesMetadataItCannotRead(t *testing.T) {
	t.Parallel()
	naming := fieldMetaEnum("Field", `null`)
	stateOfMany := fieldMetaOf("Field", `null`, "state", true)
	tests := []struct {
		name       string
		metadata   string
		expression *string
	}{
		{name: "custom fields that are no array", metadata: `{"$type":"Project","customFields":` + fieldMetaBinding("1-1", naming) + `}`},
		{name: "a custom field that is no object", metadata: `{"$type":"Project","customFields":[[]]}`},
		{name: "an id that is no text", metadata: fieldMetaProject(`{"$type":"ProjectCustomField","id":5,"field":` + naming + `}`)},
		{name: "an id no path can hold", metadata: fieldMetaProject(fieldMetaBinding("..", naming))},
		{name: "a field that is no object", metadata: fieldMetaProject(fieldMetaBinding("1-1", `null`))},
		{
			name: "a name that is no text",
			metadata: fieldMetaProject(fieldMetaBinding("1-1", `{"$type":"CustomField","name":5,"localizedName":null,`+
				`"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}`)),
		},
		{
			name: "a type that is no object",
			metadata: fieldMetaProject(fieldMetaBinding("1-1",
				`{"$type":"CustomField","name":"Field","localizedName":null,"fieldType":[]}`)),
		},
		{
			name: "a type of value that is no text",
			metadata: fieldMetaProject(fieldMetaBinding("1-1", `{"$type":"CustomField","name":"Field","localizedName":null,`+
				`"fieldType":{"$type":"FieldType","valueType":5,"isMultiValue":false}}`)),
		},
		{
			name: "a multiplicity that is no bool",
			metadata: fieldMetaProject(fieldMetaBinding("1-1", `{"$type":"CustomField","name":"Field","localizedName":null,`+
				`"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":"no"}}`)),
		},
		{name: "a translation that is neither text nor null", metadata: fieldMetaProject(fieldMetaBinding("1-1", fieldMetaEnum("Field", `5`)))},
		{name: "a type outside the ones ytrack models", metadata: fieldMetaProject(fieldMetaBinding("1-1", stateOfMany))},
		{
			name:       "a type outside the ones ytrack models, with fields added to its default",
			metadata:   fieldMetaProject(fieldMetaBinding("1-1", stateOfMany)),
			expression: new("+ordinal"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fieldMetaServer(t, tc.metadata, nil)

			_, fault := fieldMetaShow(t, client(t, server), "Field", tc.expression)

			assert.Equal(t, unreadable(lastRequest(t, server), tc.metadata), refusal(t, fault))
			assert.Equal(t, []string{fieldMetaPath}, server.Paths())
		})
	}
}

func TestShowFieldRefusesAFieldItCannotCompare(t *testing.T) {
	t.Parallel()
	const answer = `{"$type":"ProjectCustomField","field":[],"canBeEmpty":true}`
	server := fieldMetaServer(t, fieldMetaProject(fieldMetaBinding("1-1", fieldMetaEnum("Field", `null`))),
		map[string]string{"1-1": answer})

	_, fault := fieldMetaShow(t, client(t, server), "Field", new("canBeEmpty"))

	assert.Equal(t, unreadable(lastRequest(t, server), answer), refusal(t, fault))
}

func TestShowFieldRefusesAFieldThatChangedBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		naming string
	}{
		{name: "another name", naming: fieldMetaOf("Renamed", `"Translated"`, "enum", false)},
		{name: "another translation", naming: fieldMetaOf("Field", `"Retranslated"`, "enum", false)},
		{name: "no translation", naming: fieldMetaOf("Field", `null`, "enum", false)},
		{name: "another type of value", naming: fieldMetaOf("Field", `"Translated"`, "state", false)},
		{name: "many values where one was", naming: fieldMetaOf("Field", `"Translated"`, "enum", true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := fieldMetaAnswer(tc.naming)
			metadata := fieldMetaProject(fieldMetaBinding("1-1", fieldMetaOf("Field", `"Translated"`, "enum", false)))
			server := fieldMetaServer(t, metadata, map[string]string{"1-1": answer})

			_, fault := fieldMetaShow(t, client(t, server), "Field", new("canBeEmpty"))

			want := diag.Fault{Code: diag.UpstreamFailed, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "project", Value: render.NewString("DEV")},
				{Key: "field", Value: render.NewString("Field")},
				{Key: "upstream_body", Value: render.NewString(answer)},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestShowFieldNamesTheCandidatesOfANameItCannotResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		asked    string
		metadata string
		named    []string
	}{
		{
			name:  "a name of two fields alike but for letter case",
			asked: "upper",
			metadata: fieldMetaProject(
				fieldMetaBinding("1-1", fieldMetaEnum("Upper", `null`)),
				fieldMetaBinding("1-2", fieldMetaEnum("UPPER", `null`)),
				fieldMetaBinding("1-3", fieldMetaEnum("Uppe", `null`))),
			named: []string{"UPPER", "Upper"},
		},
		{
			name:  "a translation of two fields",
			asked: "shared",
			metadata: fieldMetaProject(
				fieldMetaBinding("1-1", fieldMetaEnum("First", `"Shared"`)),
				fieldMetaBinding("1-2", fieldMetaEnum("Second", `"Shared"`)),
				fieldMetaBinding("1-3", fieldMetaEnum("Share", `null`))),
			named: []string{"First", "Second"},
		},
		{
			name:  "a name of no field, beside five names nearer than the rest",
			asked: "Type",
			metadata: fieldMetaProject(
				fieldMetaBinding("1-1", fieldMetaEnum("Typl", `null`)),
				fieldMetaBinding("1-2", fieldMetaEnum("Typi", `null`)),
				fieldMetaBinding("1-3", fieldMetaEnum("Typf", `null`)),
				fieldMetaBinding("1-4", fieldMetaEnum("Ty", `null`)),
				fieldMetaBinding("1-5", fieldMetaEnum("Typk", `null`)),
				fieldMetaBinding("1-6", fieldMetaEnum("Typg", `null`)),
				fieldMetaBinding("1-7", fieldMetaEnum("Typj", `null`)),
				fieldMetaBinding("1-8", fieldMetaEnum("Typh", `null`))),
			named: []string{"Typf", "Typg", "Typh", "Typi", "Typj"},
		},
		{
			name:  "a name of no field, beside one name nearer than another",
			asked: "Type",
			metadata: fieldMetaProject(
				fieldMetaBinding("1-1", fieldMetaEnum("Typf", `null`)),
				fieldMetaBinding("1-2", fieldMetaEnum("Typxyz", `null`))),
			named: []string{"Typf"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fieldMetaServer(t, tc.metadata, nil)

			_, fault := fieldMetaShow(t, client(t, server), tc.asked, nil)

			assert.Equal(t, fieldMetaUnknown(t, server, tc.asked, tc.named...), refusal(t, fault))
			assert.Equal(t, []string{fieldMetaPath}, server.Paths())
		})
	}
}
