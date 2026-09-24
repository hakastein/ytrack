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

// The request an upload to an article goes out as, which is the one a refusal about it names.
func articleAttachmentWriteRequest(address, owner, fields string) string {
	return "POST " + address + "/api/articles/" + owner + "/attachments?fields=" + fields
}

// filedByAnArticle is what the knowledge base answers an upload with: the array of what the write filed, at
// the schema of its own, holding the one attachment.
func filedByAnArticle(name string, size int) string {
	return `[` + theArticleAttachment(name, size) + `]`
}

// theArticleAttachment is the single object the specification declares the answer to be, which the instance
// never sends: it answers the array an issue's upload is answered with.
func theArticleAttachment(name string, size int) string {
	return `{"$type":"ArticleAttachment","id":"522-9","name":` + strconv.Quote(name) +
		`,"size":` + strconv.Itoa(size) + `,"mimeType":"image/png",` +
		`"url":"/api/files/522-9?sign=s&updated=1"}`
}

// A PNG of one transparent pixel, 70 bytes as the file stands: the polygon works the type out from the content
// and makes a preview only for a picture it can read, so nothing shorter would bring a thumbnailURL back.
func onePixelPNG() []byte {
	return []byte("\x89\x50\x4e\x47\x0d\x0a\x1a\x0a\x00\x00\x00\x0d\x49\x48\x44\x52" +
		"\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4" +
		"\x89\x00\x00\x00\x0d\x49\x44\x41\x54\x78\xda\x63\xf8\xcf\xc0\xf0" +
		"\x1f\x00\x05\x00\x01\xff\x56\xc7\x2f\x0d\x00\x00\x00\x00\x49\x45" +
		"\x4e\x44\xae\x42\x60\x82")
}

// The specification declares an upload to an article to be JSON carrying the bytes encoded,
// and the instance answers every form of that JSON 500, so the file goes out the way an issue's does: one
// multipart part and no JSON anywhere in the body.
func TestAttachmentCreateSendsAnArticleTheSameMultipartAsAnIssue(t *testing.T) {
	t.Parallel()
	const name = "кот.png"
	content := onePixelPNG()
	server := serve(t, answer(http.StatusOK, filedByAnArticle(name, len(content))))
	path := fileHolding(t, name, content)

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

// The specification declares this one answer to be a single attachment where an issue's is an array of
// them, and the instance sends the array for both. An object is therefore an answer of something other than
// the endpoint that was asked, and the file is attached by the time it arrives.
func TestAttachmentCreateRefusesTheSingleObjectTheSpecificationDeclaresForAnArticle(t *testing.T) {
	t.Parallel()
	const name = "кот.png"
	content := onePixelPNG()
	server := serve(t, answer(http.StatusOK, theArticleAttachment(name, len(content))))
	path := fileHolding(t, name, content)

	got := runWith(t, server.env(), "attachment", "create", "DEV-A-7", path)

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Equal(t, detail{"request", articleAttachmentWriteRequest(server.url, "DEV-A-7", attachmentFields)},
		found.details[0])
	assert.Len(t, server.requests(), 1)
}

// A picture attached to an article of the polygon for real: the knowledge base
// takes the multipart the specification says it takes none of, works the type out itself, and sends a
// thumbnailURL back although ArticleAttachment declares no such name. Both links are whole and signed, and
// each hands its own bytes to whoever holds it.
func TestAttachmentCreateAttachesAPictureToAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	article := attachedArticle(t, dev)
	content := onePixelPNG()
	path := fileHolding(t, "image.png", content)

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

	// Every key of the default arrives for a member of the project as it does for an administrator, so
	// none of them costs such a reader their document. The polygon holds no article with a file of its own,
	// so the one filed here is where that is read.
	member := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}
	seen := requireAttachmentListing(t, runWith(t, member, "attachment", "list", article))
	require.Len(t, seen.Attachments, 1)
	filed := listed.Attachments[0]
	read := seen.Attachments[0]
	assert.Equal(t, filed.ID, read.ID)
	assert.Equal(t, filed.Name, read.Name)
	assert.Equal(t, filed.Size, read.Size)
	assert.Equal(t, filed.MimeType, read.MimeType)
	// The signature names whoever it was issued to, so the same file signed for a member is a link of its
	// own; what holds across the two readers is its form and the file it opens.
	theirs, whole := strings.CutPrefix(read.URL, dev.url)
	require.True(t, whole, "%q does not begin with the address ytrack was given", read.URL)
	assert.Regexp(t, signedLinkForm, theirs)
	assert.NotEqual(t, filed.URL, read.URL)
}

// An article nobody wrote is answered 404 by the server itself, and nothing about the owner was asked
// beforehand: one request is the whole call, and it went to the knowledge base and nowhere else. The words of
// that 404 are the knowledge base's own — an issue is refused "Entity with id … not found" — and they pass on
// as they came.
func TestAttachmentCreateRefusesAnArticleThePolygonHasNoneOf(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	path := fileHolding(t, "заметка.bin", []byte("ytrack"))

	got := runWith(t, dev.env(), "attachment", "create", "DEV-A-99999", path)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, "Can't find article with id DEV-A-99999", detailNamed(t, found, "upstream_message"))
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/articles/DEV-A-99999/attachments", dev.sentPaths()[0])
}

// attachedArticle is the fixture of a contract test that needs an article of its own to attach files to: it is
// filed by the command that files articles and taken away afterwards with everything hanging from it.
func attachedArticle(t *testing.T, dev *upstream) string {
	t.Helper()
	readable := fileArticle(t, dev, "ytrack contract "+t.Name())
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeArticle(t, dev, readable) })
	return readable
}
