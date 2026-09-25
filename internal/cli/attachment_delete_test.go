package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	tests := []struct {
		name string
		id   string
	}{
		{name: "the readable id of an issue", id: "DEV-2"},
		{name: "two dots", id: ".."},
		{name: "empty", id: ""},
		{name: "a number and a dash", id: "12-"},
		{name: "a negative number", id: "-1"},
		{name: "a letter after the number", id: "12-2x"},
		{name: "the name of a file", id: "заметка-полигона.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "attachment", "delete", "--", "DEV-1", tc.id)

			refused := requireFault(t, got)
			assert.Equal(t, "bad_usage", refused.code)
			assert.Empty(t, server.Requests())
		})
	}
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

	asked := server.Requests()
	require.Len(t, asked, 2)
	assert.Equal(t, http.MethodGet, asked[0].Method)
	assert.Equal(t, "/api/issues/dev-7/attachments/12-5", asked[0].URL.Path)
	assert.Equal(t, deletedAttachmentFields("issue"), server.Fields()[0])
	assert.Equal(t, http.MethodDelete, asked[1].Method)
	assert.Equal(t, "/api/issues/DEV-7/attachments/12-5", asked[1].URL.Path)
	assert.Empty(t, asked[1].URL.RawQuery)
	assert.Empty(t, server.Bodies()[1])
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
	tests := []struct {
		name     string
		read     http.HandlerFunc
		deletion func(t *testing.T) http.HandlerFunc
		exit     int
		methods  []string
	}{
		{
			name:     "another attachment than the one asked for",
			read:     fake.JSON(http.StatusOK, attachmentOf("12-6", "a.txt", "DEV-7")),
			deletion: noDeletion,
			exit:     1,
			methods:  []string{http.MethodGet},
		},
		{
			name:     "an owner no request can be addressed by",
			read:     fake.JSON(http.StatusOK, attachmentOf("12-5", "a.txt", "..")),
			deletion: noDeletion,
			exit:     1,
			methods:  []string{http.MethodGet},
		},
		{
			name:     "an owner that arrived as no object at all",
			read:     fake.JSON(http.StatusOK, `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":null}`),
			deletion: noDeletion,
			exit:     1,
			methods:  []string{http.MethodGet},
		},
		{
			name:     "a deletion answered with a body",
			read:     fake.JSON(http.StatusOK, attachmentOfDEV7()),
			deletion: func(*testing.T) http.HandlerFunc { return fake.JSON(http.StatusOK, `{"x":1}`) },
			exit:     2,
			methods:  []string{http.MethodGet, http.MethodDelete},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := deleting(t, tc.read, tc.deletion(t))

			got := runWith(t, server.Env(), "attachment", "delete", "DEV-7", "12-5")

			refused := requireFaultDocument(t, got)
			assert.Equal(t, "upstream_invalid", refused.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestAttachmentDeleteTakesAFileOffAnArticleThroughItsOwnAPI(t *testing.T) {
	t.Parallel()
	const answered = `{"$type":"ArticleAttachment","id":"522-4","name":"кот.png",` +
		`"article":{"$type":"Article","idReadable":"DEV-A-7"}}`
	server := deleting(t, fake.JSON(http.StatusOK, answered), deletionDone())

	got := runWith(t, server.Env(), "attachment", "delete", "DEV-A-7", "522-4")

	want := `id: "522-4"` + "\n" + `name: "кот.png"` + "\n" + "article:\n" + `  idReadable: "DEV-A-7"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/articles/DEV-A-7/attachments/522-4", "/api/articles/DEV-A-7/attachments/522-4"},
		server.Paths())
	assert.Equal(t, deletedAttachmentFields("article"), server.Fields()[0])
	assert.NotContains(t, strings.Join(server.Paths(), " "), "/api/issues")
}
