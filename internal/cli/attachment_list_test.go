package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const attachmentFields = "id,name,size,mimeType,url"

func attachmentsRequest(address, owners, id, fields, top string) string {
	return "GET " + address + "/api/" + owners + "/" + id + "/attachments?fields=" + fields + "&$top=" + top
}

func countingAttachments(limit string) []url.Values {
	return []url.Values{
		{"fields": {attachmentFields}, "$top": {limit}},
		{"fields": {"id"}, "$top": {"-1"}},
	}
}

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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestAttachmentListRefusesTheContentOfAFile(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "attachment", "list", "DEV-1", "--fields", "+base64Content")

	found := requireFault(t, got)
	assert.Equal(t, "bad_usage", found.code)
	assert.Empty(t, server.requests())
}

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
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, countingAttachments("1"), server.sentQueries())
}

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
	assert.Equal(t, want, requireFault(t, got))
	assert.Len(t, server.requests(), 1)
}

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

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t,
				attachmentsRequest(server.url, "issues", "DEV-1", attachmentFields, "50"),
				detailNamed(t, found, "request"))
			assert.Len(t, server.requests(), 1)
		})
	}
}

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
