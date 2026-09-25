package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The expression the list sends where the caller writes none.
const attachmentFields = "id,name,size,mimeType,url"

const (
	devInstanceAttachmentName = "заметка-полигона.txt"
	devInstanceAttachmentSize = 75
)

// The form of the internal id of an attachment, which is what a scenario against live data holds the id to:
// the class the instance numbers its attachments with is its own.
const attachmentIDForm = `^[0-9]+-[0-9]+$`

// attachmentsRequest is the request the list sends, as a refusal names it.
func attachmentsRequest(address, owners, id, fields, top string) string {
	return "GET " + address + "/api/" + owners + "/" + id + "/attachments?fields=" + fields + "&$top=" + top
}

// The queries of a list that fills its limit and is therefore counted: the page, then the pass over ids alone.
func countingAttachments(limit string) []url.Values {
	return []url.Values{
		{"fields": {attachmentFields}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
}

// attachmentListing is the document attachment list prints, read back.
type attachmentListing struct {
	Total       int                 `yaml:"total"`
	Returned    int                 `yaml:"returned"`
	Truncated   bool                `yaml:"truncated"`
	Attachments []printedAttachment `yaml:"attachments"`
}

func requireAttachmentListing(t *testing.T, got outcome) attachmentListing {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	decoder := yaml.NewDecoder(strings.NewReader(got.stdout))
	decoder.KnownFields(true)
	var printed attachmentListing
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", got.stdout)
	assert.Len(t, printed.Attachments, printed.Returned)
	assert.Equal(t, printed.Total > printed.Returned, printed.Truncated)
	return printed
}

// What the group offers and what it does not, settled before the network: a verb that would fetch a file
// is an unknown command like any other, and the one verb built takes exactly one owner.
func TestAttachmentRefusesACallThatNamesNoCommandOfIts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the group alone", argv: []string{"attachment"}},
		{name: "a verb it has none of", argv: []string{"attachment", "bogus"}},
		{name: "a download", argv: []string{"attachment", "download", "DEV-1", "12-2"}},
		{name: "a show of one attachment", argv: []string{"attachment", "show", "DEV-1", "12-2"}},
		{name: "a list of no owner", argv: []string{"attachment", "list"}},
		{name: "a list of two owners", argv: []string{"attachment", "list", "DEV-1", "DEV-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The block a caller reads the group by names what ytrack builds and nothing else: a download stands
// nowhere in it. The writes of the group join the block in the steps that build them.
func TestAttachmentHelpNamesItsVerbsAndNoDownload(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"attachment", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{"create", "delete", "list"}, availableCommands(t, got.stdout))
}

// The help of the list says what a caller gets unasked, so the default expression is read off it rather
// than off this repository.
func TestAttachmentListHelpNamesTheDefaultFields(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"attachment", "list", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, attachmentFields)
}

// The help documents an owner of either kind and the expression that brings a file's comment along.
func TestAttachmentListHelpNamesOwnerExamplesAndTheCommentExpression(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"attachment", "list", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	for _, said := range []string{"DEV-1", "DEV-A-1", "+comment(id)"} {
		assert.Contains(t, got.stdout, said)
	}
}

// The limit reaches the server as $top, an int32, so a number it could not carry is refused before any
// request; the flag says it once.
func TestAttachmentListRefusesALimitItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "zero", argv: []string{"--limit", "0"}},
		{name: "a negative number", argv: []string{"--limit", "-1"}},
		{name: "past the largest int32", argv: []string{"--limit", "2147483648"}},
		{name: "given twice", argv: []string{"--limit", "1", "--limit", "2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"attachment", "list", "DEV-1"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The list is the one command an attachment is read by, and the file itself is no field of it either.
func TestAttachmentListRefusesTheContentOfAFile(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "attachment", "list", "DEV-1", "--fields", "+base64Content")

	found := requireRefusal(t, got)
	assert.Equal(t, "bad_usage", found.code)
	assert.Empty(t, server.requests())
}

// A record holds what was asked of it, in the order it was asked, whatever order the server sent: $type is
// the server's own and stands nowhere, a size is a number and the link is whole. The request carries the limit
// as $top and the default expression as fields.
func TestAttachmentListPrintsTheRecordsAsTheyWereAskedFor(t *testing.T) {
	t.Parallel()
	const records = `[{"name":"заметка.txt","$type":"IssueAttachment","size":75,` +
		`"url":"/api/files/12-2?sign=Ab-_9&updated=1","mimeType":"text/plain","id":"12-2"},` +
		`{"size":0,"id":"12-3","mimeType":"application/octet-stream","$type":"IssueAttachment",` +
		`"name":"пусто.bin","url":"/api/files/12-3?sign=x&updated=2"}]`
	server := serve(t, respondWith(http.StatusOK, records))

	got := runWith(t, server.env(), "attachment", "list", "DEV-1")

	want := "total: 2\nreturned: 2\ntruncated: false\nattachments:\n" +
		`  - {id: "12-2", name: "заметка.txt", size: 75, mimeType: "text/plain", url: "` +
		server.url + `/api/files/12-2?sign=Ab-_9&updated=1"}` + "\n" +
		`  - {id: "12-3", name: "пусто.bin", size: 0, mimeType: "application/octet-stream", url: "` +
		server.url + `/api/files/12-3?sign=x&updated=2"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/issues/DEV-1/attachments"}, server.sentPaths())
	assert.Equal(t, []url.Values{{"fields": {attachmentFields}, "$top": {"50"}}}, server.sentQueries())
}

// The limit goes out on every call, whatever it is: without $top the server stops at 42 attachments of the
// owner and says nothing of the rest.
func TestAttachmentListSendsTheLimitAsTop(t *testing.T) {
	t.Parallel()
	for _, limit := range []string{"1", "2147483647"} {
		t.Run(limit, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, `[]`))

			got := runWith(t, server.env(), "attachment", "list", "DEV-1", "--limit", limit)

			assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nattachments: []\n"}, got)
			assert.Equal(t, []url.Values{{"fields": {attachmentFields}, "$top": {limit}}}, server.sentQueries())
		})
	}
}

// The document is the same shape whatever came back: an owner with no attachments prints the key empty,
// and one attachment prints a list of one rather than a record of its own (5.8).
func TestAttachmentListPrintsTheSameShapeForAnyNumberOfRecords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want func(address string) string
	}{
		{
			name: "none at all",
			body: `[]`,
			want: func(string) string { return "total: 0\nreturned: 0\ntruncated: false\nattachments: []\n" },
		},
		{
			name: "one",
			body: `[{"$type":"IssueAttachment","id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain",` +
				`"url":"/api/files/12-2?sign=s&updated=1"}]`,
			want: func(address string) string {
				return "total: 1\nreturned: 1\ntruncated: false\nattachments:\n" +
					`  - {id: "12-2", name: "a.txt", size: 1, mimeType: "text/plain", url: "` +
					address + `/api/files/12-2?sign=s&updated=1"}` + "\n"
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, respondWith(http.StatusOK, tc.body))

			got := runWith(t, server.env(), "attachment", "list", "DEV-1")

			assert.Equal(t, outcome{stdout: tc.want(server.url)}, got)
		})
	}
}

