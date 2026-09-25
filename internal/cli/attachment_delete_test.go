package cli_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func deletedAttachmentFields(owner string) string {
	return "id,name," + owner + "(idReadable)"
}

func attachmentReadRequest(address, owners, owner, id, fields string) string {
	return "GET " + address + "/api/" + owners + "/" + owner + "/attachments/" + id + "?fields=" + fields
}

func attachmentDeletionRequest(address, owners, owner, id string) string {
	return "DELETE " + address + "/api/" + owners + "/" + owner + "/attachments/" + id
}

func attachmentOf(id, name, readable string) string {
	return `{"$type":"IssueAttachment","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `}}`
}

func attachmentOfDEV7() string {
	return attachmentOf("12-5", "a.txt", "DEV-7")
}

func TestAttachmentDeleteRefusesAnIDThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "attachment", "delete", "DEV-1", "..")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestAttachmentDeleteRefusesAnInternalIDForItsOwner(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "attachment", "delete", "3-19", "12-2")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestAttachmentDeletePrintsWhatTheReadBeforeItFound(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, attachmentOfDEV7()), deletionDone())

	got := runWith(t, server.Env(), "attachment", "delete", "dev-7", "12-5")

	want := `id: "12-5"` + "\n" + `name: "a.txt"` + "\n" + "issue:\n" + `  idReadable: "DEV-7"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		"/api/issues/dev-7/attachments/12-5?fields=" + deletedAttachmentFields("issue"),
		"/api/issues/DEV-7/attachments/12-5?",
	}, server.Targets())
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestAttachmentDeleteReadsTheStatusOfEachRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		read     http.HandlerFunc
		deletion func(t *testing.T) http.HandlerFunc
		code     string
		exit     int
		request  func(address string) string
		methods  []string
	}{
		{
			name:     "an attachment the owner has none of",
			read:     fake.JSON(http.StatusNotFound, entityNotFound("12-5")),
			deletion: noDeletion,
			code:     "not_found",
			exit:     1,
			request: func(address string) string {
				return attachmentReadRequest(address, "issues", "DEV-7", "12-5", deletedAttachmentFields("issue"))
			},
			methods: []string{http.MethodGet},
		},
		{
			name:     "an attachment taken away between the read and the deletion",
			read:     fake.JSON(http.StatusOK, attachmentOfDEV7()),
			deletion: func(*testing.T) http.HandlerFunc { return fake.JSON(http.StatusNotFound, entityNotFound("12-5")) },
			code:     "not_found",
			exit:     1,
			request:  func(address string) string { return attachmentDeletionRequest(address, "issues", "DEV-7", "12-5") },
			methods:  []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "a token that may read the attachment and not take it away",
			read: fake.JSON(http.StatusOK, attachmentOfDEV7()),
			deletion: func(*testing.T) http.HandlerFunc {
				said := `{"error":"Forbidden","error_description":"Insufficient rights"}`
				return fake.JSON(http.StatusForbidden, said)
			},
			code:    "denied",
			exit:    1,
			request: func(address string) string { return attachmentDeletionRequest(address, "issues", "DEV-7", "12-5") },
			methods: []string{http.MethodGet, http.MethodDelete},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := deleting(t, tc.read, tc.deletion(t))

			got := runWith(t, server.Env(), "attachment", "delete", "DEV-7", "12-5")

			refused := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, refused.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tc.request(server.URL)}, refused.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestAttachmentDeleteRefusesAnAnswerItCannotBeAddressedBy(t *testing.T) {
	t.Parallel()
	read := attachmentOf("12-5", "a.txt", "..")
	server := deleting(t, fake.JSON(http.StatusOK, read), noDeletion(t))

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
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestAttachmentDeleteExitsWith2WhereTheDeletionIsAnsweredWithABody(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, attachmentOfDEV7()), fake.JSON(http.StatusOK, `{"x":1}`))

	got := runWith(t, server.Env(), "attachment", "delete", "DEV-7", "12-5")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", attachmentDeletionRequest(server.URL, "issues", "DEV-7", "12-5")},
			{"upstream_status", 200},
			{"upstream_body", `{"x":1}`},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
}
