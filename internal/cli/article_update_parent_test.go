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

// How many steps of the line above an article one read asks for, which is what makes a deeper article cost
// another request rather than one request per step.
const ancestryStep = 10

// The expression the read of a parent goes out with: what every read before a write asks, and above it the line
// the parent hangs from, one nested member to a step.
func articleLineFields() string {
	line := "parentArticle(id,idReadable)"
	for range ancestryStep - 1 {
		line = "parentArticle(id,idReadable," + line + ")"
	}
	return articleToWriteFields + "," + line
}

// The request that read goes out as, which is the one a refusal raised before the write names.
func articleLineRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + articleLineFields()
}

// sentSince is the methods of every request that went out after mark, which is what a scenario running several
// commands against the polygon counts one command by.
func sentSince(u *upstream, mark int) []string {
	return sentMethods(u)[mark:]
}

// One step of the line an answer carries: the internal id a cycle is settled by and the readable id a refusal
// names it with.
type above struct{ id, readable string }

// nestedLine is the parentArticle member of an answer, each step nested in the one below it. top stands at the
// innermost: "null" is the root of the knowledge base, and "" is the expression running out before the line
// did, which is how the server answers a line deeper than the read asked for.
func nestedLine(top string, steps []above) string {
	nested := top
	for at := len(steps) - 1; at >= 0; at-- {
		body := `{"$type":"Article","id":` + strconv.Quote(steps[at].id) +
			`,"idReadable":` + strconv.Quote(steps[at].readable)
		if nested != "" {
			body += `,"parentArticle":` + nested
		}
		nested = body + `}`
	}
	return nested
}

// articleAbove is an article as the read of a parent finds it: what every read before a write asks, and the
// line it hangs from above that.
func articleAbove(id, readable, project, top string, steps ...above) string {
	body := `{"$type":"Article","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":` + strconv.Quote(project) + `}`
	if line := nestedLine(top, steps); line != "" {
		body += `,"parentArticle":` + line
	}
	return body + `}`
}

// A root of DEV as the read of a parent finds it: nothing above it at all.
func rootOfDEV(id, readable string) string {
	return articleAbove(id, readable, "DEV", "null")
}

// movingAnArticle is the server of an update that names a parent: every read is answered by the id in its path,
// so a scenario says what the article, the parent and each step of the line above it come back as, and the
// write is answered on its own.
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

// What --parent and --clear parent settle before the network: that the two do not say opposite things about
// the same part, and that the id of the parent is of the form of an article, which the API of articles answers
// for an issue and for an internal id too.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The help names --parent, the flag an article hangs from, and content or parent, the two names --clear
// takes: both are interface a caller reads off the command, not prose about how it behaves.
func TestArticleUpdateHelpNamesTheParentFlagAndWhatClearTakes(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "update", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "--parent")
	assert.Contains(t, got.stdout, "content or parent")
}

// The whole of a move: the article is read, the parent is read with the line above it, and the body
// addresses the parent by the internal id that read gave rather than by the string the caller typed. Both reads
// go to the articles, since the form of the id settled that before either went out.
func TestArticleUpdateMovesAnArticleUnderTheIDTheReadGave(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: parentNamed("DEV-A-1")}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"dev-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"dev-A-1": answer(http.StatusOK, rootOfDEV("177-1", "DEV-A-1")),
	}, answer(http.StatusOK, filed.json()))

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

// The parent is asked for whatever the caller asks to print, since the check of the write has nothing to
// hold the answer against otherwise: the move is held by the readable id the read gave it, and that name is
// ytrack's own to ask for.
func TestArticleUpdateAsksForTheParentWhateverTheExpressionSays(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: parentNamed("DEV-A-1")}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-1": answer(http.StatusOK, rootOfDEV("177-1", "DEV-A-1")),
	}, answer(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-1", "--fields", "idReadable")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "idReadable: \"DEV-A-7\"\n", got.stdout)
	assert.Equal(t, []string{articleToWriteFields, articleLineFields(), "idReadable,parentArticle(idReadable)"},
		server.sentFields())
}

// A parent the server has none of is a refusal and nothing else: YouTrack would take the parent the article
// hangs from off it under a 200 and without a word, so the caller would be told about a move they never asked
// for and the old parent would be gone.
func TestArticleUpdateRefusesAParentTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7":     answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-99999": answer(http.StatusNotFound, said),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-99999")

	want := refusal{
		code: "not_found",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-99999")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Can't find article with id DEV-A-99999"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
	// The id of the parent comes by a flag, so this is where the table of forms cannot reach: two reads go out
	// where every other command of articles sends one, and the 404 of the second is an answer rather than a
	// reason to ask the issues about it.
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-99999"}, server.sentPaths())
}

