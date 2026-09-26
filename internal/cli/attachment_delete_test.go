package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func attachmentOf(id, name, readable string) string {
	return `{"$type":"IssueAttachment","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `}}`
}

func attachmentOfDEV7() string {
	return attachmentOf("12-5", "a.txt", "DEV-7")
}

func TestAttachmentDeletePrintsTheDeletedAttachment(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, attachmentOfDEV7()), deletionDone())

	got := runWith(t, envOf(server), "attachment", "delete", "DEV-7", "12-5")

	want := `id: "12-5"` + "\n" + `name: "a.txt"` + "\n" + "issue:\n" + `  idReadable: "DEV-7"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "DELETE /api/issues/DEV-7/attachments/12-5")
}
