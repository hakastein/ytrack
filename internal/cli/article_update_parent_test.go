package cli_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ancestorsPerRequest = 10

func articleLineFields() string {
	line := "parentArticle(id,idReadable)"
	for range ancestorsPerRequest - 1 {
		line = "parentArticle(id,idReadable," + line + ")"
	}
	return articleToWriteFields + "," + line
}

func articleLineRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + articleLineFields()
}

func sentSince(u *upstream, mark int) []string {
	return sentMethods(u)[mark:]
}

type ancestor struct{ id, readable string }

const (
	rootParent    = "null"
	parentNotSent = ""
)

func nestedLine(topParent string, steps []ancestor) string {
	nested := topParent
	for at := len(steps) - 1; at >= 0; at-- {
		body := `{"$type":"Article","id":` + strconv.Quote(steps[at].id) +
			`,"idReadable":` + strconv.Quote(steps[at].readable)
		if nested != parentNotSent {
			body += `,"parentArticle":` + nested
		}
		nested = body + `}`
	}
	return nested
}

func articleAbove(id, readable, project, topParent string, steps ...ancestor) string {
	body := `{"$type":"Article","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":` + strconv.Quote(project) + `}`
	if line := nestedLine(topParent, steps); line != parentNotSent {
		body += `,"parentArticle":` + line
	}
	return body + `}`
}

func rootOfDEV(id, readable string) string {
	return articleAbove(id, readable, "DEV", rootParent)
}

func movingAnArticle(t *testing.T, reads map[string]http.HandlerFunc, update http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			update(w, r)
			return
		}
		if !assert.Equal(t, http.MethodGet, r.Method, "an update reads and writes and does nothing else") {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/articles/")
		read, expected := reads[id]
		if !assert.True(t, expected, "the article %q was read and no answer was given for it", id) {
			return
		}
		read(w, r)
	})
}

func TestArticleUpdateRefusesAParentBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a parent written and taken away both", argv: []string{"--parent", "DEV-A-2", "--clear", "parent"}},
		{
			name: "a parent written and taken away under another letter case",
			argv: []string{"--parent", "DEV-A-2", "--clear", "PARENT"},
		},
		{name: "the id of an issue for a parent", argv: []string{"--parent", "DEV-1"}},
		{name: "an internal id for a parent", argv: []string{"--parent", "177-1"}},
		{name: "the marker of the parent in lower case", argv: []string{"--parent", "DEV-a-1"}},
		{name: "no parent at all", argv: []string{"--parent", ""}},
		{name: "a parent twice", argv: []string{"--parent", "DEV-A-1", "--parent", "DEV-A-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"article", "update", "DEV-A-7"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestArticleUpdateHelpNamesTheParentFlagAndWhatClearTakes(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "update", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "--parent")
	assert.Contains(t, got.stdout, "content or parent")
}

func TestArticleUpdateMovesAnArticleUnderTheIDTheReadGave(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: parentNamed("DEV-A-1")}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"dev-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"dev-A-1": respondWith(http.StatusOK, rootOfDEV("177-1", "DEV-A-1")),
	}, respondWith(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "dev-A-7", "--parent", "dev-A-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles/dev-A-7", "/api/articles/dev-A-1", "/api/articles/DEV-A-7"},
		server.sentPaths())
	assert.Equal(t, []string{articleToWriteFields, articleLineFields(), articleShowFields}, server.sentFields())
	assert.Equal(t, map[string]any{"parentArticle": map[string]any{"id": "177-1"}}, sentChanges(t, server))
	assert.Equal(t, "DEV-A-1", nodeAt(t, requireMapping(t, "stdout", got.stdout), "parentArticle", "idReadable").Value)
}

func TestArticleUpdateAsksForTheParentWhateverTheExpressionSays(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: parentNamed("DEV-A-1")}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-1": respondWith(http.StatusOK, rootOfDEV("177-1", "DEV-A-1")),
	}, respondWith(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-1", "--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "idReadable: \"DEV-A-7\"\n", got.stdout)
	assert.Equal(t, []string{articleToWriteFields, articleLineFields(), "idReadable,parentArticle(idReadable)"},
		server.sentFields())
}

func TestArticleUpdateRefusesAParentTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7":     respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-99999": respondWith(http.StatusNotFound, said),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-99999")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-99999")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Can't find article with id DEV-A-99999"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-99999"}, server.sentPaths())
}

