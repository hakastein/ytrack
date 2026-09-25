package cli_test

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// What a creation asks of the article it filed: what the caller asked to print, and beside it every part that
// went out, so the check of the write has it to hold the answer against. The project stands here and nowhere
// in the default, since it is the one part of a creation the caller names that an article does not print.
const askedArticleFields = articleShowFields + ",project(shortName)"

// The request a creation goes out as, which is the one a refusal about it names.
func articleCreationRequest(address, fields string) string {
	return "POST " + address + "/api/articles?fields=" + fields
}

// The article a creation answers with, under the expression that goes out. Each of content, project and parent
// is JSON already, so a scenario may send null for any of them; a scenario that leaves one out gets what a
// creation of an article at the root of DEV comes back as.
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

// creatingAnArticle is the server of a creation: the POST is the whole command, so a scenario says what that
// one request was answered with.
func creatingAnArticle(t *testing.T, creation http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a creation of an article sends one POST and nothing else") {
			return
		}
		creation(w, r)
	})
}

// sentArticle is the body of the one request that went out, read as JSON reads it.
func sentArticle(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	asks := u.asks()
	require.Len(t, asks, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(asks[0]), &body))
	return body
}

// What a creation takes: one project, written as an argument, and a title, which is the value of a flag.
// The title is looked for before the code is read, so a call giving neither is answered about the title, which
// is how ytrack issue create answers it.
func TestArticleCreateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no project", argv: []string{"article", "create"}},
		{name: "no title", argv: []string{"article", "create", "DEV"}},
		{name: "neither project nor title", argv: []string{"article", "create"}},
		{name: "no title and a code of no form", argv: []string{"article", "create", "1DEV"}},
		{name: "the title as an argument", argv: []string{"article", "create", "DEV", "Заголовок"}},
		{name: "three arguments", argv: []string{"article", "create", "DEV", "a", "b"}},
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
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// Text YouTrack would keep as something other than what was written never goes out: the write would happen
// and the document would disagree with it, and the caller would be told about an article that by then exists.
// The runes stand in the literals as bytes, since a source file is read by more than one tool.
func TestArticleCreateRefusesTextTheServerWouldRewrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a line feed in the title", argv: []string{"--summary", "a\nb"}},
		{name: "a carriage return in the title", argv: []string{"--summary", "a\rb"}},
		// A CRLF is named by the line feed of it, which is the first rune of the two the check comes to.
		{name: "a CRLF in the title", argv: []string{"--summary", "a\r\nb"}},
		{name: "an empty title", argv: []string{"--summary", ""}},
		{name: "a title that is no UTF-8", argv: []string{"--summary", "a\xffb"}},
		{name: "empty content", argv: []string{"--summary", "x", "--content", ""}},
		{name: "content that is no UTF-8", argv: []string{"--summary", "x", "--content", "a\xffb"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"article", "create", "DEV"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// A creation has the flags it has: nothing reads a value out of a file, and nothing clears a part of an
// article that does not exist yet.
func TestArticleCreateHasNoFlagsBesidesItsOwn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		flag string
	}{
		{name: "content out of a file", flag: "--content-file"},
		{name: "a title out of a file", flag: "--summary-file"},
		{name: "the prose of an issue", flag: "--description"},
		{name: "clearing a part", flag: "--clear"},
		{name: "comments", flag: "--comments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", tc.flag, "y")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The comments of an article are no part of a write, and --clear content belongs to the update, so a
// creation is told so by the name of the flag rather than by a refusal of its own.
func TestArticleCreatePrintsNoComments(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--fields", "+comments(text)")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// The help names what a caller gets where they write no expression at all, and how text already written is
// passed in, since no flag reads it out of a file.
func TestArticleCreateHelpNamesTheDefault(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "create", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, articleShowFields)
	assert.NotContains(t, got.stdout, "-file")
}

// The whole of the command: one request, a body of the project by the code that was typed, the title and
// the content as they were given, and the answer as the document.
func TestArticleCreateFilesTheArticleInOneRequest(t *testing.T) {
	t.Parallel()
	const title = "[bug] fix login"
	text := longContent()
	server := creatingAnArticle(t, respondWith(http.StatusOK, createdArticle("DEV-A-7", title, asJSON(text))))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", title, "--content", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles"}, server.sentPaths())
	assert.Equal(t, []string{askedArticleFields}, server.sentFields())
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

// Every part of a creation is the value of a flag, so pflag hands it over whatever it starts with, and
// nothing of it is read by ytrack: a title of runes YouTrack keeps in an article and drops from an issue goes
// out as it was typed, and so does a body of one dash.
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
		{
			name: "a title that starts with a dash",
			argv: []string{"--summary", "-x"},
			want: map[string]any{"summary": "-x"},
		},
		{
			name: "a title that is the word --help",
			argv: []string{"--summary", "--help"},
			want: map[string]any{"summary": "--help"},
		},
		{
			name: "content that starts with a dash",
			argv: []string{"--summary", "x", "--content", "-x"},
			want: map[string]any{"summary": "x", "content": "-x"},
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
			server := creatingAnArticle(t, respondWith(http.StatusOK, createdArticle("DEV-A-7", title, content)))

			got := runWith(t, server.env(), append([]string{"article", "create", "DEV"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			tc.want["project"] = map[string]any{"shortName": "DEV"}
			assert.Equal(t, tc.want, sentArticle(t, server))
		})
	}
}

// A 200 says the server took the body, not that it kept what was in it. What came back other than as it
// went out is a refusal naming both, and nothing is printed: the article exists and holds something the caller
// did not write, which is what the exit code of a write that happened is for.
func TestArticleCreateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		// The article the answer carries, and the id a refusal names it by, which is that answer's own.
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
			server := creatingAnArticle(t, respondWith(http.StatusOK, tc.filed))

			got := runWith(t, server.env(), append([]string{"article", "create", "DEV"}, tc.argv...)...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleCreationRequest(server.url, askedArticleFields)},
					{"article", tc.article},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

// The content a call never wrote is never held against anything: YouTrack files an article without it, the
// key comes back null, and that is the article the caller asked for.
func TestArticleCreateChecksNoContentWhereNoneWasWritten(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, respondWith(http.StatusOK, createdArticle("DEV-A-7", "x", "null")))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "x"},
		sentArticle(t, server))
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "content")))
}

