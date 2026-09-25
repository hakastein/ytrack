package youtrack_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
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

const articleAndCommentText = "  First\r\nSecond\rThird   \n---\n~~~\n\u0085\u2028\ufeff\U0001F600\n  "

type articleStep struct{ id, readable string }

var (
	articleWritten = articleStep{id: "177-7", readable: "DEV-A-7"}
	articleParent  = articleStep{id: "177-1", readable: "DEV-A-1"}
	articleChild   = articleStep{id: "177-9", readable: "DEV-A-9"}
	articleBetween = articleStep{id: "177-8", readable: "DEV-A-8"}
	articleOfDEMO  = articleStep{id: "177-50", readable: "DEMO-A-1"}
)

const articleOtherParent = "DEV-A-2"

const (
	articleRootAbove    = "null"
	articleNothingAbove = ""
)

const articleAncestorsPerRequest = 10

func articleLine(project, top string, line ...articleStep) string {
	nested := top
	for at := len(line) - 1; at >= 0; at-- {
		object := `{"$type":"Article","id":` + strconv.Quote(line[at].id) + `,"idReadable":` + strconv.Quote(line[at].readable)
		if at == 0 {
			object += `,"project":{"$type":"Project","shortName":` + strconv.Quote(project) + `}`
		}
		if nested != articleNothingAbove {
			object += `,"parentArticle":` + nested
		}
		nested = object + `}`
	}
	return nested
}

func articleRead(id, readable, project string) string {
	return `{"$type":"Article","id":` + id + `,"idReadable":` + readable + `,"project":` + project + `}`
}

func articleFiled(t *testing.T, changes map[string]any) string {
	t.Helper()
	filed := map[string]any{
		"$type":         "Article",
		"idReadable":    articleWritten.readable,
		"summary":       "Title",
		"content":       nil,
		"project":       map[string]any{"$type": "Project", "shortName": "DEV"},
		"parentArticle": nil,
	}
	maps.Copy(filed, changes)
	encoded, err := json.Marshal(filed)
	require.NoError(t, err)
	return string(encoded)
}

func articleFiledUnder(readable string) map[string]any {
	return map[string]any{"$type": "Article", "idReadable": readable}
}

func articleServer(t *testing.T, reads map[string]string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			write(w, r)
			return
		}
		read, answered := reads[strings.TrimPrefix(r.URL.Path, "/api/articles/")]
		if !assert.True(t, answered, "%s was read and no answer was given for it", r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fake.JSON(http.StatusOK, read)(w, r)
	})
}

func articleNamed(readable string) *render.Node {
	return render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString(readable)})
}

func TestArticleCallsRefuseTheCommentsInTheExpression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func() (youtrack.Call, *diag.Fault)
	}{
		{
			name: "an update, added to the default",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", new("Title"), nil, nil, nil, new("+comments(text)"))
			},
		},
		{
			name: "an update, in place of the default",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", new("Title"), nil, nil, nil, new("idReadable,comments"))
			},
		},
		{
			name: "a creation",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateArticle("DEV", "Title", nil, nil, new("+comments(text)"))
			},
		},
		{
			name: "a show, in place of the default",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowArticle("DEV-A-7", new("comments(text)"), youtrack.AllComments())
			},
		},
		{
			name: "a show, under a child article",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowArticle("DEV-A-7", new("childArticles(comments(id))"), youtrack.AllComments())
			},
		},
		{
			name: "a search",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListArticles("", new("+comments"), youtrack.Page{Limit: 1})
			},
		},
		{
			name: "a list of children",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListChildArticles("DEV-A-7", new("+comments"), youtrack.Page{Limit: 1})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := tc.call()

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}

func TestCreateArticleRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		summary string
		content *string
	}{
		{name: "an empty title", summary: ""},
		{name: "a line feed in the title", summary: "First\nSecond"},
		{name: "a carriage return in the title", summary: "First\rSecond"},
		{name: "a title that is no UTF-8", summary: "First\xffSecond"},
		{name: "empty content", summary: "Title", content: new("")},
		{name: "content that is no UTF-8", summary: "Title", content: new("First\xffSecond")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.CreateArticle("DEV", tc.summary, tc.content, nil, nil)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}

func TestUpdateArticleRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		summary *string
		content *string
		parent  *string
		cleared []string
	}{
		{name: "nothing to write"},
		{name: "content written and taken away", content: new("Text"), cleared: []string{"content"}},
		{name: "content written and taken away in another letter case", content: new("Text"), cleared: []string{"CONTENT"}},
		{name: "a parent written and taken away", parent: new("DEV-A-1"), cleared: []string{"parent"}},
		{name: "a parent written and taken away in another letter case", parent: new("DEV-A-1"), cleared: []string{"PARENT"}},
		{name: "an empty title", summary: new("")},
		{name: "a line feed in the title", summary: new("First\nSecond")},
		{name: "a carriage return in the title", summary: new("First\rSecond")},
		{name: "a title that is no UTF-8", summary: new("First\xffSecond")},
		{name: "empty content", content: new("")},
		{name: "content that is no UTF-8", content: new("First\xffSecond")},
		{name: "the title taken away", cleared: []string{"summary"}},
		{name: "a part of no name taken away", cleared: []string{"bogus"}},
		{name: "no part taken away", cleared: []string{""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.UpdateArticle("DEV-A-7", tc.summary, tc.content, tc.parent, tc.cleared, nil)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}

func TestCreateArticleSendsWhatItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		summary string
		content *string
		parent  *string
		filed   map[string]any
		sent    map[string]any
	}{
		{
			name:    "a title with a tab inside and spaces around",
			summary: "  First\tSecond  ",
			filed:   map[string]any{"summary": "  First\tSecond  "},
			sent:    map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "  First\tSecond  "},
		},
		{
			name:    "a title with a NEL, a line separator and a paragraph separator",
			summary: "First\u0085Second\u2028Third\u2029Fourth",
			filed:   map[string]any{"summary": "First\u0085Second\u2028Third\u2029Fourth"},
			sent: map[string]any{"project": map[string]any{"shortName": "DEV"},
				"summary": "First\u0085Second\u2028Third\u2029Fourth"},
		},
		{
			name:    "a title of spaces alone",
			summary: "   ",
			filed:   map[string]any{"summary": "   "},
			sent:    map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "   "},
		},
		{
			name:    "content the server keeps byte for byte",
			summary: "Title",
			content: new(articleAndCommentText),
			filed:   map[string]any{"content": articleAndCommentText},
			sent: map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "Title",
				"content": articleAndCommentText},
		},
		{
			name:    "a parent, by the internal id the read gave",
			summary: "Title",
			parent:  new("dev-A-1"),
			filed:   map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			sent: map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "Title",
				"parentArticle": map[string]any{"id": articleParent.id}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{"dev-A-1": articleLine("DEV", articleNothingAbove, articleParent)},
				fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))
			call, fault := youtrack.CreateArticle("DEV", tc.summary, tc.content, tc.parent, new("idReadable"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.sent, server.LastJSON(t))
		})
	}
}

func TestUpdateArticleSendsWhatItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		summary *string
		content *string
		parent  *string
		cleared []string
		filed   map[string]any
		sent    map[string]any
	}{
		{
			name:    "a title alone",
			summary: new("  First\tSecond\u2028Third  "),
			filed:   map[string]any{"summary": "  First\tSecond\u2028Third  "},
			sent:    map[string]any{"summary": "  First\tSecond\u2028Third  "},
		},
		{
			name:    "content alone",
			content: new(articleAndCommentText),
			filed:   map[string]any{"content": articleAndCommentText},
			sent:    map[string]any{"content": articleAndCommentText},
		},
		{
			name:    "a title and content both",
			summary: new("Title"),
			content: new("Text"),
			filed:   map[string]any{"content": "Text"},
			sent:    map[string]any{"summary": "Title", "content": "Text"},
		},
		{
			name:    "content taken away",
			cleared: []string{"content"},
			sent:    map[string]any{"content": nil},
		},
		{
			name:    "content taken away in another letter case",
			cleared: []string{"CONTENT"},
			sent:    map[string]any{"content": nil},
		},
		{
			name:    "the parent taken away",
			cleared: []string{"parent"},
			sent:    map[string]any{"parentArticle": nil},
		},
		{
			name:   "a parent, by the internal id the read gave",
			parent: new("dev-A-1"),
			filed:  map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			sent:   map[string]any{"parentArticle": map[string]any{"id": articleParent.id}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"dev-A-1": articleLine("DEV", articleRootAbove, articleParent),
			}, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))
			call, fault := youtrack.UpdateArticle("DEV-A-7", tc.summary, tc.content, tc.parent, tc.cleared, new("idReadable"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.sent, server.LastJSON(t))
		})
	}
}

