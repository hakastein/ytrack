package youtrack_test

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func attachmentFile(name, content string) youtrack.FileOpener {
	return func() (youtrack.AttachedFile, *diag.Fault) {
		return youtrack.AttachedFile{Name: name, Body: io.NopCloser(strings.NewReader(content))}, nil
	}
}

func attachmentFiled(schema, name, size string) string {
	return `{"$type":` + strconv.Quote(schema) + `,"id":"12-9","name":` + name + `,"size":` + size + `}`
}

func attachmentFiledOnce(name string, size int) string {
	return `[` + attachmentFiled("IssueAttachment", strconv.Quote(name), strconv.Itoa(size)) + `]`
}

type attachmentPart struct {
	field       string
	file        string
	disposition string
	content     string
}

func attachmentParts(t *testing.T, sent fake.Request) []attachmentPart {
	t.Helper()
	kind, params, err := mime.ParseMediaType(sent.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", kind)
	form := multipart.NewReader(strings.NewReader(sent.Body), params["boundary"])
	var parts []attachmentPart
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			return parts
		}
		require.NoError(t, err)
		content, err := io.ReadAll(part)
		require.NoError(t, err)
		parts = append(parts, attachmentPart{
			field:       part.FormName(),
			file:        part.FileName(),
			disposition: part.Header.Get("Content-Disposition"),
			content:     string(content),
		})
	}
}

func TestCreateAttachmentRefusesANameTheServerWouldKeepAsAnother(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
	}{
		{name: "a double quote", file: `a"b.txt`},
		{name: "a backslash", file: `a\b.txt`},
		{name: "a line feed", file: "a\nb.txt"},
		{name: "a carriage return", file: "a\rb.txt"},
		{name: "a leading space", file: " a.txt"},
		{name: "a trailing space", file: "a.txt "},
		{name: "a leading tab", file: "\ta.txt"},
		{name: "a trailing vertical tab", file: "a.txt\v"},
		{name: "a leading NUL", file: "\x00a.txt"},
		{name: "bytes that are no UTF-8", file: "\xff.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.CreateAttachment("DEV-1", attachmentFile(tc.file, "x"), nil)
			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCreateAttachmentSendsTheNameAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
	}{
		{name: "a space", file: "a b.txt"},
		{name: "a semicolon and a per cent", file: "a;100%.txt"},
		{name: "a tab inside", file: "a\tb.txt"},
		{name: "a line separator inside", file: "a\u2028b.txt"},
		{name: "a non-breaking space first", file: "\u00a0a.txt"},
		{name: "Cyrillic", file: "заметка.txt"},
		{name: "254 bytes of Cyrillic", file: strings.Repeat("я", 127)},
		{name: "three dots", file: "..."},
		{name: "a lone dash", file: "-"},
		{name: "a leading dash", file: "-a.txt"},
		{name: "no extension", file: "README"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, attachmentFiledOnce(tc.file, 1)))
			call, fault := youtrack.CreateAttachment("DEV-1", attachmentFile(tc.file, "x"), new("id"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, []attachmentPart{{
				field:       "files[0]",
				file:        tc.file,
				disposition: `form-data; name="files[0]"; filename="` + tc.file + `"`,
				content:     "x",
			}}, attachmentParts(t, server.Last(t)))
		})
	}
}

