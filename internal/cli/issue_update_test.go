package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const issueWriteFields = "idReadable,customFields($type,name,projectCustomField(id))," +
	"project(" + projectWriteFields + ")"

type currentField struct {
	name    string
	kind    string
	binding string
}

func (f currentField) sent() string {
	return `{"$type":` + strconv.Quote(f.kind) + `,"name":` + strconv.Quote(f.name) +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(f.binding) + `}}`
}

func issueToUpdate(readable, project string, held ...currentField) string {
	fields := make([]string, 0, len(held))
	for _, field := range held {
		fields = append(fields, field.sent())
	}
	return `{"$type":"Issue","idReadable":` + strconv.Quote(readable) +
		`,"customFields":[` + strings.Join(fields, ",") + `],"project":` + project + `}`
}

func updating(t *testing.T, read, update http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, readThenUpdate(read, update))
}

func readThenUpdate(read, update http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			update(w, r)
			return
		}
		read(w, r)
	}
}

func updateRequest(address, readable, fields string) string {
	return "POST " + address + "/api/issues/" + readable + "?fields=" + fields
}

func TestIssueUpdatePrintsTheDefaultFieldsOfTheIssue(t *testing.T) {
	t.Parallel()
	server := updating(t, fake.JSON(http.StatusOK, issueToUpdate("DEV-1", projectRequiringNothing())),
		fake.JSON(http.StatusOK, shownIssue()))

	got := runWith(t, server.Env(), "issue", "update", "DEV-1", "--summary", "First")

	assert.Equal(t, outcome{stdout: printedIssueFields}, got)
	assert.Equal(t, askedIssueFields, server.Last(t).URL.Query().Get("fields"))
}

func TestIssueUpdateReadsTheIssueAndWritesByTheIDOfIt(t *testing.T) {
	t.Parallel()
	project := projectResponse(
		writableField{id: "180-1", kind: "StateProjectCustomField", name: "Held", valueType: "state", canBeEmpty: true},
		writableField{id: "180-2", name: "Emptied", valueType: "enum", canBeEmpty: true},
	)
	read := issueToUpdate("DEV-1", project,
		currentField{name: "Held", kind: "StateMachineIssueCustomField", binding: "180-1"})
	held := receivedFields(receivedField{name: "Held", valueType: "state", binding: "180-1", value: bundleElement("First")})
	server := updating(t, fake.JSON(http.StatusOK, read),
		fake.JSON(http.StatusOK, createdIssueWith("DEV-1", "x", `"Second"`, held)))

	got := runWith(t, server.Env(), "issue", "update", "dev-1", "--description", "Second",
		"--field", "Held=First", "--clear", "Emptied", "--fields", "idReadable")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, server.Methods())
	assert.Equal(t, []string{
		"/api/issues/dev-1?fields=" + issueWriteFields,
		"/api/issues/DEV-1?fields=idReadable,description," + customFieldsFields,
	}, server.Targets(t))
	assert.JSONEq(t, `{"description":"Second","customFields":[`+
		`{"$type":"StateMachineIssueCustomField","name":"Held","value":{"name":"First"}},`+
		`{"$type":"SingleEnumIssueCustomField","name":"Emptied","value":null}]}`, server.Last(t).Body)
}

func TestIssueUpdateRefusesAnAnswerThatEmptiedWhatTheWriteFilled(t *testing.T) {
	t.Parallel()
	project := projectResponse(writableField{id: "180-1", kind: "PeriodProjectCustomField", name: "Period",
		valueType: "period", canBeEmpty: true})
	held := receivedFields(receivedField{name: "Period", valueType: "period", binding: "180-1"})
	server := updating(t, fake.JSON(http.StatusOK, issueToUpdate("DEV-1", project)),
		fake.JSON(http.StatusOK, createdIssueWith("DEV-1", "x", "null", held)))

	got := runWith(t, server.Env(), "issue", "update", "DEV-1", "--field", "Period=PT7H", "--fields", "idReadable")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", updateRequest(server.URL, "DEV-1", "idReadable,"+customFieldsFields)},
			{"issue", "DEV-1"},
			{"mismatch", []any{[]detail{{"field", "Period"}, {"expected", "PT7H"}, {"actual", nil}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}

func TestIssueUpdateRefusesTheClassesOfTheIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	project := projectResponse(writableField{id: "180-1", kind: "StateProjectCustomField", name: "Held",
		valueType: "state", canBeEmpty: true})
	read := `{"$type":"Issue","idReadable":"DEV-1","customFields":[{"$type":7,"name":"Held",` +
		`"projectCustomField":{"$type":"ProjectCustomField","id":"180-1"}}],"project":` + project + `}`
	server := updating(t, fake.JSON(http.StatusOK, read), fake.Unexpected(t))

	got := runWith(t, server.Env(), "issue", "update", "DEV-1", "--field", "Held=First")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", issueWriteFields)},
			{"upstream_status", 200},
			{"upstream_body", read},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}