// A page that fills the limit proves nothing about the rest, so the whole is read off a second pass over
// ids alone; a page short of the limit is the whole of it and costs no second request.
func TestAttachmentListCountsTheAttachmentsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	const page = `[{"$type":"IssueAttachment","id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain",` +
		`"url":"/api/files/12-2?sign=s&updated=1"},` +
		`{"$type":"IssueAttachment","id":"12-3","name":"b.txt","size":2,"mimeType":"text/plain",` +
		`"url":"/api/files/12-3?sign=s&updated=2"}]`
	const printed = `  - {id: "12-2", name: "a.txt", size: 1, mimeType: "text/plain", url: "%s/api/files/12-2?sign=s&updated=1"}` + "\n" +
		`  - {id: "12-3", name: "b.txt", size: 2, mimeType: "text/plain", url: "%s/api/files/12-3?sign=s&updated=2"}` + "\n"
	tests := []struct {
		name  string
		count string
		head  string
	}{
		{
			name:  "more counted than arrived",
			count: `[{"$type":"IssueAttachment","id":"12-2"},{"$type":"IssueAttachment","id":"12-3"},{"$type":"IssueAttachment","id":"12-4"}]`,
			head:  "total: 3\nreturned: 2\ntruncated: true\nattachments:\n",
		},
		{
			name:  "as many counted as arrived",
			count: `[{"$type":"IssueAttachment","id":"12-2"},{"$type":"IssueAttachment","id":"12-3"}]`,
			head:  "total: 2\nreturned: 2\ntruncated: false\nattachments:\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, countedBy(page, respondWith(http.StatusOK, tc.count)))

			got := runWith(t, server.env(), "attachment", "list", "DEV-1", "--limit", "2")

			want := tc.head + strings.ReplaceAll(printed, "%s", server.url)
			assert.Equal(t, outcome{stdout: want}, got)
			assert.Equal(t, countingAttachments("2"), server.sentQueries())
			assert.Equal(t, []string{"/api/issues/DEV-1/attachments", "/api/issues/DEV-1/attachments"}, server.sentPaths())
		})
	}
}