func TestCreateAttachmentStreamsTheFileAsOnePart(t *testing.T) {
	t.Parallel()
	content := make([]byte, 300*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	tests := []struct {
		name  string
		owner string
		path  string
		filed string
	}{
		{
			name:  "to an issue",
			owner: "DEV-1",
			path:  "/api/issues/DEV-1/attachments",
			filed: `[` + attachmentFiled("IssueAttachment", `"one.bin"`, strconv.Itoa(len(content))) + `]`,
		},
		{
			name:  "to an article",
			owner: "DEV-A-1",
			path:  "/api/articles/DEV-A-1/attachments",
			filed: `[` + attachmentFiled("ArticleAttachment", `"one.bin"`, strconv.Itoa(len(content))) + `]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.filed))
			call, fault := youtrack.CreateAttachment(tc.owner, attachmentFile("one.bin", string(content)), new("id"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			sent := server.Last(t)
			assert.Equal(t, http.MethodPost, sent.Method)
			assert.Equal(t, []string{tc.path + "?fields=id,name,size"}, server.Targets())
			assert.Equal(t, []string{"chunked"}, sent.TransferEncoding)
			assert.EqualValues(t, -1, sent.ContentLength)
			assert.Equal(t, []attachmentPart{{
				field:       "files[0]",
				file:        "one.bin",
				disposition: `form-data; name="files[0]"; filename="one.bin"`,
				content:     string(content),
			}}, attachmentParts(t, sent))
		})
	}
}

func TestCreateAttachmentAsksForTheNameAndTheSizeItChecks(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, attachmentFiledOnce("one.txt", 1)))
	call, fault := youtrack.CreateAttachment("DEV-1", attachmentFile("one.txt", "x"), new("id"))
	require.Nil(t, fault)

	node, fault := call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(render.Pair{Key: "id", Value: render.NewString("12-9")}), node)
	assert.Equal(t, []string{"id,name,size"}, server.Fields())
}

func TestCreateAttachmentRefusesAnAnswerThatIsNotTheFileThatWentOut(t *testing.T) {
	t.Parallel()
	one := attachmentFiled("IssueAttachment", `"one.txt"`, "6")
	mismatch := func(field string, expected, actual *render.Node) []render.Pair {
		return []render.Pair{
			{Key: "attachment", Value: render.NewString("12-9")},
			{Key: "mismatch", Value: render.NewList(render.NewMap(
				render.Pair{Key: "field", Value: render.NewString(field)},
				render.Pair{Key: "expected", Value: expected},
				render.Pair{Key: "actual", Value: actual}))},
		}
	}
	tests := []struct {
		name    string
		owner   string
		path    string
		body    string
		details []render.Pair
	}{
		{
			name:  "no attachment at all",
			owner: "DEV-1",
			path:  "/api/issues/DEV-1/attachments",
			body:  `[]`,
			details: []render.Pair{
				{Key: "actual_count", Value: render.NewNumber("0")},
				{Key: "upstream_body", Value: render.NewString(`[]`)},
			},
		},
		{
			name:  "two attachments",
			owner: "DEV-1",
			path:  "/api/issues/DEV-1/attachments",
			body:  `[` + one + `,` + one + `]`,
			details: []render.Pair{
				{Key: "actual_count", Value: render.NewNumber("2")},
				{Key: "upstream_body", Value: render.NewString(`[` + one + `,` + one + `]`)},
			},
		},
		{
			name:    "a name the server kept as another",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `"other.txt"`, "6") + `]`,
			details: mismatch("name", render.NewString("one.txt"), render.NewString("other.txt")),
		},
		{
			name:    "a name that arrived as a number",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `7`, "6") + `]`,
			details: mismatch("name", render.NewString("one.txt"), render.NewNumber("7")),
		},
		{
			name:    "a size that is not the count of bytes that went out",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `"one.txt"`, "7") + `]`,
			details: mismatch("size", render.NewNumber("6"), render.NewNumber("7")),
		},
		{
			name:    "a size that arrived as text",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `"one.txt"`, `"6"`) + `]`,
			details: mismatch("size", render.NewNumber("6"), render.NewString("6")),
		},
		{
			name:  "the one object the specification declares for an article",
			owner: "DEV-A-1",
			path:  "/api/articles/DEV-A-1/attachments",
			body:  attachmentFiled("ArticleAttachment", `"one.txt"`, "6"),
			details: []render.Pair{
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(attachmentFiled("ArticleAttachment", `"one.txt"`, "6"))},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))
			call, fault := youtrack.CreateAttachment(tc.owner, attachmentFile("one.txt", "ytrack"), new("id"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{
				Code:       diag.UpstreamInvalid,
				AfterWrite: true,
				Details: append([]render.Pair{
					{Key: "request", Value: render.NewString("POST " + server.URL + tc.path + "?fields=id,name,size")},
				}, tc.details...),
			}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestDeleteAttachmentRefusesAnIDThatIsNoInternalID(t *testing.T) {
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
		{name: "the name of a file", id: "a-b.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.DeleteAttachment("DEV-1", tc.id)
			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func attachmentDeleting(t *testing.T, read http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		read(w, r)
	})
}

func TestDeleteAttachmentDeletesUnderTheOwnerTheReadNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		owner    string
		read     string
		kind     string
		readable string
		targets  []string
	}{
		{
			name:     "an issue in lower case",
			owner:    "dev-7",
			read:     `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-7"}}`,
			kind:     "issue",
			readable: "DEV-7",
			targets: []string{
				"/api/issues/dev-7/attachments/12-5?fields=id,name,issue(idReadable)",
				"/api/issues/DEV-7/attachments/12-5?",
			},
		},
		{
			name:     "an article in mixed case",
			owner:    "dev-A-7",
			read:     `{"$type":"ArticleAttachment","id":"12-5","name":"a.txt","article":{"$type":"Article","idReadable":"DEV-A-7"}}`,
			kind:     "article",
			readable: "DEV-A-7",
			targets: []string{
				"/api/articles/dev-A-7/attachments/12-5?fields=id,name,article(idReadable)",
				"/api/articles/DEV-A-7/attachments/12-5?",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := attachmentDeleting(t, fake.JSON(http.StatusOK, tc.read))
			call, fault := youtrack.DeleteAttachment(tc.owner, "12-5")
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(
				render.Pair{Key: "id", Value: render.NewString("12-5")},
				render.Pair{Key: "name", Value: render.NewString("a.txt")},
				render.Pair{Key: tc.kind, Value: render.NewMap(
					render.Pair{Key: "idReadable", Value: render.NewString(tc.readable)})}), node)
			assert.Equal(t, tc.targets, server.Targets())
		})
	}
}

