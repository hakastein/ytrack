package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const fieldListDefault = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"

const fieldListSent = fieldListDefault + ",ordinal"

func fieldsRequest(address, project, fields string) string {
	return "GET " + address + "/api/admin/projects/" + project + "/customFields?fields=" + fields + "&$top=-1"
}

func fieldsQueries(fields string) []url.Values {
	return []url.Values{{"fields": {fields}, "$top": {"-1"}}}
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

	got := runWith(t, server.Env(), "field", "list", "DEV", "--fields", "field(name)")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", fieldsRequest(server.URL, "DEV", "field(name),ordinal")},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}

func TestFieldListRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

	got := runWith(t, server.Env(), "field", "list", "DEV", "--fields", "field(name)")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", fieldsRequest(server.URL, "DEV", "field(name),ordinal")},
			{"project", "DEV"},
			{"permission", "jetbrains.jetpass.project-read"},
			authFromEnv(),
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}
