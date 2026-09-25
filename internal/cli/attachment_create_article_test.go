package cli_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func articleAttachmentWriteRequest(address, owner, fields string) string {
	return "POST " + address + "/api/articles/" + owner + "/attachments?fields=" + fields
}

func filedByAnArticle(name string, size int) string {
	return `[` + theArticleAttachment(name, size) + `]`
}

func theArticleAttachment(name string, size int) string {
	return `{"$type":"ArticleAttachment","id":"522-9","name":` + strconv.Quote(name) +
		`,"size":` + strconv.Itoa(size) + `,"mimeType":"image/png",` +
		`"url":"/api/files/522-9?sign=s&updated=1"}`
}

func onePixelPNG() []byte {
	return []byte("\x89\x50\x4e\x47\x0d\x0a\x1a\x0a\x00\x00\x00\x0d\x49\x48\x44\x52" +
		"\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4" +
		"\x89\x00\x00\x00\x0d\x49\x44\x41\x54\x78\xda\x63\xf8\xcf\xc0\xf0" +
		"\x1f\x00\x05\x00\x01\xff\x56\xc7\x2f\x0d\x00\x00\x00\x00\x49\x45" +
		"\x4e\x44\xae\x42\x60\x82")
}

func TestAttachmentCreateSendsAnArticleTheSameMultipartAsAnIssue(t *testing.T) {
	t.Parallel()
	const name = "кот.png"
	content := onePixelPNG()
	server := serve(t, respondWith(http.StatusOK, filedByAnArticle(name, len(content))))
	path := fileWith(t, name, content)

	got := runWith(t, server.env(), "attachment", "create", "DEV-A-7", path)

	want := `id: "522-9"` + "\n" + `name: "` + name + `"` + "\nsize: " + strconv.Itoa(len(content)) + "\n" +
		`mimeType: "image/png"` + "\n" +
		`url: "` + server.url + `/api/files/522-9?sign=s&updated=1"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)

	asked := server.requests()
	require.Len(t, asked, 1)
	assert.Equal(t, http.MethodPost, asked[0].Method)
	assert.Equal(t, "/api/articles/DEV-A-7/attachments", asked[0].URL.Path)
	assert.Equal(t, []url.Values{{"fields": {attachmentFields}}}, server.sentQueries())
	assert.True(t, strings.HasPrefix(asked[0].Header.Get("Content-Type"), multipartForm+"; boundary="),
		"the content type names no boundary: %q", asked[0].Header.Get("Content-Type"))

	sent := requireOnePart(t, server)
	assert.Equal(t, name, sent.file)
	assert.Equal(t, string(content), sent.content)
	body := server.asks()[0]
	assert.False(t, json.Valid([]byte(body)), "the body is JSON: %q", body)
	assert.NotContains(t, body, "base64Content")
}

func TestAttachmentCreateRefusesTheSingleObjectTheSpecificationDeclaresForAnArticle(t *testing.T) {
	t.Parallel()
	const name = "кот.png"
	content := onePixelPNG()
	server := serve(t, respondWith(http.StatusOK, theArticleAttachment(name, len(content))))
	path := fileWith(t, name, content)

	got := runWith(t, server.env(), "attachment", "create", "DEV-A-7", path)

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, detail{"request", articleAttachmentWriteRequest(server.url, "DEV-A-7", attachmentFields)},
		found.details[0])
	assert.Len(t, server.requests(), 1)
}

func TestAttachmentCreateAttachesAPictureToAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := attachedArticle(t, dev)
	content := onePixelPNG()
	path := fileWith(t, "image.png", content)

	got := runWith(t, dev.env(), "attachment", "create", article, path, "--fields", "+thumbnailURL")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	written := requireAttachmentPrinted(t, got.stdout)
	assert.Regexp(t, attachmentIDForm, written.ID)
	assert.Equal(t, "image.png", written.Name)
	assert.Equal(t, len(content), written.Size)
	assert.Equal(t, "image/png", written.MimeType)
	reference, whole := strings.CutPrefix(written.URL, dev.url)
	require.True(t, whole, "%q does not begin with the address ytrack was given", written.URL)
	assert.Regexp(t, signedLinkForm, reference)
	preview, whole := strings.CutPrefix(written.ThumbnailURL, dev.url)
	require.True(t, whole, "%q does not begin with the address ytrack was given", written.ThumbnailURL)
	assert.Regexp(t, previewLinkForm, preview)

	response, body := fetched(t, written.URL, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "body: %s", body)
	assert.Equal(t, content, body)

	shown, thumbnail := fetched(t, written.ThumbnailURL, "")
	require.Equal(t, http.StatusOK, shown.StatusCode, "body: %s", thumbnail)
	assert.Equal(t, "image/png", shown.Header.Get("Content-Type"))

	listed := requireAttachmentListing(t, runWith(t, dev.env(), "attachment", "list", article))
	assert.Equal(t, 1, listed.Total)
	require.Len(t, listed.Attachments, 1)
	assert.Equal(t, written.ID, listed.Attachments[0].ID)

	member := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}
	seen := requireAttachmentListing(t, runWith(t, member, "attachment", "list", article))
	require.Len(t, seen.Attachments, 1)
	filed := listed.Attachments[0]
	read := seen.Attachments[0]
	assert.Equal(t, filed.ID, read.ID)
	assert.Equal(t, filed.Name, read.Name)
	assert.Equal(t, filed.Size, read.Size)
	assert.Equal(t, filed.MimeType, read.MimeType)
	theirs, whole := strings.CutPrefix(read.URL, dev.url)
	require.True(t, whole, "%q does not begin with the address ytrack was given", read.URL)
	assert.Regexp(t, signedLinkForm, theirs)
	assert.NotEqual(t, filed.URL, read.URL)
}

func TestAttachmentCreateRefusesAnArticleTheDevInstanceHasNoneOf(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	path := fileWith(t, "заметка.bin", []byte("ytrack"))

	got := runWith(t, dev.env(), "attachment", "create", "DEV-A-99999", path)

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Can't find article with id DEV-A-99999", detailNamed(t, found, "upstream_message"))
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/articles/DEV-A-99999/attachments", dev.sentPaths()[0])
}

func attachedArticle(t *testing.T, dev *upstream) string {
	t.Helper()
	readable := fileArticle(t, dev, "ytrack contract "+t.Name())
	t.Cleanup(func() { removeArticle(t, dev, readable) })
	return readable
}
