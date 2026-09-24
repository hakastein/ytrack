package cli_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// The request an update goes out as, which is the one a refusal about the write names. The id is the readable
// one the read before it gave, so nothing of it needs escaping.
func articleUpdateRequest(address, readable, fields string) string {
	return "POST " + address + "/api/articles/" + readable + "?fields=" + fields
}

// updatingAnArticle is the server of an update: the read of the article and the write itself, each answered by
// the scenario, and nothing else reaches it.
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

// The body of the write, read as JSON reads it, which is what tells a key left out from one sent null.
func sentChanges(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(u)), &body))
	return body
}

// Everything an update settles before the network: how many ids it takes, what an id may look like, that it
// writes something at all, and that no two flags say opposite things about one part.
func TestArticleUpdateRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no id", argv: []string{}},
		{name: "the title as an argument", argv: []string{"DEV-A-7", "Заголовок"}},
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// An update has the flags it has: nothing reads a value out of a file, and the custom fields and the prose
// of an issue are no parts of an article.
func TestArticleUpdateHasNoFlagsBesidesItsOwn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		flag string
	}{
		{name: "content out of a file", flag: "--content-file"},
		{name: "the prose of an issue", flag: "--description"},
		{name: "a custom field", flag: "--field"},
		{name: "comments", flag: "--comments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "article", "update", "DEV-A-7", tc.flag, "y")

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The help names what a caller gets where they write no expression, how text already written is passed in,
// and the one way to take the text away.
func TestArticleUpdateHelpNamesTheDefault(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "update", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, articleShowFields)
	assert.NotContains(t, got.stdout, "-file")
}

// The whole of the command: the article is read first, the write goes to the id that read gave rather than
// to the string the caller typed, and the body carries the one part the call named and not a key more.
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
		{
			name: "a title that starts with a dash",
			argv: []string{"--summary", "-x"},
			want: map[string]any{"summary": "-x"},
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
				answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				answer(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), append([]string{"article", "update", "dev-A-7"}, tc.argv...)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
			assert.Equal(t, []string{"/api/articles/dev-A-7", "/api/articles/DEV-A-7"}, server.sentPaths())
			assert.Equal(t, tc.want, sentChanges(t, server))
		})
	}
}

// A title the call does not write leaves its key out of the body, and the answer is then held to nothing
// about it: the article keeps the title it had, whatever that is.
func TestArticleUpdateHoldsTheAnswerToThePartsItWrote(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "заголовок, которого никто не писал", content: "null"}
	server := updatingAnArticle(t,
		answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		answer(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--clear", "content")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{articleToWriteFields, articleShowFields}, server.sentFields())
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "заголовок, которого никто не писал", nodeAt(t, mapping, "summary").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "content")))
}

// The title of an article is held to the runes an article keeps and not to the runes an issue keeps: a NEL,
// the two separators and a tab are stored byte for byte here where an issue loses them, so an update carries
// them out as they were typed.
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
				answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				answer(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--summary", tc.title)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, map[string]any{"summary": tc.title}, sentChanges(t, server))
		})
	}
}

// What the caller asks to print is one thing and what the check of the write needs is another: every part
// that went out is asked for beside the expression, or the answer would carry nothing to hold the write
// against, and a write that went through would be refused for a value the server never sent.
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
				answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				answer(http.StatusOK, tc.filed.json()))
			argv := append([]string{"article", "update", "dev-A-7"}, tc.argv...)

			got := runWith(t, server.env(), append(argv, "--fields", "idReadable")...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, "idReadable: \"DEV-A-7\"\n", got.stdout)
			assert.Equal(t, []string{articleToWriteFields, tc.asked}, server.sentFields())
		})
	}
}

