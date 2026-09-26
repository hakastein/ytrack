package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

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

func TestIssueUpdateWritesTheIssue(t *testing.T) {
	t.Parallel()
	project := projectResponse(
		writableField{id: "180-1", kind: "StateProjectCustomField", name: "Held", valueType: "state", canBeEmpty: true},
		writableField{id: "180-2", name: "Emptied", valueType: "enum", canBeEmpty: true},
	)
	read := issueToUpdate("DEV-1", project,
		currentField{name: "Held", kind: "StateMachineIssueCustomField", binding: "180-1"},
		currentField{name: "Emptied", kind: "SingleEnumIssueCustomField", binding: "180-2"})
	held := receivedFields(
		receivedField{name: "Held", valueType: "state", binding: "180-1", value: bundleElement("First")},
		receivedField{name: "Emptied", valueType: "enum", ordinal: "2", binding: "180-2"})
	server := updating(t, fake.JSON(http.StatusOK, read),
		fake.JSON(http.StatusOK, createdIssueWith("DEV-1", "x", `"Second"`, held)))

	got := runWith(t, envOf(server), "issue", "update", "DEV-1", "--description", "Second",
		"--field", "Held=First", "--clear", "Emptied", "--fields", "idReadable")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\n"}, got)
	sent := server.Last(t)
	assert.Equal(t, http.MethodPost, sent.Method)
	assert.Equal(t, "/api/issues/DEV-1", sent.URL.Path)
}