// Fewer counted than arrived is no partial answer to print: the two passes saw different collections, and
// what the first of them found is no longer what the owner holds.
func TestAttachmentListRefusesACountBelowTheAttachmentsReceived(t *testing.T) {
	t.Parallel()
	const page = `[{"$type":"IssueAttachment","id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain",` +
		`"url":"/api/files/12-2?sign=s&updated=1"}]`
	server := serve(t, countedBy(page, respondWith(http.StatusOK, `[]`)))

	got := runWith(t, server.env(), "attachment", "list", "DEV-1", "--limit", "1")

	want := faultDocument{
		code:    "upstream_failed",
		details: []detail{{"total", 0}, {"returned", 1}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, countingAttachments("1"), server.sentQueries())
}

// More than the limit asked for is an answer to a question nobody asked, and printing it would print a
// page the caller set a size for.
func TestAttachmentListRefusesMoreAttachmentsThanTheLimit(t *testing.T) {
	t.Parallel()
	const page = `[{"$type":"IssueAttachment","id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain",` +
		`"url":"/api/files/12-2?sign=s&updated=1"},` +
		`{"$type":"IssueAttachment","id":"12-3","name":"b.txt","size":2,"mimeType":"text/plain",` +
		`"url":"/api/files/12-3?sign=s&updated=2"},` +
		`{"$type":"IssueAttachment","id":"12-4","name":"c.txt","size":3,"mimeType":"text/plain",` +
		`"url":"/api/files/12-4?sign=s&updated=3"}]`
	server := serve(t, respondWith(http.StatusOK, page))

	got := runWith(t, server.env(), "attachment", "list", "DEV-1", "--limit", "2")

	want := faultDocument{
		code:    "upstream_invalid",
		details: []detail{{"limit", 2}, {"returned", 3}},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Len(t, server.requests(), 1)
}

// What the server answers with is read the way it is read everywhere: an owner it has none of, a token it
// does not let through, and a page that is no answer of the API at all.
func TestAttachmentListRefusesWhatTheServerAnswered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		code        string
	}{
		{
			name: "an owner the server has none of", status: http.StatusNotFound,
			contentType: "application/json", body: entityNotFound("DEV-1"),
			code: "not_found",
		},
		{
			name: "a token the server does not let through", status: http.StatusForbidden,
			contentType: "application/json", body: `{"error":"Forbidden","error_description":"Access to the project is denied"}`,
			code: "denied",
		},
		{
			name: "a page under a 200", status: http.StatusOK,
			contentType: "text/html", body: "<html><body>Sign in</body></html>",
			code: "upstream_invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			got := runWith(t, server.env(), "attachment", "list", "DEV-1")

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t,
				attachmentsRequest(server.url, "issues", "DEV-1", attachmentFields, "50"),
				detailNamed(t, found, "request"))
			assert.Len(t, server.requests(), 1)
		})
	}
}