// What the server says about a body it refused passes on word for word, and no article was filed, so the
// caller may fix the call and send it again.
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
			server := creatingAnArticle(t, respondWith(tc.status, said))

			got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", articleCreationRequest(server.url, askedArticleFields)},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

// The body left whole and the connection went away before an answer: the article may stand in the project
// and may never have been filed, and nothing ytrack could send afterwards tells the two apart — a repeat would
// file a second one. So the caller is told that much, and the exit code says the instance may have changed.
func TestArticleCreateIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, breakOff)

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", articleCreationRequest(server.url, askedArticleFields)}}, found.details)
	assert.Empty(t, got.stdout)
}

// What the caller asks to print and what the check of the write reads are two things: every part that went
// out is asked for whatever the expression says, and only the expression reaches the document.
func TestArticleCreateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := creatingAnArticle(t, respondWith(http.StatusOK, createdArticle("DEV-A-7", "x", asJSON("первая"))))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--content", "первая",
		"--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "idReadable: \"DEV-A-7\"\n", got.stdout)
	assert.Equal(t, []string{"idReadable,summary,content,project(shortName)"}, server.sentFields())
}

func fileArticle(t *testing.T, dev *upstream, summary string, argv ...string) string {
	t.Helper()
	got := runWith(t, dev.env(), append([]string{"article", "create", "DEV", "--summary", summary}, argv...)...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	require.Empty(t, got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-A-[0-9]+$`, readable)
	return readable
}

// removeArticle is the cleanup of a contract test that filed an article: the deletion prints the id it was
// known by, and a read afterwards finds nothing. The context of the test is cancelled before any cleanup runs,
// so these two calls get one of their own or they would leave the article behind.
func removeArticle(t *testing.T, dev *upstream, readable string) {
	t.Helper()
	deleted := runInContext(t, context.Background(), dev.env(), "article", "delete", readable)
	assert.Equal(t, outcome{stdout: "idReadable: " + strconv.Quote(readable) + "\n"}, deleted)

	gone := runInContext(t, context.Background(), dev.env(), "article", "show", readable, "--comments=0")
	assert.Equal(t, "not_found", requireRefusalDocument(t, gone).code)
}

func TestArticleCreateFilesAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	text := hostileContent()

	got := runWith(t, dev.env(), "article", "create", "DEV", "--summary", contractArticleTitle(t), "--content", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	readable := nodeAt(t, mapping, "idReadable").Value
	require.Regexp(t, `^DEV-A-[0-9]+$`, readable)
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeArticle(t, dev, readable) })

	assert.Equal(t, contractArticleTitle(t), nodeAt(t, mapping, "summary").Value)
	assert.Equal(t, text, nodeAt(t, mapping, "content").Value)

	read := runWith(t, dev.env(), "article", "show", readable, "--comments=0", "--fields", "content")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	content := nodeAt(t, requireMapping(t, "stdout", read.stdout), "content")
	assert.Equal(t, text, content.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, content.Style, "a carriage return keeps content out of a literal block")
}

func hostileContent() string {
	return textOfSize(hostileText, 81_033)
}

// The server reads the code in any letter case and answers with the project as it keeps it, so an article
// filed in dev is an article of DEV and the check holds the two the same.
func TestArticleCreateFilesAnArticleInTheProjectOfALowerCaseCode(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "create", "dev", "--summary", contractArticleTitle(t))

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	t.Cleanup(func() { removeArticle(t, dev, readable) })
	assert.Regexp(t, `^DEV-A-[0-9]+$`, readable)
}

func TestArticleCreateRefusesAProjectTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "create", "NOPE", "--summary", contractArticleTitle(t))

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleCreationRequest(dev.url, askedArticleFields)},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Project was not found"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodPost}, sentMethods(dev))
}

// A token that may not write in the project is answered 403 rather than the 404 a read of an article
// hidden from it gets: the knowledge base tells a caller that may not file from one that may not see.
func TestArticleCreateRefusesTheProjectTheLimitedUserMayNotWriteIn(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"article", "create", "DEV", "--summary", contractArticleTitle(t))

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", articleCreationRequest(dev.url, askedArticleFields)},
			{"upstream_status", 403},
			{"upstream_error", "Forbidden"},
			{"upstream_message", "HTTP 403 Forbidden"},
			authFromEnv(),
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodPost}, sentMethods(dev))
}
