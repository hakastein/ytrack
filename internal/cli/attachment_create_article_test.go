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

	"github.com/hakastein/ytrack/internal/fake"
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
	server := fake.Serve(t, fake.JSON(http.StatusOK, filedByAnArticle(name, len(content))))
	path := fileWith(t, name, content)

	got := runWith(t, server.Env(), "attachment", "create", "DEV-A-7", path)

	want := `id: "522-9"` + "\n" + `name: "` + name + `"` + "\nsize: " + strconv.Itoa(len(content)) + "\n" +
		`mimeType: "image/png"` + "\n" +
		`url: "` + server.Origin + `/api/files/522-9?sign=s&updated=1"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)

	asked := server.Requests()
	require.Len(t, asked, 1)
	assert.Equal(t, http.MethodPost, asked[0].Method)
	assert.Equal(t, "/api/articles/DEV-A-7/attachments", asked[0].URL.Path)
	assert.Equal(t, []url.Values{{"fields": {attachmentFields}}}, server.Queries())
	assert.True(t, strings.HasPrefix(asked[0].Header.Get("Content-Type"), multipartForm+"; boundary="),
		"the content type names no boundary: %q", asked[0].Header.Get("Content-Type"))

	sent := requireOnePart(t, server)
	assert.Equal(t, name, sent.file)
	assert.Equal(t, string(content), sent.content)
	body := server.Bodies()[0]
	assert.False(t, json.Valid([]byte(body)), "the body is JSON: %q", body)
	assert.NotContains(t, body, "base64Content")
}

func TestAttachmentCreateRefusesTheSingleObjectTheSpecificationDeclaresForAnArticle(t *testing.T) {
	t.Parallel()
	const name = "кот.png"
	content := onePixelPNG()
	server := fake.Serve(t, fake.JSON(http.StatusOK, theArticleAttachment(name, len(content))))
	path := fileWith(t, name, content)

	got := runWith(t, server.Env(), "attachment", "create", "DEV-A-7", path)

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, detail{"request", articleAttachmentWriteRequest(server.URL, "DEV-A-7", attachmentFields)},
		found.details[0])
	assert.Len(t, server.Requests(), 1)
}