func TestUpdateArticleWritesByTheReadableIDTheReadGave(t *testing.T) {
	t.Parallel()
	server := articleServer(t, map[string]string{"dev-A-7": articleLine("DEV", articleNothingAbove, articleWritten)},
		fake.JSON(http.StatusOK, articleFiled(t, nil)))
	call, fault := youtrack.UpdateArticle("dev-A-7", new("Title"), nil, nil, nil, new("idReadable"))
	require.Nil(t, fault)

	_, fault = call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, []string{"/api/articles/dev-A-7", "/api/articles/DEV-A-7"}, server.Paths())
}

func TestArticleWritesCheckMoreThanTheyPrint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		call   func() (youtrack.Call, *diag.Fault)
		filed  map[string]any
		fields string
	}{
		{
			name: "a creation",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateArticle("DEV", "Title", nil, nil, new("idReadable"))
			},
			fields: "idReadable,summary,content,project(shortName)",
		},
		{
			name: "a creation under a parent",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateArticle("DEV", "Title", nil, new("DEV-A-1"), new("idReadable"))
			},
			filed:  map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			fields: "idReadable,summary,content,project(shortName),parentArticle(idReadable)",
		},
		{
			name: "an update of the title",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", new("Title"), nil, nil, nil, new("idReadable"))
			},
			fields: "idReadable,summary",
		},
		{
			name: "an update of the title and content",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", new("Title"), new("Text"), nil, nil, new("idReadable"))
			},
			filed:  map[string]any{"content": "Text"},
			fields: "idReadable,summary,content",
		},
		{
			name: "content taken away",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", nil, nil, nil, []string{"content"}, new("idReadable"))
			},
			fields: "idReadable,content",
		},
		{
			name: "the parent taken away",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", nil, nil, nil, []string{"parent"}, new("idReadable"))
			},
			fields: "idReadable,parentArticle(idReadable)",
		},
		{
			name: "an update of the parent",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEV-A-1"), nil, new("idReadable"))
			},
			filed:  map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			fields: "idReadable,parentArticle(idReadable)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-1": articleLine("DEV", articleRootAbove, articleParent),
			}, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))
			call, fault := tc.call()
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, articleNamed(articleWritten.readable), node)
			assert.Equal(t, tc.fields, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestCreateArticleRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		summary  string
		content  *string
		parent   *string
		filed    map[string]any
		article  string
		mismatch []*render.Node
	}{
		{
			name:     "a title the server stored in another letter case",
			summary:  "Title",
			filed:    map[string]any{"summary": "title"},
			article:  articleWritten.readable,
			mismatch: []*render.Node{mismatch("summary", render.NewString("Title"), render.NewString("title"))},
		},
		{
			name:     "content the server kept none of",
			summary:  "Title",
			content:  new("Text"),
			article:  articleWritten.readable,
			mismatch: []*render.Node{mismatch("content", render.NewString("Text"), render.NewNull())},
		},
		{
			name:    "content the server cut a carriage return out of",
			summary: "Title",
			content: new("First\rSecond"),
			filed:   map[string]any{"content": "FirstSecond"},
			article: articleWritten.readable,
			mismatch: []*render.Node{
				mismatch("content", render.NewString("First\rSecond"), render.NewString("FirstSecond")),
			},
		},
		{
			name:    "an article filed in another project",
			summary: "Title",
			filed: map[string]any{"idReadable": articleOfDEMO.readable,
				"project": map[string]any{"$type": "Project", "shortName": "DEMO"}},
			article:  articleOfDEMO.readable,
			mismatch: []*render.Node{mismatch("project", render.NewString("DEV"), render.NewString("DEMO"))},
		},
		{
			name:     "an article filed in no project",
			summary:  "Title",
			filed:    map[string]any{"project": nil},
			article:  articleWritten.readable,
			mismatch: []*render.Node{mismatch("project", render.NewString("DEV"), render.NewNull())},
		},
		{
			name:    "the title and the content both",
			summary: "Title",
			content: new("Text"),
			filed:   map[string]any{"summary": "title", "content": "text"},
			article: articleWritten.readable,
			mismatch: []*render.Node{
				mismatch("summary", render.NewString("Title"), render.NewString("title")),
				mismatch("content", render.NewString("Text"), render.NewString("text")),
			},
		},
		{
			name:    "no parent where the call named one",
			summary: "Title",
			parent:  new("DEV-A-1"),
			article: articleWritten.readable,
			mismatch: []*render.Node{
				mismatch("parentArticle", render.NewString(articleParent.readable), render.NewNull()),
			},
		},
		{
			name:    "another parent than the one the call named",
			summary: "Title",
			parent:  new("DEV-A-1"),
			filed:   map[string]any{"parentArticle": articleFiledUnder(articleOtherParent)},
			article: articleWritten.readable,
			mismatch: []*render.Node{
				mismatch("parentArticle", render.NewString(articleParent.readable), render.NewString(articleOtherParent)),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{"DEV-A-1": articleLine("DEV", articleNothingAbove, articleParent)},
				fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))
			call, fault := youtrack.CreateArticle("DEV", tc.summary, tc.content, tc.parent, new("idReadable"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "article", Value: render.NewString(tc.article)},
				{Key: "mismatch", Value: render.NewList(tc.mismatch...)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestUpdateArticleRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		summary  *string
		content  *string
		parent   *string
		cleared  []string
		filed    map[string]any
		mismatch *render.Node
	}{
		{
			name:     "a title the server stored in another letter case",
			summary:  new("Title"),
			filed:    map[string]any{"summary": "title"},
			mismatch: mismatch("summary", render.NewString("Title"), render.NewString("title")),
		},
		{
			name:     "content the server cut a carriage return out of",
			content:  new("First\rSecond"),
			filed:    map[string]any{"content": "FirstSecond"},
			mismatch: mismatch("content", render.NewString("First\rSecond"), render.NewString("FirstSecond")),
		},
		{
			name:     "content still standing where the call took it away",
			cleared:  []string{"content"},
			filed:    map[string]any{"content": "Text"},
			mismatch: mismatch("content", render.NewNull(), render.NewString("Text")),
		},
		{
			name:     "a parent still standing where the call took it away",
			cleared:  []string{"parent"},
			filed:    map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			mismatch: mismatch("parentArticle", render.NewNull(), render.NewString(articleParent.readable)),
		},
		{
			name:     "no parent where the call wrote one",
			parent:   new("DEV-A-1"),
			mismatch: mismatch("parentArticle", render.NewString(articleParent.readable), render.NewNull()),
		},
		{
			name:     "another parent than the one the call wrote",
			parent:   new("DEV-A-1"),
			filed:    map[string]any{"parentArticle": articleFiledUnder(articleOtherParent)},
			mismatch: mismatch("parentArticle", render.NewString(articleParent.readable), render.NewString(articleOtherParent)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-1": articleLine("DEV", articleRootAbove, articleParent),
			}, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))
			call, fault := youtrack.UpdateArticle("DEV-A-7", tc.summary, tc.content, tc.parent, tc.cleared, new("idReadable"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "article", Value: render.NewString(articleWritten.readable)},
				{Key: "mismatch", Value: render.NewList(tc.mismatch)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestArticleWritesTakeAProjectInAnyLetterCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		call  func() (youtrack.Call, *diag.Fault)
		reads map[string]string
		filed map[string]any
	}{
		{
			name: "a creation the answer files under the code in capitals",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateArticle("dev", "Title", nil, nil, new("idReadable"))
			},
		},
		{
			name: "a creation under a parent of the code in capitals",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateArticle("dev", "Title", nil, new("DEV-A-1"), new("idReadable"))
			},
			reads: map[string]string{"DEV-A-1": articleLine("DEV", articleNothingAbove, articleParent)},
			filed: map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
		},
		{
			name: "a move under a parent whose code the read spelled otherwise",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEV-A-1"), nil, new("idReadable"))
			},
			reads: map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-1": articleLine("dev", articleRootAbove, articleParent),
			},
			filed: map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, tc.reads, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))
			call, fault := tc.call()
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, articleNamed(articleWritten.readable), node)
		})
	}
}