// A move that would close the line into a ring is settled off the line the read brought back, not sent:
// YouTrack answers an article moved under itself honestly and one moved under its own descendant with a 500 of
// a servlet, which would be a write_uncertain over an instance nothing touched.
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
			found:    articleAbove("177-7", "DEV-A-7", "DEV", "null"),
			chain:    []any{"DEV-A-7"},
		},
		{
			name:     "an article written under it",
			parent:   "DEV-A-9",
			readable: "DEV-A-9",
			found: articleAbove("177-9", "DEV-A-9", "DEV", "null",
				above{id: "177-8", readable: "DEV-A-8"}, above{id: "177-7", readable: "DEV-A-7"}),
			chain: []any{"DEV-A-9", "DEV-A-8", "DEV-A-7"},
		},
		{
			name:     "an article written under it, with a root above them both",
			parent:   "DEV-A-9",
			readable: "DEV-A-9",
			found: articleAbove("177-9", "DEV-A-9", "DEV", "null",
				above{id: "177-7", readable: "DEV-A-7"}, above{id: "177-1", readable: "DEV-A-1"}),
			chain: []any{"DEV-A-9", "DEV-A-7"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := movingAnArticle(t, map[string]http.HandlerFunc{
				"DEV-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				tc.parent: answer(http.StatusOK, tc.found),
			}, noUpdate(t))

			got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", tc.parent)

			want := refusal{
				code: "bad_usage",
				details: []detail{
					{"request", articleLineRequest(server.url, tc.parent)},
					{"article", "DEV-A-7"},
					{"parent", tc.readable},
					{"chain", tc.chain},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
		})
	}
}

// A line longer than one read asks for is read on: the deepest step of the answer carries no parent of its
// own, which is what tells a line that ran out of the expression from one that reached the root, and the next
// read starts from that step by the internal id it arrived under.
func TestArticleUpdateReadsOnWhereTheLineIsDeeperThanOneRequest(t *testing.T) {
	t.Parallel()
	steps := make([]above, 0, ancestryStep)
	for at := range ancestryStep {
		steps = append(steps, above{id: "177-" + strconv.Itoa(30+at), readable: "DEV-A-" + strconv.Itoa(30+at)})
	}
	deepest := steps[len(steps)-1]
	filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: parentNamed("DEV-A-20")}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7":  answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-20": answer(http.StatusOK, articleAbove("177-20", "DEV-A-20", "DEV", "", steps...)),
		deepest.id: answer(http.StatusOK, articleAbove(deepest.id, deepest.readable, "DEV", "null",
			above{id: "177-2", readable: "DEV-A-2"})),
	}, answer(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-20")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-20", "/api/articles/" + deepest.id,
		"/api/articles/DEV-A-7"}, server.sentPaths())
	assert.Equal(t, articleLineFields(), server.sentFields()[2], "the line is read on with the same expression")
}

