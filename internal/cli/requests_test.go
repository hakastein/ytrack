package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const markupFields = "query,styleRanges(start,length,style)"

func requireMarkedUpFirst(t *testing.T, server *fake.Server, query string) {
	t.Helper()
	first := server.Request(t, 0)
	assert.Equal(t, fake.AssistPath, first.URL.Path)
	assert.Equal(t, 1, sentTo(server, fake.AssistPath))
	assert.Equal(t, http.MethodPost, first.Method)
	assert.Equal(t, "application/json", first.Header.Get("Content-Type"))
	assert.Equal(t, markupFields, first.URL.Query().Get("fields"))
	asked, err := json.Marshal(struct {
		Query string `json:"query"`
	}{Query: query})
	require.NoError(t, err)
	assert.Equal(t, string(asked), first.Body)
}

const multipartForm = "multipart/form-data"

type formPart struct {
	field       string
	file        string
	content     string
	disposition string
}

func partsOf(body []byte, contentType string) ([]formPart, bool) {
	boundary, isForm := formBoundary(contentType)
	if !isForm {
		return nil, false
	}
	form := multipart.NewReader(bytes.NewReader(body), boundary)
	read := []formPart{}
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			return read, true
		}
		if err != nil {
			return nil, false
		}
		content, err := io.ReadAll(part)
		if err != nil {
			return nil, false
		}
		read = append(read, formPart{
			field:       part.FormName(),
			file:        part.FileName(),
			content:     string(content),
			disposition: part.Header.Get("Content-Disposition"),
		})
	}
}

func formBoundary(contentType string) (string, bool) {
	kind, params, err := mime.ParseMediaType(contentType)
	if err != nil || kind != multipartForm {
		return "", false
	}
	boundary, given := params["boundary"]
	return boundary, given
}

func sentParts(t *testing.T, server *fake.Server, at int) []formPart {
	t.Helper()
	sent := server.Request(t, at)
	parts, isForm := partsOf([]byte(sent.Body), sent.Header.Get("Content-Type"))
	require.True(t, isForm, "the body of %s %s is no multipart form: %q", sent.Method, sent.URL, sent.Body)
	return parts
}

func sentTo(server *fake.Server, path string) int {
	sent := 0
	for _, p := range server.Paths() {
		if p == path {
			sent++
		}
	}
	return sent
}
