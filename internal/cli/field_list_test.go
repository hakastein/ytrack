package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

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
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

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
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

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
	server := serve(t, respondWith(http.StatusOK, shuffledFields))

	got := runWith(t, server.env(), "field", "list", "DEV")

	want := "total: 3\nreturned: 3\ntruncated: false\nfields:\n" +
		`  - {field: {name: "First", localizedName: "Первое", fieldType: {valueType: "enum", isMultiValue: true}}, canBeEmpty: false}` + "\n" +
		`  - {field: {name: "Second", localizedName: null, fieldType: {valueType: "state", isMultiValue: false}}, canBeEmpty: true}` + "\n" +
		`  - {field: {name: "Third", localizedName: null, fieldType: {valueType: "string", isMultiValue: false}}, canBeEmpty: true}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, fieldsQueries(fieldListSent), server.sentQueries())
}

const goSortIsStableUpTo = 12

func unplacedFields() string {
	records := []string{listedField(2, "Placed second")}
	for i := 1; i <= goSortIsStableUpTo; i++ {
		records = append(records, listedField(0, fmt.Sprintf("Unplaced %02d", i)))
	}
	records = append(records, listedField(1, "Placed first"))
	return "[" + strings.Join(records, ",") + "]"
}

func listedField(place int, name string) string {
	return fmt.Sprintf(`{"$type":"SimpleProjectCustomField","ordinal":%d,"canBeEmpty":true,`+
		`"field":{"$type":"CustomField","name":%q,"localizedName":null,`+
		`"fieldType":{"$type":"FieldType","valueType":"string","isMultiValue":false}}}`, place, name)
}

func TestFieldListKeepsTheOrderTheServerSentFieldsOfOneOrdinalIn(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, unplacedFields()))

	got := runWith(t, server.env(), "field", "list", "DEV")

	var lines []string
	for i := 1; i <= 12; i++ {
		lines = append(lines, fmt.Sprintf("Unplaced %02d", i))
	}
	lines = append(lines, "Placed first", "Placed second")
	want := "total: 14\nreturned: 14\ntruncated: false\nfields:\n"
	for _, name := range lines {
		want += `  - {field: {name: "` + name + `", localizedName: null, fieldType: {valueType: "string", isMultiValue: false}}, canBeEmpty: true}` + "\n"
	}
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestFieldListPrintsTheOrdinalOnlyWhenItIsAskedFor(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, shuffledFields))

	got := runWith(t, server.env(), "field", "list", "DEV", "--fields", "field(name),ordinal")

	want := "total: 3\nreturned: 3\ntruncated: false\nfields:\n" +
		`  - {field: {name: "First"}, ordinal: 1}` + "\n" +
		`  - {field: {name: "Second"}, ordinal: 2}` + "\n" +
		`  - {field: {name: "Third"}, ordinal: 3}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, fieldsQueries("field(name),ordinal"), server.sentQueries())
}

func TestFieldListRefusesAnOrdinalItCannotOrderBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ordinal string
	}{
		{name: "a string", ordinal: `"1"`},
		{name: "null", ordinal: "null"},
		{name: "a fraction", ordinal: "1.5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `[{"$type":"EnumProjectCustomField","canBeEmpty":true,"ordinal":` + tc.ordinal +
				`,"field":{"$type":"CustomField","name":"A","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]`
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "field", "list", "DEV")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", fieldsRequest(server.url, "DEV", fieldListSent)},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
}

func TestFieldListRefusesAnOrdinalMissingFromTheResponse(t *testing.T) {
	t.Parallel()
	body := `[{"$type":"EnumProjectCustomField","canBeEmpty":true,` +
		`"field":{"$type":"CustomField","name":"A","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]`
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "field", "list", "DEV")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", fieldsRequest(server.url, "DEV", fieldListSent)},
			{"fields", fieldListSent},
			{"missing", []any{missingEntry("ordinal", "EnumProjectCustomField")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}
