package cli_test

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/require"
)

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