func TestArticleWritesRefuseAParentOfAnotherProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func() (youtrack.Call, *diag.Fault)
	}{
		{
			name: "a creation",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateArticle("DEV", "Title", nil, new("DEMO-A-1"), nil)
			},
		},
		{
			name: "a move",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEMO-A-1"), nil, nil)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7":  articleLine("DEV", articleNothingAbove, articleWritten),
				"DEMO-A-1": articleLine("DEMO", articleRootAbove, articleOfDEMO),
			}, fake.Unexpected(t))
			call, fault := tc.call()
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.BadUsage, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "project", Value: render.NewString("DEV")},
				{Key: "parent", Value: render.NewString(articleOfDEMO.readable)},
				{Key: "parent_project", Value: render.NewString("DEMO")},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestUpdateArticleRefusesAnArticleItCannotWriteByTheRead(t *testing.T) {
	t.Parallel()
	const project = `{"$type":"Project","shortName":"DEV"}`
	tests := []struct {
		name string
		read string
	}{
		{name: "two dots for a readable id", read: articleRead(`"177-7"`, `".."`, project)},
		{name: "a path after the readable id", read: articleRead(`"177-7"`, `"DEV-A-7/.."`, project)},
		{name: "the readable id of an issue", read: articleRead(`"177-7"`, `"DEV-1"`, project)},
		{name: "an empty readable id", read: articleRead(`"177-7"`, `""`, project)},
		{name: "a number for a readable id", read: articleRead(`"177-7"`, `7`, project)},
		{name: "no internal id", read: articleRead(`null`, `"DEV-A-7"`, project)},
		{name: "no project", read: articleRead(`"177-7"`, `"DEV-A-7"`, `null`)},
		{name: "a project of no code", read: articleRead(`"177-7"`, `"DEV-A-7"`, `{"$type":"Project","shortName":null}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{"DEV-A-7": tc.read}, fake.Unexpected(t))
			call, fault := youtrack.UpdateArticle("DEV-A-7", new("Title"), nil, nil, nil, nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(tc.read)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestUpdateArticleRefusesAParentThatClosesTheLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		parent string
		line   string
		chain  []*render.Node
	}{
		{
			name:   "the article itself",
			parent: "dev-A-7",
			line:   articleLine("DEV", articleRootAbove, articleWritten),
			chain:  []*render.Node{render.NewString("DEV-A-7")},
		},
		{
			name:   "an article written under it",
			parent: "DEV-A-9",
			line:   articleLine("DEV", articleRootAbove, articleChild, articleBetween, articleWritten),
			chain:  []*render.Node{render.NewString("DEV-A-9"), render.NewString("DEV-A-8"), render.NewString("DEV-A-7")},
		},
		{
			name:   "an article written under it, with a root above them both",
			parent: "DEV-A-9",
			line:   articleLine("DEV", articleRootAbove, articleChild, articleWritten, articleParent),
			chain:  []*render.Node{render.NewString("DEV-A-9"), render.NewString("DEV-A-7")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				tc.parent: tc.line,
			}, fake.Unexpected(t))
			call, fault := youtrack.UpdateArticle("DEV-A-7", nil, nil, &tc.parent, nil, nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.BadUsage, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "article", Value: render.NewString(articleWritten.readable)},
				{Key: "parent", Value: tc.chain[0]},
				{Key: "chain", Value: render.NewList(tc.chain...)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestUpdateArticleRefusesALineOfParentsTheServerBrokeOff(t *testing.T) {
	t.Parallel()
	deepest := articleStep{id: "177-39", readable: "DEV-A-39"}
	tests := []struct {
		name  string
		reads map[string]string
	}{
		{
			name: "at the parent",
			reads: map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-9": articleLine("DEV", articleNothingAbove, articleChild),
			},
		},
		{
			name: "where the line is read on",
			reads: map[string]string{
				"DEV-A-7":  articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-9":  articleLine("DEV", articleNothingAbove, articleLineAbove(articleChild, articleAncestorsPerRequest)...),
				deepest.id: articleLine("DEV", articleNothingAbove, deepest),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, tc.reads, fake.Unexpected(t))
			call, fault := youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEV-A-9"), nil, nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			missing := render.NewMap(
				render.Pair{Key: "field", Value: render.NewString("parentArticle")},
				render.Pair{Key: "type", Value: render.NewString("Article")})
			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "fields", Value: render.NewString(server.Last(t).URL.Query().Get("fields"))},
				{Key: "missing", Value: render.NewList(missing)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func articleLineAbove(top articleStep, above int) []articleStep {
	line := []articleStep{top}
	for at := range above {
		line = append(line, articleStep{id: "177-" + strconv.Itoa(30+at), readable: "DEV-A-" + strconv.Itoa(30+at)})
	}
	return line
}

func TestUpdateArticleRefusesAnAncestorItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
	}{
		{
			name: "an ancestor with no internal id",
			line: `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9","project":{"$type":"Project","shortName":"DEV"},` +
				`"parentArticle":{"$type":"Article","id":null,"idReadable":"DEV-A-8","parentArticle":null}}`,
		},
		{
			name: "an ancestor with no readable id",
			line: `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9","project":{"$type":"Project","shortName":"DEV"},` +
				`"parentArticle":{"$type":"Article","id":"177-8","idReadable":null,"parentArticle":null}}`,
		},
		{
			name: "a parent with the readable id of an issue",
			line: articleLine("DEV", articleRootAbove, articleStep{id: "177-9", readable: "DEV-9"}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-9": tc.line,
			}, fake.Unexpected(t))
			call, fault := youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEV-A-9"), nil, nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(tc.line)},
			}}
			assert.Equal(t, want, faultOf(t, fault))
		})
	}
}

