package cli_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const printedEnumType = `field:
  name: "Type"
  localizedName: "Kind"
  fieldType:
    valueType: "enum"
    isMultiValue: false
canBeEmpty: false
bundle:
  values: []
`

func localizedNameOrNull(localized string) string {
	if localized == "" {
		return "null"
	}
	return strconv.Quote(localized)
}

func projectField(id, name, localized string) string {
	return fmt.Sprintf(`{"$type":"EnumProjectCustomField","id":%q,"ordinal":0,"canBeEmpty":false,"field":{"$type":"CustomField","name":%q,`+
		`"localizedName":%s,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}`,
		id, name, localizedNameOrNull(localized))
}

func projectMetadata(fields ...string) string {
	return `{"$type":"Project","customFields":[` + strings.Join(fields, ",") + `]}`
}

func oneField(name, localized string, canBeEmpty bool) string {
	return fmt.Sprintf(`{"$type":"EnumProjectCustomField","field":{"$type":"CustomField","name":%q,`+
		`"localizedName":%s,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}},`+
		`"canBeEmpty":%t,"bundle":{"$type":"EnumBundle","values":[]}}`, name, localizedNameOrNull(localized), canBeEmpty)
}

func serveTheProject(t *testing.T, metadata string, field http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/customFields/") {
			field(w, r)
			return
		}
		fake.JSON(http.StatusOK, metadata)(w, r)
	})
}

func TestFieldShowPrintsTheFieldItsNameResolvesTo(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("180-1", "Type", "Kind"))
	server := serveTheProject(t, metadata, fake.JSON(http.StatusOK, oneField("Type", "Kind", false)))

	got := runWith(t, envOf(server), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: printedEnumType}, got)
	sent := server.Last(t)
	assert.Equal(t, http.MethodGet, sent.Method)
	assert.Equal(t, "/api/admin/projects/DEV/customFields/180-1", sent.URL.Path)
}
