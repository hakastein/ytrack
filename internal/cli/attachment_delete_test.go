package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expression the read before a deletion sends, one to a kind of owner: the id it was asked by, the name
// the document prints, and the owner the deletion is addressed by.
func deletedAttachmentFields(owner string) string {
	return "id,name," + owner + "(idReadable)"
}

// The two requests a deletion sends, as a refusal names them.
func attachmentReadRequest(address, owners, owner, id, fields string) string {
	return "GET " + address + "/api/" + owners + "/" + owner + "/attachments/" + id + "?fields=" + fields
}

func attachmentDeletionRequest(address, owners, owner, id string) string {
	return "DELETE " + address + "/api/" + owners + "/" + owner + "/attachments/" + id
}

// attachmentOf is what the read before a deletion is answered with: the attachment as it stands, under the
// issue the server says it hangs from, which is not always the owner the caller wrote the call with.
func attachmentOf(id, name, readable string) string {
	return `{"$type":"IssueAttachment","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) +
		`,"issue":{"$type":"Issue","idReadable":` + strconv.Quote(readable) + `}}`
}

func attachmentOfDEV7() string {
	return attachmentOf("12-5", "a.txt", "DEV-7")
}

// A deletion names an owner and an attachment, and nothing else: a call of any other shape is refused
// before a file is opened, a request is built or anything is destroyed.
func TestAttachmentDeleteRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: nil},
		{name: "an owner alone", argv: []string{"DEV-1"}},
		{name: "a third argument", argv: []string{"DEV-1", "12-2", "12-3"}},
		// Nothing is asked before the file goes, so there is no flag that answers.
		{name: "a flag that would confirm the deletion", argv: []string{"DEV-1", "12-2", "--yes"}},
		{name: "a flag that would force it", argv: []string{"DEV-1", "12-2", "--force"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"attachment", "delete"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// An attachment is addressed by the internal id and by nothing else. A file name is the form worth naming among
// these: an issue can carry the same name many times over, so a name addresses nothing even where it looks like
// it should.
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

			refused := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", refused.code)
			assert.Empty(t, server.requests())
		})
	}
}

// An internal id addresses the owner of nothing: a deletion names the entity the attachment hangs from by
// the readable id, as every command that names one does.
func TestAttachmentDeleteRefusesAnInternalIDForItsOwner(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "attachment", "delete", "3-19", "12-2")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// The help says what the command costs: nothing is asked before the file goes, so there is no flag that
// answers, and the id it takes is the one the list prints.
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

// The whole of the call on the wire: one read of the attachment under the owner the caller wrote, then the
// deletion under the owner the server named, carrying no body and no query at all. What is printed is what the
// read answered — the argument was never checked, and dev-7 and DEV-7 reach the same issue.
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

// Where the read answers and where the deletion does, each status is read the way it is read everywhere,
// and a read that finds nothing costs the caller no destructive request at all.
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

			refused := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, refused.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tc.request(server.url)}, refused.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// What the read answered is what the deletion is addressed by and what the document prints, so an answer
// about anything but the attachment that was asked for stops the call where it stands: printing it would print
// the unchecked, and destroying under it would destroy what nobody named. A body under the deletion is the
// answer of something other than the endpoint asked, and by then the file is gone.
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

			refused := requireRefusalDocument(t, got)
			assert.Equal(t, "upstream_invalid", refused.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The knowledge base is reached through its own API, and the owner the read is asked for stands under a
// name of its own there: an attachment of an article carries article where one of an issue carries issue.
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
	// Not a picture: the bytes of one are no valid UTF-8, and go-vcr would write the upload into the cassette
	// as base64. What a preview looks like live is held by the creation.
	onTheArticle := attachedTo(t, dev, article, "вложение статьи.bin", everyLatinRune(1))

	t.Run("an attachment of another issue", func(t *testing.T) {
		before := len(dev.requests())

		got := runWith(t, dev.env(), "attachment", "delete", second, onTheIssue.ID)

		assert.Equal(t, "not_found", requireRefusal(t, got).code)
		assert.Len(t, dev.requests()[before:], 1)
	})
	t.Run("an attachment of an issue named under an article", func(t *testing.T) {
		got := runWith(t, dev.env(), "attachment", "delete", article, onTheIssue.ID)

		assert.Equal(t, "not_found", requireRefusal(t, got).code)
	})
	t.Run("an attachment of an article named under an issue", func(t *testing.T) {
		got := runWith(t, dev.env(), "attachment", "delete", first, onTheArticle.ID)

		assert.Equal(t, "not_found", requireRefusal(t, got).code)
	})
	t.Run("the file is still there after every refusal", func(t *testing.T) {
		listed := requireAttachmentListing(t, runWith(t, dev.env(), "attachment", "list", first))
		assert.Equal(t, 1, listed.Total)
	})
	t.Run("a token the issue is not answered for", func(t *testing.T) {
		limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}

		got := runWith(t, limited, "attachment", "delete", first, onTheIssue.ID)

		assert.Equal(t, "not_found", requireRefusal(t, got).code)
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

		assert.Equal(t, "not_found", requireRefusal(t, got).code)
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

// anIssueOfItsOwn is one of the issues this scenario needs two of, told apart in the project by called; both
// are filed by the command that files issues and taken away with everything hanging from them.
func anIssueOfItsOwn(t *testing.T, dev *upstream, called string) string {
	t.Helper()
	summary := "ytrack contract " + t.Name() + " " + called
	argv := append([]string{"issue", "create", "DEV", "--summary", summary}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

// attachedTo is the file a contract test about deletions works on: attached by the command that attaches
// files, so the id and the signed link it is held to afterwards are the ones a caller would have.
func attachedTo(t *testing.T, dev *upstream, owner, name string, content []byte) printedAttachment {
	t.Helper()
	got := runWith(t, dev.env(), "attachment", "create", owner, fileWith(t, name, content))
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	return requireAttachmentPrinted(t, got.stdout)
}