func TestUpdateArticleRefusesAParentOfAnotherShape(t *testing.T) {
	t.Parallel()
	line := `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9","project":{"$type":"Project","shortName":"DEV"},` +
		`"parentArticle":[]}`
	server := articleServer(t, map[string]string{
		"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
		"DEV-A-9": line,
	}, fake.Unexpected(t))
	call, fault := youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEV-A-9"), nil, nil)
	require.Nil(t, fault)

	_, fault = call(t.Context(), client(t, server))

	assert.Equal(t, unreadable(lastRequest(t, server), line), faultOf(t, fault))
}

func TestUpdateArticleRefusesALineThatRepeatsAnArticle(t *testing.T) {
	t.Parallel()
	server := articleServer(t, map[string]string{
		"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
		"DEV-A-9": articleLine("DEV", articleRootAbove, articleChild, articleBetween, articleChild),
	}, fake.Unexpected(t))
	call, fault := youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEV-A-9"), nil, nil)
	require.Nil(t, fault)

	_, fault = call(t.Context(), client(t, server))

	want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
		lastRequest(t, server),
		{Key: "article", Value: render.NewString(articleChild.readable)},
	}}
	assert.Equal(t, want, faultOf(t, fault))
}

func TestUpdateArticleReadsTheLineOnWhereItIsDeeperThanOneRequest(t *testing.T) {
	t.Parallel()
	deepest := articleStep{id: "177-39", readable: "DEV-A-39"}
	server := articleServer(t, map[string]string{
		"DEV-A-7":  articleLine("DEV", articleNothingAbove, articleWritten),
		"DEV-A-9":  articleLine("DEV", articleNothingAbove, articleLineAbove(articleChild, articleAncestorsPerRequest)...),
		deepest.id: articleLine("DEV", articleRootAbove, deepest, articleParent),
	}, fake.JSON(http.StatusOK, articleFiled(t, map[string]any{"parentArticle": articleFiledUnder(articleChild.readable)})))
	call, fault := youtrack.UpdateArticle("DEV-A-7", nil, nil, new("DEV-A-9"), nil, new("idReadable"))
	require.Nil(t, fault)

	_, fault = call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-9", "/api/articles/" + deepest.id,
		"/api/articles/DEV-A-7"}, server.Paths())
	assert.Equal(t, server.Request(t, 1).URL.Query().Get("fields"), server.Request(t, 2).URL.Query().Get("fields"))
	assert.Equal(t, map[string]any{"parentArticle": map[string]any{"id": articleChild.id}}, server.LastJSON(t))
}

