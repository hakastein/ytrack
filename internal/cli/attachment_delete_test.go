package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func deletedAttachmentFields(owner string) string {
	return "id,name," + owner + "(idReadable)"
}

func attachmentReadRequest(address, owners, owner, id, fields string) string {
	return "GET " + address + "/api/" + owners + "/" + owner + "/attachments/" + id + "?fields=" + fields
}

func attachmentOf(id, name, readable string) string {
	return `{"$type":"IssueAttachment","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `}}`
}

func attachmentOfDEV7() string {
	return attachmentOf("12-5", "a.txt", "DEV-7")
}

func TestAttachmentDeletePrintsWhatTheReadBeforeItFound(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, attachmentOfDEV7()), deletionDone())

	got := runWith(t, server.Env(), "attachment", "delete", "dev-7", "12-5")

	want := `id: "12-5"` + "\n" + `name: "a.txt"` + "\n" + "issue:\n" + `  idReadable: "DEV-7"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, server.Methods())
	assert.Equal(t, []string{
		"/api/issues/dev-7/attachments/12-5?fields=" + deletedAttachmentFields("issue"),
		"/api/issues/DEV-7/attachments/12-5?",
	}, server.Targets(t))
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestAttachmentDeleteRefusesAnAnswerItCannotBeAddressedBy(t *testing.T) {
	t.Parallel()
	read := attachmentOf("12-5", "a.txt", "..")
	server := deleting(t, fake.JSON(http.StatusOK, read), fake.Unexpected(t))

	got := runWith(t, server.Env(), "attachment", "delete", "DEV-7", "12-5")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", attachmentReadRequest(server.URL, "issues", "DEV-7", "12-5", deletedAttachmentFields("issue"))},
			{"upstream_status", 200},
			{"upstream_body", read},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}