// The knowledge base is reached through its own API and answers with its own schema; thumbnailURL
// arrives on it although the specification declares the name for an attachment of an issue alone, and it
// is resolved against the instance like every other link of an attachment.
func TestAttachmentListReadsAnArticleThroughTheAPIOfArticles(t *testing.T) {
	t.Parallel()
	const records = `[{"$type":"ArticleAttachment","id":"522-4","name":"кот.png","size":70,` +
		`"mimeType":"image/png","url":"/api/files/522-4?sign=z&updated=2",` +
		`"thumbnailURL":"/api/files/211-3?sign=y&updated=2"}]`
	server := serve(t, respondWith(http.StatusOK, records))

	got := runWith(t, server.env(), "attachment", "list", "DEV-A-7", "--fields", "+thumbnailURL")

	want := "total: 1\nreturned: 1\ntruncated: false\nattachments:\n" +
		`  - {id: "522-4", name: "кот.png", size: 70, mimeType: "image/png", url: "` +
		server.url + `/api/files/522-4?sign=z&updated=2", thumbnailURL: "` +
		server.url + `/api/files/211-3?sign=y&updated=2"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/articles/DEV-A-7/attachments"}, server.sentPaths())
	assert.Equal(t, []url.Values{{"fields": {attachmentFields + ",thumbnailURL"}, "$top": {"50"}}}, server.sentQueries())
}

// The three things the help sells --fields +comment(id) as: the expression goes out as it was written,
// a file that hangs from a comment stands in the list of the issue under the id of that comment, and one
// attached to the issue itself carries comment: null.
func TestAttachmentListNamesTheCommentAFileBelongsTo(t *testing.T) {
	t.Parallel()
	const records = `[{"$type":"IssueAttachment","id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain",` +
		`"url":"/api/files/12-2?sign=s&updated=1","comment":null},` +
		`{"$type":"IssueAttachment","id":"12-3","name":"b.txt","size":2,"mimeType":"text/plain",` +
		`"url":"/api/files/12-3?sign=s&updated=2","comment":{"$type":"IssueComment","id":"7-12"}}]`
	server := serve(t, respondWith(http.StatusOK, records))

	got := runWith(t, server.env(), "attachment", "list", "DEV-1", "--fields", "+comment(id)")

	want := "total: 2\nreturned: 2\ntruncated: false\nattachments:\n" +
		`  - {id: "12-2", name: "a.txt", size: 1, mimeType: "text/plain", url: "` + server.url +
		`/api/files/12-2?sign=s&updated=1", comment: null}` + "\n" +
		`  - {id: "12-3", name: "b.txt", size: 2, mimeType: "text/plain", url: "` + server.url +
		`/api/files/12-3?sign=s&updated=2", comment: {id: "7-12"}}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t,
		[]url.Values{{"fields": {attachmentFields + ",comment(id)"}, "$top": {"50"}}},
		server.sentQueries())
}

func TestAttachmentListReadsTheDevInstanceAttachment(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "attachment", "list", "DEV-1")

	printed := requireAttachmentListing(t, got)
	assert.Equal(t, 1, printed.Total)
	assert.Equal(t, 1, printed.Returned)
	assert.False(t, printed.Truncated)
	require.Len(t, printed.Attachments, 1)
	file := printed.Attachments[0]
	assert.Regexp(t, attachmentIDForm, file.ID)
	assert.Equal(t, devInstanceAttachmentName, file.Name)
	assert.Equal(t, devInstanceAttachmentSize, file.Size)
	assert.Equal(t, "text/plain", file.MimeType)
	reference, whole := strings.CutPrefix(file.URL, dev.url)
	require.True(t, whole, "%q does not begin with the address ytrack was given", file.URL)
	assert.Regexp(t, signedLinkForm, reference)

	asked := dev.requests()
	require.Len(t, asked, 1)
	assert.Equal(t, "/api/issues/DEV-1/attachments", asked[0].URL.Path)
	assert.NotContains(t, strings.Join(dev.sentPaths(), " "), filesPath)

	response, body := fetched(t, file.URL, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "body: %s", body)
	assert.Len(t, body, file.Size)
}

func TestAttachmentListReadsTheCommentOfTheDevInstanceAttachment(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "attachment", "list", "DEV-1", "--fields", "+comment(id)")

	printed := requireAttachmentListing(t, got)
	require.Len(t, printed.Attachments, 1)
	assert.Equal(t, devInstanceAttachmentName, printed.Attachments[0].Name)
	assert.Nil(t, printed.Attachments[0].Comment, "the file hangs from a comment")
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, []string{attachmentFields + ",comment(id)"}, dev.sentFields())
}

// The second pass is what a page that fills the limit is counted by, live: one attachment counted out of
// one is the whole of them, and truncated says so.
func TestAttachmentListCountsTheDevInstanceAttachmentsWhenTheyFillTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "attachment", "list", "DEV-1", "--limit", "1")

	printed := requireAttachmentListing(t, got)
	assert.Equal(t, 1, printed.Total)
	assert.Equal(t, 1, printed.Returned)
	assert.False(t, printed.Truncated)
	assert.Equal(t, countingAttachments("1"), dev.sentQueries())
	assert.Equal(t, []string{"/api/issues/DEV-1/attachments", "/api/issues/DEV-1/attachments"}, dev.sentPaths())
}

func TestAttachmentListReadsAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "attachment", "list", "DEV-A-1")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nattachments: []\n"}, got)
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/articles/DEV-A-1/attachments", dev.sentPaths()[0])
}

// An issue the token may not see is the same answer as an issue nobody filed: YouTrack answers 404 for
// both, and ytrack passes that on rather than guessing which of the two it was.
func TestAttachmentListRefusesAnIssueTheTokenIsNotAnsweredFor(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	t.Run("an issue the dev instance has none of", func(t *testing.T) {
		got := runWith(t, dev.env(), "attachment", "list", "DEV-99999")

		found := requireRefusal(t, got)
		assert.Equal(t, "not_found", found.code)
		assert.Equal(t, "Entity with id DEV-99999 not found", detailNamed(t, found, "upstream_message"))
		require.Len(t, dev.requests(), 1)
		assert.Equal(t, "/api/issues/DEV-99999/attachments", dev.sentPaths()[0])
	})

	t.Run("an issue the limited token may not see", func(t *testing.T) {
		before := len(dev.requests())

		got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
			"attachment", "list", "DEV-1")

		found := requireRefusal(t, got)
		assert.Equal(t, "not_found", found.code)
		assert.Equal(t, "Entity with id DEV-1 not found", detailNamed(t, found, "upstream_message"))
		assert.Len(t, dev.requests()[before:], 1)
	})
}