func TestDeleteAttachmentRefusesAnAnswerItCannotAddressTheDeletionBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read string
	}{
		{
			name: "another attachment than the one asked for",
			read: `{"$type":"IssueAttachment","id":"12-6","name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-7"}}`,
		},
		{
			name: "an id that is no text",
			read: `{"$type":"IssueAttachment","id":125,"name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-7"}}`,
		},
		{
			name: "an owner of two dots",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":".."}}`,
		},
		{
			name: "an owner that is an article",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-A-7"}}`,
		},
		{
			name: "an owner with no readable id",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":null}}`,
		},
		{
			name: "an owner that arrived as no object",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := attachmentDeleting(t, fake.JSON(http.StatusOK, tc.read))
			call, fault := youtrack.DeleteAttachment("DEV-7", "12-5")
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				{Key: "request", Value: render.NewString("GET " + server.URL +
					"/api/issues/DEV-7/attachments/12-5?fields=id,name,issue(idReadable)")},
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(tc.read)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{"/api/issues/DEV-7/attachments/12-5"}, server.Paths())
		})
	}
}

func TestListAttachmentsReadsTheOwnerThroughItsOwnAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		owner string
		path  string
	}{
		{name: "an issue", owner: "DEV-1", path: "/api/issues/DEV-1/attachments"},
		{name: "an article", owner: "DEV-A-1", path: "/api/articles/DEV-A-1/attachments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))
			call, fault := youtrack.ListAttachments(tc.owner, new("id"), youtrack.Page{Limit: 50})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, []string{tc.path}, server.Paths())
		})
	}
}

func TestFieldsRefuseTheContentOfAFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func() (youtrack.Call, *diag.Fault)
	}{
		{
			name: "under the attachments of an issue",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("attachments(base64Content)"), youtrack.Comments{})
			},
		},
		{
			name: "beside a name of the default",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("+attachments(name,base64Content)"), youtrack.Comments{})
			},
		},
		{
			name: "under the attachments of an article",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowArticle("DEV-A-1", new("attachments(base64Content)"), youtrack.Comments{})
			},
		},
		{
			name: "in a record of a list of issues",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListIssues("x", new("+attachments(base64Content)"), youtrack.Page{Limit: 50}, nil)
			},
		},
		{
			name: "in a record of a list of attachments",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListAttachments("DEV-1", new("+base64Content"), youtrack.Page{Limit: 50})
			},
		},
		{
			name: "in what an upload prints",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateAttachment("DEV-1", attachmentFile("one.txt", "x"), new("id,base64Content"))
			},
		},
		{
			name: "at a place that holds no file at all",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "base64Content") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := tc.call()
			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}
