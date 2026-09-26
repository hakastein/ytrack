package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const listedFirstField = `[{"$type":"EnumProjectCustomField","ordinal":1,"canBeEmpty":false,` +
	`"field":{"$type":"CustomField","name":"First","localizedName":null,` +
	`"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":true}}}]`

func TestFieldListPrintsTheFieldsOfTheProject(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, listedFirstField))

	got := runWith(t, envOf(server), "field", "list", "DEV")

	want := "total: 1\nreturned: 1\ntruncated: false\nfields:\n" +
		`  - {field: {name: "First", localizedName: null, fieldType: {valueType: "enum", isMultiValue: true}}, canBeEmpty: false}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	sent := server.Last(t)
	assert.Equal(t, http.MethodGet, sent.Method)
	assert.Equal(t, "/api/admin/projects/DEV/customFields", sent.URL.Path)
}
