package cli_test

import (
	"cmp"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

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

func projectResponse(fields ...writableField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + strings.Join(sent, ",") + `]}`
}

func projectRequiringNothing() string {
	return projectResponse(writableField{id: "180-1", name: "Optional", valueType: "enum", canBeEmpty: true})
}

func createdIssueWith(readable, summary, descriptionJSON, fields string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `,"summary":` + asJSON(summary) +
		`,"customFields":` + fields + `,"description":` + descriptionJSON + `}`
}

func asJSON(text string) string {
	written, _ := json.Marshal(text)
	return string(written)
}

func creating(t *testing.T, metadata, creation http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creation(w, r)
			return
		}
		metadata(w, r)
	})
}

func TestIssueCreateFilesTheIssue(t *testing.T) {
	t.Parallel()
	metadata := projectResponse(writableField{id: "180-1", kind: "SimpleProjectCustomField", name: "Field",
		valueType: "string", canBeEmpty: true})
	held := receivedFields(receivedField{name: "Field", valueType: "string", binding: "180-1", value: `"Third"`})
	server := creating(t, fake.JSON(http.StatusOK, metadata),
		fake.JSON(http.StatusOK, createdIssueWith("DEV-7", "First", `"Second\nline"`, held)))

	got := runWith(t, envOf(server), "issue", "create", "DEV", "--summary", "First", "--description", "Second\nline",
		"--field", "Field=Third", "--fields", "idReadable,description")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\ndescription: |-\n  Second\n  line\n"}, got)
	sent := server.Last(t)
	assert.Equal(t, http.MethodPost, sent.Method)
	assert.Equal(t, "/api/issues", sent.URL.Path)
}

func TestIssueCreateHangsTheTagsInTheWriteThatFilesTheIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-7","summary":"First","tags":[`+catalogueTag("10-5", "Ready", "first")+
				`,`+catalogueTag("10-8", "Done", "second")+`]}`)(w, r)
		case r.URL.Path == tagsCollection:
			fake.JSON(http.StatusOK, "["+catalogueTag("10-5", "Ready", "first")+","+catalogueTag("10-8", "Done", "second")+"]")(w, r)
		case r.URL.Path == starProfile:
			noStarOfTheTags()(w, r)
		default:
			fake.JSON(http.StatusOK, projectRequiringNothing())(w, r)
		}
	})

	got := runWith(t, envOf(server), "issue", "create", "DEV", "--summary", "First", "--tag", "done", "--tag", "Ready",
		"--fields", "idReadable")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\n"}, got)
	assert.Equal(t, "/api/issues", server.Last(t).URL.Path)
	assert.Contains(t, server.Last(t).Body, `"tags":[{"id":"10-8"},{"id":"10-5"}]`)
}

func starredIssue(star string) string {
	return `{"$type":"Issue","idReadable":"DEV-7","summary":"First","watchers":{"$type":"IssueWatchers","hasStar":` + star + `}}`
}

func TestIssueCreateTakesOffTheStarTheProfilePutOn(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			fake.JSON(http.StatusOK, starredIssue("true"))(w, r)
		case r.Method == http.MethodPost:
			fake.JSON(http.StatusOK, starredIssue("false"))(w, r)
		default:
			fake.JSON(http.StatusOK, projectRequiringNothing())(w, r)
		}
	})

	got := runWith(t, envOf(server), "issue", "create", "DEV", "--summary", "First", "--star=false", "--fields", "idReadable")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\n"}, got)
	assert.Equal(t, "/api/issues/DEV-7", server.Last(t).URL.Path)
	assert.JSONEq(t, `{"watchers":{"hasStar":false}}`, server.Last(t).Body)
}

func TestIssueCreateLeavesTheStarToTheProfileWithoutTheFlag(t *testing.T) {
	t.Parallel()
	server := creating(t, fake.JSON(http.StatusOK, projectRequiringNothing()),
		fake.JSON(http.StatusOK, starredIssue("true")))

	got := runWith(t, envOf(server), "issue", "create", "DEV", "--summary", "First", "--fields", "idReadable")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-7\"\n"}, got)
	assert.NotContains(t, server.Last(t).Body, "watchers")
}
