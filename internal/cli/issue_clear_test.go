package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestIssueUpdateEmptiesTheDescriptionUnderClear(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		clear string
	}{
		{name: "in lower case", clear: "description"},
		{name: "in another letter case", clear: "Description"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := issueToUpdate("DEV-1", projectRequiringNothing(),
				currentField{name: "Optional", kind: "SingleEnumIssueCustomField", binding: "180-1"})
			held := receivedFields(receivedField{name: "Optional", valueType: "enum", binding: "180-1"})
			server := updating(t, fake.JSON(http.StatusOK, read),
				fake.JSON(http.StatusOK, createdIssueWith("DEV-1", "x", "null", held)))

			got := runWith(t, envOf(server), "issue", "update", "DEV-1", "--clear", tc.clear, "--fields", "idReadable")

			assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\n"}, got)
			sent := server.Last(t)
			assert.Equal(t, http.MethodPost, sent.Method)
			assert.Equal(t, "/api/issues/DEV-1", sent.URL.Path)
		})
	}
}