// A 200 says the server took the body, not that it kept what was in it. What came back other than as it
// went out is a refusal naming both, and nothing is printed: the article holds something the caller did not
// write, which is what the exit code of a write that happened is for.
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
			mismatch: []any{[]detail{{"field", "summary"}, {"written", "a b"}, {"arrived", "a  b"}}},
		},
		{
			name:  "content the server cut a carriage return out of",
			argv:  []string{"--content", "первая\rвторая"},
			filed: answeredArticle{readable: "DEV-A-7", summary: "x", content: asJSON("перваявторая")},
			mismatch: []any{
				[]detail{{"field", "content"}, {"written", "первая\rвторая"}, {"arrived", "перваявторая"}},
			},
		},
		{
			name:     "content still standing where the call emptied it",
			argv:     []string{"--clear", "content"},
			filed:    answeredArticle{readable: "DEV-A-7", summary: "x", content: asJSON("первая")},
			mismatch: []any{[]detail{{"field", "content"}, {"written", nil}, {"arrived", "первая"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := updatingAnArticle(t,
				answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				answer(http.StatusOK, tc.filed.json()))

			got := runWith(t, server.env(), append([]string{"article", "update", "DEV-A-7"}, tc.argv...)...)

			want := refusal{
				code: "upstream_lied",
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

// An article the read does not find is a refusal and nothing else: the write never goes out, so a number
// nobody used is answered before anything is changed.
func TestArticleUpdateRefusesAnArticleTheReadDoesNotFind(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := updatingAnArticle(t, answer(http.StatusNotFound, said), noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-99999", "--summary", "x")

	want := refusal{
		code: "not_found",
		details: []detail{
			{"request", articleToWriteRequest(server.url, "DEV-A-99999")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Can't find article with id DEV-A-99999"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// The id the read gave is sent straight out as the path segment of the write, so it is held to the form
// ytrack sends before anything goes: the generated client would resolve ".." against the endpoint and reach
// /api/, where the body would be written to something nobody addressed, and the id of an issue would carry the
// write to an entity of another kind entirely.
func TestArticleUpdateRefusesAReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrived string
	}{
		{name: "two dots", arrived: `".."`},
		{name: "a path after the id", arrived: `"DEV-A-7/.."`},
		{name: "the id of an issue", arrived: `"DEV-1"`},
		{name: "no id at all", arrived: `""`},
		{name: "a number", arrived: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Article","id":"177-7","idReadable":` + tc.arrived +
				`,"project":{"$type":"Project","shortName":"DEV"}}`
			server := updatingAnArticle(t, answer(http.StatusOK, body), noUpdate(t))

			got := runWith(t, server.env(), "article", "update", "dev-A-7", "--summary", "x")

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", articleToWriteRequest(server.url, "dev-A-7")},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// The parent of a move is named by the readable id the read gave in every refusal and in the check of the
// write, so it is held to the form of an article there too, whatever the body addresses it by.
func TestArticleUpdateRefusesAParentReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-1": answer(http.StatusOK, articleAbove("177-1", "DEV-1", "DEV", "null")),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-1")

	want := refusal{
		code: "upstream_lied",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-1")},
			{"upstream_status", 200},
			{"upstream_body", articleAbove("177-1", "DEV-1", "DEV", "null")},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

// The body left whole and the connection went away before an answer: the article may hold what was written
// and may hold what it held, and nothing ytrack could send afterwards tells the two apart. So the caller is
// told that much, and the exit code says the instance may have changed.
func TestArticleUpdateIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := updatingAnArticle(t, answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")), breakOff)

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", articleUpdateRequest(server.url, "DEV-A-7", articleShowFields)}},
		found.details)
	assert.Empty(t, got.stdout)
}

// An article of the polygon written into for real: a title of the runes an article keeps and an issue loses,
// and content holding a carriage return and a line separator, come back byte for byte both in the answer to the
// write and in the article as ytrack reads it afterwards.
func TestArticleUpdateWritesIntoAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	filed := fileArticle(t, dev, contractArticleTitle(t), "--content", "Текст, который будет переписан.")
	t.Cleanup(func() { removeArticle(t, dev, filed) })
	// The title keeps the trailing whitespace and the tab YouTrack stores as they were written; the leading
	// words stay as they are so that an article left behind is still found by the title every contract test uses.
	title := contractArticleTitle(t) + " переписанный  \t"
	text := "первая\rвторая\xe2\x80\xa8третья\n"

	got := runWith(t, dev.env(), "article", "update", filed, "--summary", title, "--content", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, filed, nodeAt(t, mapping, "idReadable").Value)
	assert.Equal(t, title, nodeAt(t, mapping, "summary").Value)
	assert.Equal(t, text, nodeAt(t, mapping, "content").Value)

	read := runWith(t, dev.env(), "article", "show", filed, "--comments=0", "--fields", "summary,content")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	after := requireMapping(t, "stdout", read.stdout)
	assert.Equal(t, title, nodeAt(t, after, "summary").Value)
	content := nodeAt(t, after, "content")
	assert.Equal(t, text, content.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, content.Style, "a carriage return keeps content out of a literal block")
}

// The polygon keeps no empty content: what --clear content sends is a null, and a null is what comes back.
func TestArticleUpdateEmptiesTheContentOfAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	filed := fileArticle(t, dev, contractArticleTitle(t), "--content", "Текст, который будет снят.")
	t.Cleanup(func() { removeArticle(t, dev, filed) })

	got := runWith(t, dev.env(), "article", "update", filed, "--clear", "content")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Nil(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", got.stdout), "content")))

	read := runWith(t, dev.env(), "article", "show", filed, "--comments=0", "--fields", "content")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	assert.Nil(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", read.stdout), "content")))
}

// An article the limited token may not see is a 404 to it, and the read before the write is where that
// lands: one request goes out, the write never does, and the article keeps the title the admin gave it.
func TestArticleUpdateWritesNothingForTheLimitedToken(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	title := contractArticleTitle(t)
	filed := fileArticle(t, dev, title)
	t.Cleanup(func() { removeArticle(t, dev, filed) })

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"article", "update", filed, "--summary", title+" переписанный урезанным токеном")

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, detail{"request", articleToWriteRequest(dev.url, filed)}, found.details[0])

	read := runWith(t, dev.env(), "article", "show", filed, "--comments=0", "--fields", "summary")
	require.Equal(t, 0, read.code, "stderr: %s", read.stderr)
	assert.Equal(t, title, nodeAt(t, requireMapping(t, "stdout", read.stdout), "summary").Value)
}