func TestArticleUpdateRefusesAParentThatClosesTheLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		parent   string
		readable string
		found    string
		chain    []any
	}{
		{
			name:     "the article itself",
			parent:   "dev-A-7",
			readable: "DEV-A-7",
			found:    articleAbove("177-7", "DEV-A-7", "DEV", rootParent),
			chain:    []any{"DEV-A-7"},
		},
		{
			name:     "an article written under it",
			parent:   "DEV-A-9",
			readable: "DEV-A-9",
			found: articleAbove("177-9", "DEV-A-9", "DEV", rootParent,
				ancestor{id: "177-8", readable: "DEV-A-8"}, ancestor{id: "177-7", readable: "DEV-A-7"}),
			chain: []any{"DEV-A-9", "DEV-A-8", "DEV-A-7"},
		},
		{
			name:     "an article written under it, with a root above them both",
			parent:   "DEV-A-9",
			readable: "DEV-A-9",
			found: articleAbove("177-9", "DEV-A-9", "DEV", rootParent,
				ancestor{id: "177-7", readable: "DEV-A-7"}, ancestor{id: "177-1", readable: "DEV-A-1"}),
			chain: []any{"DEV-A-9", "DEV-A-7"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := movingAnArticle(t, map[string]http.HandlerFunc{
				"DEV-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				tc.parent: respondWith(http.StatusOK, tc.found),
			}, noUpdate(t))

			got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", tc.parent)

			want := faultDocument{
				code: "bad_usage",
				details: []detail{
					{"request", articleLineRequest(server.url, tc.parent)},
					{"article", "DEV-A-7"},
					{"parent", tc.readable},
					{"chain", tc.chain},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
		})
	}
}

func TestArticleUpdateReadsOnWhereTheLineIsDeeperThanOneRequest(t *testing.T) {
	t.Parallel()
	steps := make([]ancestor, 0, ancestorsPerRequest)
	for at := range ancestorsPerRequest {
		steps = append(steps, ancestor{id: "177-" + strconv.Itoa(30+at), readable: "DEV-A-" + strconv.Itoa(30+at)})
	}
	deepest := steps[len(steps)-1]
	filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: parentNamed("DEV-A-20")}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7":  respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-20": respondWith(http.StatusOK, articleAbove("177-20", "DEV-A-20", "DEV", parentNotSent, steps...)),
		deepest.id: respondWith(http.StatusOK, articleAbove(deepest.id, deepest.readable, "DEV", rootParent,
			ancestor{id: "177-2", readable: "DEV-A-2"})),
	}, respondWith(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-20")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-20", "/api/articles/" + deepest.id,
		"/api/articles/DEV-A-7"}, server.sentPaths())
	assert.Equal(t, articleLineFields(), server.sentFields()[2], "the line is read on with the same expression")
}

func TestArticleUpdateRefusesAnAncestorStepLeftEmptyInTheResponse(t *testing.T) {
	t.Parallel()
	found := `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9",` +
		`"project":{"$type":"Project","shortName":"DEV"},` +
		`"parentArticle":{"$type":"Article","id":null,"idReadable":"DEV-A-8","parentArticle":null}}`
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-9": respondWith(http.StatusOK, found),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-9")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-9")},
			{"upstream_status", 200},
			{"upstream_body", found},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

