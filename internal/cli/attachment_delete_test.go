package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestAttachmentDeleteRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: nil},
		{name: "an owner alone", argv: []string{"DEV-1"}},
		{name: "a third argument", argv: []string{"DEV-1", "12-2", "12-3"}},
		{name: "a flag that would confirm the deletion", argv: []string{"DEV-1", "12-2", "--yes"}},
		{name: "a flag that would force it", argv: []string{"DEV-1", "12-2", "--force"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"attachment", "delete"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
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
			server := serveNothing(t)

			got := runWith(t, server.env(), "attachment", "delete", "--", "DEV-1", tc.id)

			refused := requireFault(t, got)
			assert.Equal(t, "bad_usage", refused.code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestAttachmentDeleteRefusesAnInternalIDForItsOwner(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "attachment", "delete", "3-19", "12-2")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestAttachmentDeleteHelpAsksNothingAndNamesWhereTheIDComesFrom(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"attachment", "delete", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	for _, flag := range []string{"--yes", "--force", "--confirm"} {
		assert.NotContains(t, got.stdout, flag)
	}
	assert.Contains(t, got.stdout, "ytrack attachment list")
}

func TestAttachmentDeletePrintsWhatTheReadBeforeItFound(t *testing.T) {
	t.Parallel()
	server := deleting(t, respondWith(http.StatusOK, attachmentOfDEV7()), deletionDone())

	got := runWith(t, server.env(), "attachment", "delete", "dev-7", "12-5")

	want := `id: "12-5"` + "\n" + `name: "a.txt"` + "\n" + "issue:\n" + `  idReadable: "DEV-7"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)

	asked := server.requests()
	require.Len(t, asked, 2)
	assert.Equal(t, http.MethodGet, asked[0].Method)
	assert.Equal(t, "/api/issues/dev-7/attachments/12-5", asked[0].URL.Path)
	assert.Equal(t, deletedAttachmentFields("issue"), server.sentFields()[0])
	assert.Equal(t, http.MethodDelete, asked[1].Method)
	assert.Equal(t, "/api/issues/DEV-7/attachments/12-5", asked[1].URL.Path)
	assert.Empty(t, asked[1].URL.RawQuery)
	assert.Empty(t, server.asks()[1])
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
			read:     respondWith(http.StatusNotFound, entityNotFound("12-5")),
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
			read:     respondWith(http.StatusOK, attachmentOfDEV7()),
			deletion: func(*testing.T) http.HandlerFunc { return respondWith(http.StatusNotFound, entityNotFound("12-5")) },
			code:     "not_found",
			exit:     1,
			request:  func(address string) string { return attachmentDeletionRequest(address, "issues", "DEV-7", "12-5") },
			methods:  []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "a token that may read the attachment and not take it away",
			read: respondWith(http.StatusOK, attachmentOfDEV7()),
			deletion: func(*testing.T) http.HandlerFunc {
				said := `{"error":"Forbidden","error_description":"Insufficient rights"}`
				return respondWith(http.StatusForbidden, said)
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

			got := runWith(t, server.env(), "attachment", "delete", "DEV-7", "12-5")

			refused := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, refused.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tc.request(server.url)}, refused.details[0])
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
			read:     respondWith(http.StatusOK, attachmentOf("12-6", "a.txt", "DEV-7")),
			deletion: noDeletion,
			exit:     1,
			methods:  []string{http.MethodGet},
		},
		{
			name:     "an owner no request can be addressed by",
			read:     respondWith(http.StatusOK, attachmentOf("12-5", "a.txt", "..")),
			deletion: noDeletion,
			exit:     1,
			methods:  []string{http.MethodGet},
		},
		{
			name:     "an owner that arrived as no object at all",
			read:     respondWith(http.StatusOK, `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":null}`),
			deletion: noDeletion,
			exit:     1,
			methods:  []string{http.MethodGet},
		},
		{
			name:     "a deletion answered with a body",
			read:     respondWith(http.StatusOK, attachmentOfDEV7()),
			deletion: func(*testing.T) http.HandlerFunc { return respondWith(http.StatusOK, `{"x":1}`) },
			exit:     2,
			methods:  []string{http.MethodGet, http.MethodDelete},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := deleting(t, tc.read, tc.deletion(t))

			got := runWith(t, server.env(), "attachment", "delete", "DEV-7", "12-5")

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
	server := deleting(t, respondWith(http.StatusOK, answered), deletionDone())

	got := runWith(t, server.env(), "attachment", "delete", "DEV-A-7", "522-4")

	want := `id: "522-4"` + "\n" + `name: "кот.png"` + "\n" + "article:\n" + `  idReadable: "DEV-A-7"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/articles/DEV-A-7/attachments/522-4", "/api/articles/DEV-A-7/attachments/522-4"},
		server.sentPaths())
	assert.Equal(t, deletedAttachmentFields("article"), server.sentFields()[0])
	assert.NotContains(t, strings.Join(server.sentPaths(), " "), "/api/issues")
}

func TestAttachmentDeleteTakesFilesOffThePolygon(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	first := anIssueOfItsOwn(t, dev, "first")
	second := anIssueOfItsOwn(t, dev, "second")
	article := attachedArticle(t, dev)
	onTheIssue := attachedTo(t, dev, first, "заметка.txt", []byte("ytrack"))
	onTheArticle := attachedTo(t, dev, article, "вложение статьи.bin", everyLatin1RuneAsUTF8(1))

	t.Run("an attachment of another issue", func(t *testing.T) {
		before := len(dev.requests())

		got := runWith(t, dev.env(), "attachment", "delete", second, onTheIssue.ID)

		assert.Equal(t, "not_found", requireFault(t, got).code)
		assert.Len(t, dev.requests()[before:], 1)
	})
	t.Run("an attachment of an issue named under an article", func(t *testing.T) {
		got := runWith(t, dev.env(), "attachment", "delete", article, onTheIssue.ID)

		assert.Equal(t, "not_found", requireFault(t, got).code)
	})
	t.Run("an attachment of an article named under an issue", func(t *testing.T) {
		got := runWith(t, dev.env(), "attachment", "delete", first, onTheArticle.ID)

		assert.Equal(t, "not_found", requireFault(t, got).code)
	})
	t.Run("the file is still there after every refusal", func(t *testing.T) {
		listed := requireAttachmentListing(t, runWith(t, dev.env(), "attachment", "list", first))
		assert.Equal(t, 1, listed.Total)
	})
	t.Run("a token the issue is not answered for", func(t *testing.T) {
		limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}

		got := runWith(t, limited, "attachment", "delete", first, onTheIssue.ID)

		assert.Equal(t, "not_found", requireFault(t, got).code)
	})
	t.Run("the owner written in lower case", func(t *testing.T) {
		got := runWith(t, dev.env(), "attachment", "delete", strings.ToLower(first), onTheIssue.ID)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		want := "id: " + strconv.Quote(onTheIssue.ID) + "\nname: \"заметка.txt\"\nissue:\n" +
			"  idReadable: " + strconv.Quote(first) + "\n"
		assert.Equal(t, want, got.stdout)
	})
	t.Run("the link printed for it before", func(t *testing.T) {
		response, body := fetched(t, onTheIssue.URL, "")

		assert.Equal(t, http.StatusNotFound, response.StatusCode)
		assert.Contains(t, string(body), "File with id "+onTheIssue.ID+" not found")
	})
	t.Run("the same deletion again", func(t *testing.T) {
		before := len(dev.requests())

		got := runWith(t, dev.env(), "attachment", "delete", first, onTheIssue.ID)

		assert.Equal(t, "not_found", requireFault(t, got).code)
		assert.Len(t, dev.requests()[before:], 1)
	})
	t.Run("an attachment of an article", func(t *testing.T) {
		got := runWith(t, dev.env(), "attachment", "delete", article, onTheArticle.ID)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		want := "id: " + strconv.Quote(onTheArticle.ID) + "\nname: " + strconv.Quote(onTheArticle.Name) + "\narticle:\n" +
			"  idReadable: " + strconv.Quote(article) + "\n"
		assert.Equal(t, want, got.stdout)
	})
}

func anIssueOfItsOwn(t *testing.T, dev *upstream, called string) string {
	t.Helper()
	summary := "ytrack contract " + t.Name() + " " + called
	argv := append([]string{"issue", "create", "DEV", "--summary", summary}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

func attachedTo(t *testing.T, dev *upstream, owner, name string, content []byte) printedAttachment {
	t.Helper()
	got := runWith(t, dev.env(), "attachment", "create", owner, fileWith(t, name, content))
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	return requireAttachmentPrinted(t, got.stdout)
}