func articleComment(id string, created int64, text string) map[string]any {
	return map[string]any{
		"$type":   "ArticleComment",
		"id":      id,
		"author":  map[string]any{"$type": "User", "login": "author"},
		"created": created,
		"text":    text,
	}
}

func articleCommentPrinted(id, created, text string) *render.Node {
	return render.NewMap(
		render.Pair{Key: "id", Value: render.NewString(id)},
		render.Pair{Key: "author", Value: render.NewMap(render.Pair{Key: "login", Value: render.NewString("author")})},
		render.Pair{Key: "created", Value: render.NewString(created)},
		render.Pair{Key: "text", Value: render.NewText(text)})
}

func TestShowArticlePrintsTheCommentsOldestFirst(t *testing.T) {
	t.Parallel()
	first := articleCommentPrinted("8-3", "1970-01-01T00:00:01Z", "First")
	second := articleCommentPrinted("8-2", "1970-01-01T00:00:02Z", "Second")
	third := articleCommentPrinted("8-1", "1970-01-01T00:00:03Z", "Third")
	tests := []struct {
		name     string
		comments string
		want     *render.Node
	}{
		{
			name:     "every one of them",
			comments: "all",
			want: render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-A-7")},
				render.Pair{Key: "comments", Value: render.NewList(first, second, third)}),
		},
		{
			name:     "the last two",
			comments: "2",
			want: render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-A-7")},
				render.Pair{Key: "comments", Value: render.NewList(second, third)}),
		},
		{
			name:     "more than there are",
			comments: "5",
			want: render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-A-7")},
				render.Pair{Key: "comments", Value: render.NewList(first, second, third)}),
		},
		{
			name:     "none at all",
			comments: "0",
			want:     articleNamed("DEV-A-7"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			article, err := json.Marshal(map[string]any{"$type": "Article", "idReadable": "DEV-A-7", "comments": []any{
				articleComment("8-2", 2000, "Second"),
				articleComment("8-3", 1000, "First"),
				articleComment("8-1", 3000, "Third"),
			}})
			require.NoError(t, err)
			server := fake.Serve(t, fake.JSON(http.StatusOK, string(article)))
			comments, err := youtrack.ParseComments(tc.comments)
			require.NoError(t, err)
			call, fault := youtrack.ShowArticle("DEV-A-7", new("idReadable"), comments)
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.want, node)
		})
	}
}

func TestShowArticleAsksForTheCommentsItPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		comments string
		fields   string
	}{
		{name: "every one of them", comments: "all", fields: "idReadable,comments(id,author(login),created,text)"},
		{name: "the last one", comments: "1", fields: "idReadable,comments(id,author(login),created,text)"},
		{name: "none at all", comments: "0", fields: "idReadable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Article","idReadable":"DEV-A-7","comments":[]}`))
			comments, err := youtrack.ParseComments(tc.comments)
			require.NoError(t, err)
			call, fault := youtrack.ShowArticle("DEV-A-7", new("idReadable"), comments)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, []string{tc.fields}, server.Fields())
		})
	}
}

func TestListArticlesSendsTheSearchAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "spaces around it", search: "  project: DEV  "},
		{name: "a tab and a line feed around it", search: "\tproject: DEV\n"},
		{name: "a line separator inside", search: "title: First\u2028Second"},
		{name: "brackets that open and never close", search: "(((("},
		{name: "a saved search of the language of issues", search: "#Unresolved"},
		{name: "characters a query escapes", search: "title: a&b=c?d#e%20+f"},
		{name: "an empty search", search: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))
			call, fault := youtrack.ListArticles(tc.search, new("idReadable"), youtrack.Page{Limit: 1})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, []string{"/api/articles"}, server.Paths())
			assert.Equal(t, []url.Values{{"fields": {"idReadable"}, "$top": {"1"}, "query": {tc.search}}}, server.Queries())
		})
	}
}

func TestListArticlesRefusesASearchThatIsNoUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "a byte that is no UTF-8", search: "\xff"},
		{name: "a truncated sequence inside a search that parses", search: "title: \xc3\x28"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ListArticles(tc.search, nil, youtrack.Page{Limit: 1})

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, faultOf(t, fault))
		})
	}
}
