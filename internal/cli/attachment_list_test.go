package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestAttachmentListPrintsTheAttachmentsOfTheOwner(t *testing.T) {
	t.Parallel()
	const records = `[{"name":"заметка.txt","$type":"IssueAttachment","size":75,"id":"12-2"},` +
		`{"size":0,"id":"12-3","$type":"IssueAttachment","name":"пусто.bin"}]`
	server := fake.Serve(t, fake.JSON(http.StatusOK, records))

	got := runWith(t, envOf(server), "attachment", "list", "DEV-1", "--fields", "id,name,size")

	want := "total: 2\nreturned: 2\ntruncated: false\nattachments:\n" +
		`  - {id: "12-2", name: "заметка.txt", size: 75}` + "\n" +
		`  - {id: "12-3", name: "пусто.bin", size: 0}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), "GET /api/issues/DEV-1/attachments")
}
