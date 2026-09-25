package cli_test

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const askedArticleFields = articleShowFields + ",project(shortName)"

func articleCreationRequest(address, fields string) string {
	return "POST " + address + "/api/articles?fields=" + fields
}

type answeredArticle struct {
	readable string
	summary  string
	content  string
	project  string
	parent   string
}

func (a answeredArticle) json() string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(a.readable) +
		`,"summary":` + asJSON(a.summary) +
		`,"reporter":{"$type":"User","login":"admin"},"created":1789035410875,"updated":1789035410875,` +
		`"tags":[],"parentArticle":` + cmp.Or(a.parent, "null") +
		`,"childArticles":[],"content":` + cmp.Or(a.content, "null") +
		`,"project":` + cmp.Or(a.project, `{"$type":"Project","shortName":"DEV"}`) + `}`
}

func createdArticle(readable, summary, content string) string {
	return answeredArticle{readable: readable, summary: summary, content: content}.json()
}

func filedArticleIn(readable, summary, content, project string) string {
	return answeredArticle{readable: readable, summary: summary, content: content, project: project}.json()
}

func creatingAnArticle(t *testing.T, creation http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a creation of an article sends one POST and nothing else") {
			return
		}
		creation(w, r)
	})
}

func sentArticle(t *testing.T, u *fake.Server) map[string]any {
	t.Helper()
	asks := u.Bodies()
	require.Len(t, asks, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(asks[0]), &body))
	return body
}

func TestArticleCreateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no title", argv: []string{"article", "create", "DEV"}},
		{name: "no title and a code of no form", argv: []string{"article", "create", "1DEV"}},
		{name: "a code that opens with a digit", argv: []string{"article", "create", "1DEV", "--summary", "x"}},
		{name: "two dots for a project", argv: []string{"article", "create", "..", "--summary", "x"}},
		{
			name: "a title twice",
			argv: []string{"article", "create", "DEV", "--summary", "a", "--summary", "b"},
		},
		{
			name: "content twice",
			argv: []string{"article", "create", "DEV", "--summary", "x", "--content", "a", "--content", "b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleCreateRefusesTextTheServerWouldRewrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a line feed in the title", argv: []string{"--summary", "a\nb"}},
		{name: "a carriage return in the title", argv: []string{"--summary", "a\rb"}},
		{name: "a CRLF in the title", argv: []string{"--summary", "a\r\nb"}},
		{name: "an empty title", argv: []string{"--summary", ""}},
		{name: "a title that is no UTF-8", argv: []string{"--summary", "a\xffb"}},
		{name: "empty content", argv: []string{"--summary", "x", "--content", ""}},
		{name: "content that is no UTF-8", argv: []string{"--summary", "x", "--content", "a\xffb"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"article", "create", "DEV"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleCreatePrintsNoComments(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "x", "--fields", "+comments(text)")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestArticleCreateFilesTheArticleInOneRequest(t *testing.T) {
	t.Parallel()
	const title = "[bug] fix login"
	text := longContent()
	server := creatingAnArticle(t, fake.JSON(http.StatusOK, createdArticle("DEV-A-7", title, asJSON(text))))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", title, "--content", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles"}, server.Paths())
	assert.Equal(t, []string{askedArticleFields}, server.Fields())
	assert.Equal(t, map[string]any{
		"project": map[string]any{"shortName": "DEV"},
		"summary": title,
		"content": text,
	}, sentArticle(t, server))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"idReadable", "summary", "reporter", "created", "updated", "tags", "parentArticle",
		"childArticles", "content"}, keysOf(mapping))
	assert.Equal(t, "DEV-A-7", nodeAt(t, mapping, "idReadable").Value)
	assert.Equal(t, text, nodeAt(t, mapping, "content").Value)
}

func longContent() string {
	return textOfSize(hostileText, 131_071)
}

const hostileText = "Шаги:  \r\n1. открыть\rи закрыть   \n---\n\xe2\x80\xa8и ещё \xf0\x9f\x98\x80\n"

func textOfSize(chunk string, size int) string {
	var written strings.Builder
	for written.Len() < size {
		written.WriteString(chunk)
	}
	text := written.String()
	cut := size
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + strings.Repeat("x", size-cut)
}

func TestArticleCreateWritesTheTextItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want map[string]any
	}{
		{
			name: "a NEL in the title",
			argv: []string{"--summary", "первая\xc2\x85вторая"},
			want: map[string]any{"summary": "первая\xc2\x85вторая"},
		},
		{
			name: "a line separator in the title",
			argv: []string{"--summary", "первая\xe2\x80\xa8вторая"},
			want: map[string]any{"summary": "первая\xe2\x80\xa8вторая"},
		},
		{
			name: "a paragraph separator in the title",
			argv: []string{"--summary", "первая\xe2\x80\xa9вторая"},
			want: map[string]any{"summary": "первая\xe2\x80\xa9вторая"},
		},
		{
			name: "a tab in the title",
			argv: []string{"--summary", "первая\tвторая"},
			want: map[string]any{"summary": "первая\tвторая"},
		},
		{
			name: "spaces around the title",
			argv: []string{"--summary", "  заголовок  "},
			want: map[string]any{"summary": "  заголовок  "},
		},
		{
			name: "a title of spaces alone",
			argv: []string{"--summary", "   "},
			want: map[string]any{"summary": "   "},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			title, _ := tc.want["summary"].(string)
			content := "null"
			if text, written := tc.want["content"].(string); written {
				content = asJSON(text)
			}
			server := creatingAnArticle(t, fake.JSON(http.StatusOK, createdArticle("DEV-A-7", title, content)))

			got := runWith(t, server.Env(), append([]string{"article", "create", "DEV"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			tc.want["project"] = map[string]any{"shortName": "DEV"}
			assert.Equal(t, tc.want, sentArticle(t, server))
		})
	}
}

func TestArticleCreateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		argv     []string
		filed    string
		article  string
		mismatch []any
	}{
		{
			name:     "a title the server stored otherwise",
			argv:     []string{"--summary", "a b"},
			filed:    createdArticle("DEV-A-7", "a  b", "null"),
			article:  "DEV-A-7",
			mismatch: []any{[]detail{{"field", "summary"}, {"expected", "a b"}, {"actual", "a  b"}}},
		},
		{
			name:     "content the server kept none of",
			argv:     []string{"--summary", "x", "--content", "первая"},
			filed:    createdArticle("DEV-A-7", "x", "null"),
			article:  "DEV-A-7",
			mismatch: []any{[]detail{{"field", "content"}, {"expected", "первая"}, {"actual", nil}}},
		},
		{
			name:     "content the server cut a carriage return out of",
			argv:     []string{"--summary", "x", "--content", "первая\rвторая"},
			filed:    createdArticle("DEV-A-7", "x", asJSON("перваявторая")),
			article:  "DEV-A-7",
			mismatch: []any{[]detail{{"field", "content"}, {"expected", "первая\rвторая"}, {"actual", "перваявторая"}}},
		},
		{
			name:     "an article filed in another project",
			argv:     []string{"--summary", "x"},
			filed:    filedArticleIn("DEMO-A-7", "x", "null", `{"$type":"Project","shortName":"DEMO"}`),
			article:  "DEMO-A-7",
			mismatch: []any{[]detail{{"field", "project"}, {"expected", "DEV"}, {"actual", "DEMO"}}},
		},
		{
			name:     "no project on the article at all",
			argv:     []string{"--summary", "x"},
			filed:    filedArticleIn("DEV-A-7", "x", "null", "null"),
			article:  "DEV-A-7",
			mismatch: []any{[]detail{{"field", "project"}, {"expected", "DEV"}, {"actual", nil}}},
		},
		{
			name:    "the title and the content both",
			argv:    []string{"--summary", "a b", "--content", "первая"},
			filed:   createdArticle("DEV-A-7", "a  b", asJSON("вторая")),
			article: "DEV-A-7",
			mismatch: []any{
				[]detail{{"field", "summary"}, {"expected", "a b"}, {"actual", "a  b"}},
				[]detail{{"field", "content"}, {"expected", "первая"}, {"actual", "вторая"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creatingAnArticle(t, fake.JSON(http.StatusOK, tc.filed))

			got := runWith(t, server.Env(), append([]string{"article", "create", "DEV"}, tc.argv...)...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleCreationRequest(server.URL, askedArticleFields)},
					{"article", tc.article},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestArticleCreateChecksNoContentWhereNoneWasWritten(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, fake.JSON(http.StatusOK, createdArticle("DEV-A-7", "x", "null")))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "x")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "x"},
		sentArticle(t, server))
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "content")))
}

func TestArticleCreateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		status          int
		code            string
		upstreamError   string
		upstreamMessage string
		details         []detail
	}{
		{
			name:            "a project the instance has none of",
			status:          http.StatusNotFound,
			code:            "not_found",
			upstreamError:   "Not Found",
			upstreamMessage: "Project was not found",
		},
		{
			name:            "a project the token may not write in",
			status:          http.StatusForbidden,
			code:            "denied",
			upstreamError:   "Forbidden",
			upstreamMessage: "HTTP 403 Forbidden",
			details:         []detail{authFromEnv()},
		},
		{
			name:            "a body the server disagreed with",
			status:          http.StatusBadRequest,
			code:            "rejected",
			upstreamError:   "Bad Request",
			upstreamMessage: "Value is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := `{"error":` + strconv.Quote(tc.upstreamError) + `,"error_description":` +
				strconv.Quote(tc.upstreamMessage) + `}`
			server := creatingAnArticle(t, fake.JSON(tc.status, said))

			got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "x")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", articleCreationRequest(server.URL, askedArticleFields)},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

func TestArticleCreateIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, breakOff)

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", articleCreationRequest(server.URL, askedArticleFields)}}, found.details)
	assert.Empty(t, got.stdout)
}

func TestArticleCreateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, fake.JSON(http.StatusOK, createdArticle("DEV-A-7", "x", asJSON("первая"))))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "x", "--content", "первая",
		"--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "idReadable: \"DEV-A-7\"\n", got.stdout)
	assert.Equal(t, []string{"idReadable,summary,content,project(shortName)"}, server.Fields())
}
