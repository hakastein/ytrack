package cli_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func articleUpdateRequest(address, readable, fields string) string {
	return "POST " + address + "/api/articles/" + readable + "?fields=" + fields
}

func updatingAnArticle(t *testing.T, read, update http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			read(w, r)
			return
		}
		if !assert.Equal(t, http.MethodPost, r.Method, "an update sends one GET and one POST") {
			return
		}
		update(w, r)
	})
}

func sentChanges(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body))
	return body
}

func TestArticleUpdateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing to write", argv: []string{"DEV-A-7"}},
		{name: "the id of an issue", argv: []string{"DEV-1", "--summary", "x"}},
		{name: "an internal id", argv: []string{"3-19", "--summary", "x"}},
		{name: "content written and cleared both", argv: []string{"DEV-A-7", "--content", "текст", "--clear", "content"}},
		{
			name: "content cleared under another letter case and written",
			argv: []string{"DEV-A-7", "--content", "текст", "--clear", "CONTENT"},
		},
		{name: "an empty title", argv: []string{"DEV-A-7", "--summary", ""}},
		{name: "a line feed in the title", argv: []string{"DEV-A-7", "--summary", "a\nb"}},
		{name: "a carriage return in the title", argv: []string{"DEV-A-7", "--summary", "a\rb"}},
		{name: "a title that is no UTF-8", argv: []string{"DEV-A-7", "--summary", "a\xffb"}},
		{name: "empty content", argv: []string{"DEV-A-7", "--content", ""}},
		{name: "content that is no UTF-8", argv: []string{"DEV-A-7", "--content", "a\xffb"}},
		{name: "the title cleared", argv: []string{"DEV-A-7", "--clear", "summary"}},
		{name: "a part of no name", argv: []string{"DEV-A-7", "--clear", "bogus"}},
		{name: "no part at all", argv: []string{"DEV-A-7", "--clear", ""}},
		{name: "a title twice", argv: []string{"DEV-A-7", "--summary", "a", "--summary", "b"}},
		{name: "content twice", argv: []string{"DEV-A-7", "--content", "a", "--content", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"article", "update"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestArticleUpdateWritesOnlyThePartsItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want map[string]any
	}{
		{name: "a title alone", argv: []string{"--summary", "x"}, want: map[string]any{"summary": "x"}},
		{
			name: "content alone",
			argv: []string{"--content", "первая\rвторая"},
			want: map[string]any{"content": "первая\rвторая"},
		},
		{
			name: "a title and content both",
			argv: []string{"--summary", "x", "--content", "первая"},
			want: map[string]any{"summary": "x", "content": "первая"},
		},
		{
			name: "content taken away",
			argv: []string{"--clear", "content"},
			want: map[string]any{"content": nil},
		},
		{
			name: "content taken away under another letter case",
			argv: []string{"--clear", "CONTENT"},
			want: map[string]any{"content": nil},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			title, _ := tc.want["summary"].(string)
			text := "null"
			if written, isText := tc.want["content"].(string); isText {
				text = asJSON(written)
			}
			filed := answeredArticle{readable: "DEV-A-7", summary: title, content: text}
			server := updatingAnArticle(t,
				respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				respondWith(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), append([]string{"article", "update", "dev-A-7"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
			assert.Equal(t, []string{"/api/articles/dev-A-7", "/api/articles/DEV-A-7"}, server.sentPaths())
			assert.Equal(t, tc.want, sentChanges(t, server))
		})
	}
}

func TestArticleUpdateChecksTheResponseAgainstThePartsItWrote(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "заголовок, которого никто не писал", content: "null"}
	server := updatingAnArticle(t,
		respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		respondWith(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--clear", "content")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{articleToWriteFields, articleShowFields}, server.sentFields())
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "заголовок, которого никто не писал", nodeAt(t, mapping, "summary").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "content")))
}

func TestArticleUpdateWritesTheTitleItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		title string
	}{
		{name: "a NEL", title: "первая\xc2\x85вторая"},
		{name: "a line separator", title: "первая\xe2\x80\xa8вторая"},
		{name: "a paragraph separator", title: "первая\xe2\x80\xa9вторая"},
		{name: "a tab", title: "первая\tвторая"},
		{name: "spaces around it", title: "  заголовок  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filed := answeredArticle{readable: "DEV-A-7", summary: tc.title, content: "null"}
			server := updatingAnArticle(t,
				respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				respondWith(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--summary", tc.title)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, map[string]any{"summary": tc.title}, sentChanges(t, server))
		})
	}
}

func TestArticleUpdateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		filed answeredArticle
		asked string
	}{
		{
			name:  "a title",
			argv:  []string{"--summary", "x"},
			filed: answeredArticle{readable: "DEV-A-7", summary: "x", content: "null"},
			asked: "idReadable,summary",
		},
		{
			name:  "a title and content both",
			argv:  []string{"--summary", "x", "--content", "первая"},
			filed: answeredArticle{readable: "DEV-A-7", summary: "x", content: asJSON("первая")},
			asked: "idReadable,summary,content",
		},
		{
			name:  "content taken away",
			argv:  []string{"--clear", "content"},
			filed: answeredArticle{readable: "DEV-A-7", summary: "x", content: "null"},
			asked: "idReadable,content",
		},
		{
			name:  "the parent taken away",
			argv:  []string{"--clear", "parent"},
			filed: answeredArticle{readable: "DEV-A-7", summary: "x", content: "null"},
			asked: "idReadable,parentArticle(idReadable)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := updatingAnArticle(t,
				respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				respondWith(http.StatusOK, tc.filed.json()))
			argv := append([]string{"article", "update", "dev-A-7"}, tc.argv...)

			got := runWith(t, server.env(), append(argv, "--fields", "idReadable")...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, "idReadable: \"DEV-A-7\"\n", got.stdout)
			assert.Equal(t, []string{articleToWriteFields, tc.asked}, server.sentFields())
		})
	}
}

func TestArticleUpdateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		argv     []string
		filed    answeredArticle
		mismatch []any
	}{
		{
			name:     "a title the server stored otherwise",
			argv:     []string{"--summary", "a b"},
			filed:    answeredArticle{readable: "DEV-A-7", summary: "a  b", content: "null"},
			mismatch: []any{[]detail{{"field", "summary"}, {"expected", "a b"}, {"actual", "a  b"}}},
		},
		{
			name:  "content the server cut a carriage return out of",
			argv:  []string{"--content", "первая\rвторая"},
			filed: answeredArticle{readable: "DEV-A-7", summary: "x", content: asJSON("перваявторая")},
			mismatch: []any{
				[]detail{{"field", "content"}, {"expected", "первая\rвторая"}, {"actual", "перваявторая"}},
			},
		},
		{
			name:     "content still standing where the call emptied it",
			argv:     []string{"--clear", "content"},
			filed:    answeredArticle{readable: "DEV-A-7", summary: "x", content: asJSON("первая")},
			mismatch: []any{[]detail{{"field", "content"}, {"expected", nil}, {"actual", "первая"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := updatingAnArticle(t,
				respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				respondWith(http.StatusOK, tc.filed.json()))

			got := runWith(t, server.env(), append([]string{"article", "update", "DEV-A-7"}, tc.argv...)...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleUpdateRequest(server.url, "DEV-A-7", articleShowFields)},
					{"article", "DEV-A-7"},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestArticleUpdateRefusesAnArticleTheReadDoesNotFind(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := updatingAnArticle(t, respondWith(http.StatusNotFound, said), noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-99999", "--summary", "x")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleToWriteRequest(server.url, "DEV-A-99999")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Can't find article with id DEV-A-99999"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestArticleUpdateRefusesAReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
	}{
		{name: "two dots", received: `".."`},
		{name: "a path after the id", received: `"DEV-A-7/.."`},
		{name: "the id of an issue", received: `"DEV-1"`},
		{name: "no id at all", received: `""`},
		{name: "a number", received: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Article","id":"177-7","idReadable":` + tc.received +
				`,"project":{"$type":"Project","shortName":"DEV"}}`
			server := updatingAnArticle(t, respondWith(http.StatusOK, body), noUpdate(t))

			got := runWith(t, server.env(), "article", "update", "dev-A-7", "--summary", "x")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleToWriteRequest(server.url, "dev-A-7")},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestArticleUpdateRefusesAParentReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-1": respondWith(http.StatusOK, articleAbove("177-1", "DEV-1", "DEV", rootParent)),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-1")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-1")},
			{"upstream_status", 200},
			{"upstream_body", articleAbove("177-1", "DEV-1", "DEV", rootParent)},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

func TestArticleUpdateIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := updatingAnArticle(t, respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")), breakOff)

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", articleUpdateRequest(server.url, "DEV-A-7", articleShowFields)}},
		found.details)
	assert.Empty(t, got.stdout)
}
