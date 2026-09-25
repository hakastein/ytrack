package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const fieldListDefault = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"

const fieldListSent = fieldListDefault + ",ordinal"

func fieldsRequest(address, project, fields string) string {
	return "GET " + address + "/api/admin/projects/" + project + "/customFields?fields=" + fields + "&$top=-1"
}

func fieldsQueries(fields string) []url.Values {
	return []url.Values{{"fields": {fields}, "$top": {"-1"}}}
}

func TestFieldRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a project code the generated client would send elsewhere", argv: []string{"field", "list", ".."}},
		{name: "fields that are not an expression", argv: []string{"field", "list", "DEV", "--fields", "field("}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

func TestFieldRefusesAFlagGivenTwice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "fields of field list", argv: []string{"field", "list", "DEV", "--fields", "field(name)", "--fields", "ordinal"}},
		{name: "fields of field show", argv: []string{"field", "show", "DEV", "Type", "--fields", "field(name)", "--fields", "canBeEmpty"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
		})
	}
}

const shuffledFields = `[
	{"$type":"SimpleProjectCustomField","ordinal":3,"canBeEmpty":true,
	 "field":{"$type":"CustomField","fieldType":{"isMultiValue":false,"valueType":"string","$type":"FieldType"},"localizedName":null,"name":"Third"}},
	{"canBeEmpty":false,"$type":"EnumProjectCustomField","ordinal":1,
	 "field":{"name":"First","localizedName":"Первое","$type":"CustomField","fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":true}}},
	{"ordinal":2,"canBeEmpty":true,"$type":"StateProjectCustomField",
	 "field":{"fieldType":{"valueType":"state","isMultiValue":false,"$type":"FieldType"},"name":"Second","localizedName":null,"$type":"CustomField"}}
]`

func TestFieldListPrintsTheKeysAsAskedAndTheRecordsByOrdinal(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, shuffledFields))

	got := runWith(t, server.Env(), "field", "list", "DEV")

	want := "total: 3\nreturned: 3\ntruncated: false\nfields:\n" +
		`  - {field: {name: "First", localizedName: "Первое", fieldType: {valueType: "enum", isMultiValue: true}}, canBeEmpty: false}` + "\n" +
		`  - {field: {name: "Second", localizedName: null, fieldType: {valueType: "state", isMultiValue: false}}, canBeEmpty: true}` + "\n" +
		`  - {field: {name: "Third", localizedName: null, fieldType: {valueType: "string", isMultiValue: false}}, canBeEmpty: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, fieldsQueries(fieldListSent), server.Queries())
}

func TestFieldListRefusesAnOrdinalItCannotOrderBy(t *testing.T) {
	t.Parallel()
	const body = `[{"$type":"EnumProjectCustomField","canBeEmpty":true,"ordinal":1.5,` +
		`"field":{"$type":"CustomField","name":"First","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "field", "list", "DEV")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", fieldsRequest(server.URL, "DEV", fieldListSent)},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}

func TestFieldListRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

	got := runWith(t, server.Env(), "field", "list", "DEV")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", fieldsRequest(server.URL, "DEV", fieldListSent)},
			{"project", "DEV"},
			{"permission", "jetbrains.jetpass.project-read"},
			authFromEnv(),
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}
