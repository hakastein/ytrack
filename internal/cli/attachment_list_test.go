package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const attachmentFields = "id,name,size,mimeType,url"

func attachmentsRequest(address, owners, id, fields, top string) string {
	return "GET " + address + "/api/" + owners + "/" + id + "/attachments?fields=" + fields + "&$top=" + top
}

func TestAttachmentListRefusesTheContentOfAFile(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "attachment", "list", "DEV-1", "--fields", "+base64Content")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestAttachmentListPrintsTheRecordsAsTheyWereAskedFor(t *testing.T) {
	t.Parallel()
	const records = `[{"name":"заметка.txt","$type":"IssueAttachment","size":75,` +
		`"url":"/api/files/12-2?sign=Ab-_9&updated=1","mimeType":"text/plain","id":"12-2"},` +
		`{"size":0,"id":"12-3","mimeType":"application/octet-stream","$type":"IssueAttachment",` +
		`"name":"пусто.bin","url":"/api/files/12-3?sign=x&updated=2"}]`
	server := fake.Serve(t, fake.JSON(http.StatusOK, records))

	got := runWith(t, server.Env(), "attachment", "list", "DEV-1")

	want := "total: 2\nreturned: 2\ntruncated: false\nattachments:\n" +
		`  - {id: "12-2", name: "заметка.txt", size: 75, mimeType: "text/plain", url: "` +
		server.Origin + `/api/files/12-2?sign=Ab-_9&updated=1"}` + "\n" +
		`  - {id: "12-3", name: "пусто.bin", size: 0, mimeType: "application/octet-stream", url: "` +
		server.Origin + `/api/files/12-3?sign=x&updated=2"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/issues/DEV-1/attachments"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {attachmentFields}, "$top": {"50"}}}, server.Queries())
}

func TestAttachmentListRefusesALinkThatIsNoAbsolutePath(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK,
		`[{"id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain","url":"api/files/12-2"}]`))

	got := runWith(t, server.Env(), "attachment", "list", "DEV-1")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", attachmentsRequest(server.URL, "issues", "DEV-1", attachmentFields, "50")},
			{"field", "url"},
			{"upstream_value", "api/files/12-2"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}