// Every step of the line is held to its shape too: the judgment of names says the members arrived, and a
// step holding a null for its id would settle a cycle against nothing.
func TestArticleUpdateRefusesALineStepTheAnswerHoldsNothingIn(t *testing.T) {
	t.Parallel()
	found := `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9",` +
		`"project":{"$type":"Project","shortName":"DEV"},` +
		`"parentArticle":{"$type":"Article","id":null,"idReadable":"DEV-A-8","parentArticle":null}}`
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-9": answer(http.StatusOK, found),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-9")

	want := refusal{
		code: "upstream_lied",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-9")},
			{"upstream_status", 200},
			{"upstream_body", found},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

// An article standing twice in the line it hangs from is a tree the knowledge base cannot hold, so it is
// the answer that is wrong: the line is read no further, since reading it further would not end.
func TestArticleUpdateRefusesALineThatRepeatsAnArticle(t *testing.T) {
	t.Parallel()
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEV-A-9": answer(http.StatusOK, articleAbove("177-9", "DEV-A-9", "DEV", "null",
			above{id: "177-8", readable: "DEV-A-8"}, above{id: "177-9", readable: "DEV-A-9"})),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEV-A-9")

	want := refusal{
		code: "upstream_lied",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEV-A-9")},
			{"article", "DEV-A-9"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

// A call that names one project through the article and another through the parent is refused before the
// write, the way a creation of an article is: the rule is one for both verbs, whatever the server would answer.
func TestArticleUpdateRefusesAParentOfAnotherProject(t *testing.T) {
	t.Parallel()
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7":  answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		"DEMO-A-1": answer(http.StatusOK, articleAbove("177-50", "DEMO-A-1", "DEMO", "null")),
	}, noUpdate(t))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--parent", "DEMO-A-1")

	want := refusal{
		code: "bad_usage",
		details: []detail{
			{"request", articleLineRequest(server.url, "DEMO-A-1")},
			{"project", "DEV"},
			{"parent", "DEMO-A-1"},
			{"parent_project", "DEMO"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethods(server))
}

// Taking the parent away reads nothing of it: there is no parent to resolve, so the body says null outright
// and the article goes to the root of the knowledge base.
func TestArticleUpdateTakesTheParentAwayWithoutReadingOne(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "x"}
	server := movingAnArticle(t, map[string]http.HandlerFunc{
		"DEV-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
	}, answer(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "update", "DEV-A-7", "--clear", "parent")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, map[string]any{"parentArticle": nil}, sentChanges(t, server))
	assert.Nil(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", got.stdout), "parentArticle")))
}

// A 200 says the server took the body, not that it kept what was in it: an article still hanging where it
// hung, or hanging from another than the one that was read, is the write disagreeing with itself, and the move
// happened either way.
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
			mismatch: []detail{{"field", "parentArticle"}, {"written", nil}, {"arrived", "DEV-A-1"}},
		},
		{
			name:     "no parent at all where the call wrote one",
			argv:     []string{"--parent", "DEV-A-1"},
			parent:   "null",
			mismatch: []detail{{"field", "parentArticle"}, {"written", "DEV-A-1"}, {"arrived", nil}},
		},
		{
			name:     "another parent than the one that was read",
			argv:     []string{"--parent", "DEV-A-1"},
			parent:   parentNamed("DEV-A-2"),
			mismatch: []detail{{"field", "parentArticle"}, {"written", "DEV-A-1"}, {"arrived", "DEV-A-2"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filed := answeredArticle{readable: "DEV-A-7", summary: "x", parent: tc.parent}
			server := movingAnArticle(t, map[string]http.HandlerFunc{
				"DEV-A-7": answer(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
				"DEV-A-1": answer(http.StatusOK, rootOfDEV("177-1", "DEV-A-1")),
			}, answer(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), append([]string{"article", "update", "DEV-A-7"}, tc.argv...)...)

			want := refusal{
				code: "upstream_lied",
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

// A tree of the polygon moved about for real: the move that would close the line is refused off the line
// itself, a parent the polygon has none of leaves the standing parent where it was, and taking the parent away
// puts the article at the root of the knowledge base.
func TestArticleUpdateMovesArticlesOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	title := contractArticleTitle(t)
	root := fileArticle(t, dev, title+" R")
	t.Cleanup(func() { removeArticle(t, dev, root) })
	parent := fileArticle(t, dev, title+" P", "--parent", root)
	child := fileArticle(t, dev, title+" C", "--parent", parent)
	// Taken out of the tree by the last step below, so it is deleted on its own rather than with the root.
	t.Cleanup(func() { removeArticle(t, dev, child) })
	grandchild := fileArticle(t, dev, title+" G", "--parent", child)

	// The line above G runs G, C, P, so P cannot be moved under it while the tree stands that way.
	mark := len(sentMethods(dev))
	closing := runWith(t, dev.env(), "article", "update", parent, "--parent", grandchild)
	found := requireRefusal(t, closing)
	assert.Equal(t, "bad_usage", found.code)
	assert.Contains(t, found.details, detail{"chain", []any{grandchild, child, parent}})
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentSince(dev, mark), "the write never went out")

	moved := runWith(t, dev.env(), "article", "update", child, "--parent", root)
	require.Equal(t, 0, moved.code, "stderr: %s", moved.stderr)
	assert.Equal(t, root, nodeAt(t, requireMapping(t, "stdout", moved.stdout), "parentArticle", "idReadable").Value)

	mark = len(sentMethods(dev))
	missing := runWith(t, dev.env(), "article", "update", child, "--parent", "DEV-A-99999")
	assert.Equal(t, "not_found", requireRefusal(t, missing).code)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentSince(dev, mark), "the write never went out")
	standing := runWith(t, dev.env(), "article", "show", child, "--comments=0", "--fields", "parentArticle(idReadable)")
	require.Equal(t, 0, standing.code, "stderr: %s", standing.stderr)
	assert.Equal(t, root, nodeAt(t, requireMapping(t, "stdout", standing.stdout), "parentArticle", "idReadable").Value,
		"a parent the polygon has none of leaves the standing one where it was")

	taken := runWith(t, dev.env(), "article", "update", child, "--clear", "parent")
	require.Equal(t, 0, taken.code, "stderr: %s", taken.stderr)
	assert.Nil(t, requireValue(t, nodeAt(t, requireMapping(t, "stdout", taken.stdout), "parentArticle")))
}

// DEMO-A-1 is a fixture of another project: the read of the parent finds the project before the write and
// nothing is sent, so the article of DEV keeps the project and the parent it had.
func TestArticleUpdateMovesNothingUnderAParentOfAnotherProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	filed := fileArticle(t, dev, contractArticleTitle(t))
	t.Cleanup(func() { removeArticle(t, dev, filed) })
	mark := len(sentMethods(dev))

	got := runWith(t, dev.env(), "article", "update", filed, "--parent", "DEMO-A-1")

	want := refusal{
		code: "bad_usage",
		details: []detail{
			{"request", articleLineRequest(dev.url, "DEMO-A-1")},
			{"project", "DEV"},
			{"parent", "DEMO-A-1"},
			{"parent_project", "DEMO"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentSince(dev, mark), "the write never went out")
}