func TestArticleUpdateRefusesALineThatRepeatsAnArticle(t *testing.T) {
	t.Parallel()
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-9": respondWith(http.StatusOK, articleAbove("177-9", "DEV-A-9", "DEV", rootParent,
			ancestor{id: "177-8", readable: "DEV-A-8"}, ancestor{id: "177-9", readable: "DEV-A-9"})),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-9")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-9")},
			{"article", "DEV-A-9"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

func TestArticleUpdateRefusesAParentOfAnotherProject(t *testing.T) {
	t.Parallel()
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7":  respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEMO-A-1": respondWith(http.StatusOK, articleAbove("177-50", "DEMO-A-1", "DEMO", rootParent)),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEMO-A-1")

	want := faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEMO-A-1")},
			{"project", "DEV"},
			{"parent", "DEMO-A-1"},
			{"parent_project", "DEMO"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

func TestArticleUpdateTakesTheParentAwayWithoutReadingOne(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "x"}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
	}, respondWith(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--clear", "parent")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, map[string]any{"parentArticle": nil}, sentChanges(t, server))
	assert.Nil(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", got.stdout), "parentArticle")))
}

func TestArticleUpdateRefusesAnAnswerThatDisagreesAboutTheParent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		argv     []string
		parent   string
		mismatch []detail
	}{
		{
			name:     "a parent still standing where the call took it away",
			argv:     []string{"--clear", "parent"},
			parent:   parentNamed("DEV-A-1"),
			mismatch: []detail{{"field", "parentArticle"}, {"expected", nil}, {"actual", "DEV-A-1"}},
		},
		{
			name:     "no parent at all where the call wrote one",
			argv:     []string{"--parent", "DEV-A-1"},
			parent:   "null",
			mismatch: []detail{{"field", "parentArticle"}, {"expected", "DEV-A-1"}, {"actual", nil}},
		},
		{
			name:     "another parent than the one that was read",
			argv:     []string{"--parent", "DEV-A-1"},
			parent:   parentNamed("DEV-A-2"),
			mismatch: []detail{{"field", "parentArticle"}, {"expected", "DEV-A-1"}, {"actual", "DEV-A-2"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: tc.parent}
			server := movingAnArticle(t, map[string]http.HandlerFunc{
				"DEV-A-7": respondWith(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				"DEV-A-1": respondWith(http.StatusOK, rootOfDEV("177-1", "DEV-A-1")),
			}, respondWith(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), append([]string{"article", "update", "DEV-A-7"}, tc.argv...)...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleUpdateRequest(server.url, "DEV-A-7", articleShowFields)},
					{"article", "DEV-A-7"},
					{"mismatch", []any{tc.mismatch}},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestArticleUpdateMovesArticlesOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	title := contractArticleTitle(t)
	root := fileArticle(t, dev, title+" R")
	t.Cleanup(func() { removeArticle(t, dev, root) })
	parent := fileArticle(t, dev, title+" P", "--parent", root)
	child := fileArticle(t, dev, title+" C", "--parent", parent)
	grandchild := fileArticle(t, dev, title+" G", "--parent", child)

	mark := len(sentMethods(dev))
	closing := runWith(t, dev.env(), "article", "update", parent, "--parent", grandchild)
	found := requireFault(t, closing)
	assert.Equal(t, "bad_usage", found.code)
	assert.Contains(t, found.details, detail{"chain", []any{grandchild, child, parent}})
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentSince(dev, mark), "the write never went out")

	moved := runWith(t, dev.env(), "article", "update", child, "--parent", root)
	require.Equal(t, 0, moved.code, "stderr: %s", moved.stderr)
	assert.Equal(t, root, nodeAt(t, requireMapping(t, "stdout", moved.stdout), "parentArticle", "idReadable").Value)

	mark = len(sentMethods(dev))
	missing := runWith(t, dev.env(), "article", "update", child, "--parent", "DEV-A-99999")
	assert.Equal(t, "not_found", requireFault(t, missing).code)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentSince(dev, mark), "the write never went out")
	standing := runWith(t, dev.env(), "article", "show", child, "--comments=0", "--fields", "parentArticle(idReadable)")
	require.Equal(t, 0, standing.code, "stderr: %s", standing.stderr)
	assert.Equal(t, root, nodeAt(t, requireMapping(t, "stdout", standing.stdout), "parentArticle", "idReadable").Value,
		"a parent the dev instance has none of leaves the standing one where it was")

	taken := runWith(t, dev.env(), "article", "update", child, "--clear", "parent")
	t.Cleanup(func() { removeArticle(t, dev, child) })
	require.Equal(t, 0, taken.code, "stderr: %s", taken.stderr)
	assert.Nil(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", taken.stdout), "parentArticle")))
}

func TestArticleUpdateMovesNothingUnderAParentOfAnotherProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	filed := fileArticle(t, dev, contractArticleTitle(t))
	t.Cleanup(func() { removeArticle(t, dev, filed) })
	mark := len(sentMethods(dev))

	got := runWith(t, dev.env(), "article", "update", filed, "--parent", "DEMO-A-1")

	want := faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", articleLineRequest(dev.url, "DEMO-A-1")},
			{"project", "DEV"},
			{"parent", "DEMO-A-1"},
			{"parent_project", "DEMO"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentSince(dev, mark), "the write never went out")
}
