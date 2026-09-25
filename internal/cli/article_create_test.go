package cli_test

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

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
}

func (a answeredArticle) json() string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(a.readable) +
		`,"summary":` + asJSON(a.summary) +
		`,"reporter":{"$type":"User","login":"admin"},"created":1789035410875,"updated":1789035410875,` +
		`"tags":[],"parentArticle":null,"childArticles":[],"content":` + cmp.Or(a.content, "null") +
		`,"project":{"$type":"Project","shortName":"DEV"}}`
}

func createdArticle(readable, summary, content string) string {
	return answeredArticle{readable: readable, summary: summary, content: content}.json()
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
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(u.Last(t).Body), &body))
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

func TestArticleCreateFilesTheArticleInOneRequest(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, fake.JSON(http.StatusOK, createdArticle("DEV-A-7", "Title", asJSON(hostileText))))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "Title", "--content", hostileText)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles"}, server.Paths())
	assert.Equal(t, []string{askedArticleFields}, server.Fields())
	assert.Equal(t, map[string]any{
		"project": map[string]any{"shortName": "DEV"},
		"summary": "Title",
		"content": hostileText,
	}, sentArticle(t, server))

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"idReadable", "summary", "reporter", "created", "updated", "tags", "parentArticle",
		"childArticles", "content"}, keysOf(mapping))
	assert.Equal(t, "DEV-A-7", nodeAt(t, mapping, "idReadable").Value)
	assert.Equal(t, hostileText, nodeAt(t, mapping, "content").Value)
}

const hostileText = "  First\r\nSecond\rThird   \n---\n~~~\n\xc2\x85\xe2\x80\xa8\xef\xbb\xbf\xf0\x9f\x98\x80\n  "

func TestArticleCreateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, fake.JSON(http.StatusOK, createdArticle("DEV-A-7", "title", "null")))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "Title")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleCreationRequest(server.URL, askedArticleFields)},
			{"article", "DEV-A-7"},
			{"mismatch", []any{[]detail{{"field", "summary"}, {"expected", "Title"}, {"actual", "title"}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
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
