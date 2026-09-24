package cli_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the read before a write asks of the article: the id a body carries it by, the id the answer is held
// against, and the project no article may be filed across. A creation asks it of the parent and an update of
// the article it writes into.
const articleToWriteFields = "id,idReadable,project(shortName)"

// The request that read goes out as, which is the one a refusal raised before the write names.
func articleToWriteRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + articleToWriteFields
}

// An article of DEV as the read before a write finds it: the internal id a body addresses it by, the readable
// id it is held against, and the project it stands in.
func articleOfDEVToWrite(id, readable string) string {
	return `{"$type":"Article","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":"DEV"}}`
}

// The parent as it stands on the article the creation answers with, which carries the readable id alone.
func parentNamed(readable string) string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(readable) + `,"summary":"Родительская статья"}`
}

// filingUnderAParent is the server of a creation that names a parent: the read of the parent and the creation
// itself, each answered by the scenario, and nothing else reaches it.
func filingUnderAParent(t *testing.T, read, creation http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			read(w, r)
			return
		}
		if !assert.Equal(t, http.MethodPost, r.Method, "a creation under a parent sends one GET and one POST") {
			return
		}
		creation(w, r)
	})
}

// The id of the parent is held to the form of an article before anything is sent, the same way the id a
// command is given as an argument is: the API of articles answers for an issue and for an internal id too, and
// that answer would name the wrong reason.
func TestArticleCreateRefusesAParentOfAnyOtherFormBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the id of an issue", argv: []string{"--parent", "DEV-1"}},
		{name: "an internal id", argv: []string{"--parent", "177-1"}},
		{name: "the marker in lower case", argv: []string{"--parent", "DEV-a-1"}},
		{name: "no id at all", argv: []string{"--parent", ""}},
		{name: "a parent twice", argv: []string{"--parent", "DEV-A-1", "--parent", "DEV-A-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"article", "create", "DEV", "--summary", "x"}, tc.argv...)...)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

// The help names the flag a parent is filed under, since a caller reading --help has nothing else to find
// the option by.
func TestArticleCreateHelpNamesTheParentFlag(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"article", "create", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "--parent")
}

// The whole of a creation under a parent: the parent is read first, and the body addresses it by the
// internal id that read gave rather than by the string the caller typed, which the server resolves anew.
func TestArticleCreateAddressesTheParentByTheIDTheReadGave(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-8", summary: "x", parent: parentNamed("DEV-A-1")}
	server := filingUnderAParent(t,
		answer(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
		answer(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "dev-A-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles/dev-A-1", "/api/articles"}, server.sentPaths())
	assert.Equal(t, []string{articleToWriteFields, askedArticleFields}, server.sentFields())

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(server)), &body))
	assert.Equal(t, map[string]any{
		"project":       map[string]any{"shortName": "DEV"},
		"summary":       "x",
		"parentArticle": map[string]any{"id": "177-1"},
	}, body)

	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "DEV-A-1", nodeAt(t, mapping, "parentArticle", "idReadable").Value)
}

// A parent the server has none of is a refusal and nothing else: YouTrack would file the article at the
// root of the knowledge base under a 200, and the caller would be told about an article they did not ask for.
func TestArticleCreateRefusesAParentTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := filingUnderAParent(t, answer(http.StatusNotFound, said), noCreation(t))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-99999")

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

// A call that names one project as its argument and another through its parent is refused before the
// write: YouTrack files the article in the parent's project, so the one the caller wrote would be the one
// ignored.
func TestArticleCreateRefusesAParentOfAnotherProject(t *testing.T) {
	t.Parallel()
	found := `{"$type":"Article","id":"177-50","idReadable":"DEMO-A-1",` +
		`"project":{"$type":"Project","shortName":"DEMO"}}`
	server := filingUnderAParent(t, answer(http.StatusOK, found), noCreation(t))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEMO-A-1")

	want := refusal{
		code: "bad_usage",
		details: []detail{
			{"request", articleToWriteRequest(server.url, "DEMO-A-1")},
			{"project", "DEV"},
			{"parent", "DEMO-A-1"},
			{"parent_project", "DEMO"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// The judgment of names says the members of the parent arrived, not what they hold: a null under the id or
// under the project of it passes that judgment, and the body would then address the parent by an empty string
// and the article would be filed at the root of the tree under a 200.
func TestArticleCreateRefusesAParentTheAnswerHoldsNothingIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		found string
	}{
		{
			name: "no internal id",
			found: `{"$type":"Article","id":null,"idReadable":"DEV-A-1",` +
				`"project":{"$type":"Project","shortName":"DEV"}}`,
		},
		{
			name:  "no project at all",
			found: `{"$type":"Article","id":"177-1","idReadable":"DEV-A-1","project":null}`,
		},
		{
			name: "a project of no name",
			found: `{"$type":"Article","id":"177-1","idReadable":"DEV-A-1",` +
				`"project":{"$type":"Project","shortName":null}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := filingUnderAParent(t, answer(http.StatusOK, tc.found), noCreation(t))

			got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-1")

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", articleToWriteRequest(server.url, "DEV-A-1")},
					{"upstream_status", 200},
					{"upstream_body", tc.found},
				},
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// The server reads dev for DEV and answers with the code as it keeps it, which the help of the command
// promises: an argument and a parent that differ only in letter case name one project, and the article is
// filed rather than refused for standing across one.
func TestArticleCreateFilesUnderAParentOfTheProjectInAnotherLetterCase(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-8", summary: "x", parent: parentNamed("DEV-A-1")}
	server := filingUnderAParent(t,
		answer(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
		answer(http.StatusOK, filed.json()))

	got := runWith(t, server.env(), "article", "create", "dev", "--summary", "x", "--parent", "DEV-A-1")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastAsk(server)), &body))
	assert.Equal(t, map[string]any{
		"project":       map[string]any{"shortName": "dev"},
		"summary":       "x",
		"parentArticle": map[string]any{"id": "177-1"},
	}, body)
}

// The read before the write settles what is there before the write, not what the write did: an article
// that came back at the root of the tree, or under another parent than the one that was read, is the write
// disagreeing with itself and the article exists either way.
func TestArticleCreateRefusesAnAnswerCarryingAnotherParent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		parent  string
		arrived any
	}{
		{name: "no parent at all", parent: "null", arrived: nil},
		{name: "another parent", parent: parentNamed("DEV-A-2"), arrived: "DEV-A-2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filed := answeredArticle{readable: "DEV-A-8", summary: "x", parent: tc.parent}
			server := filingUnderAParent(t,
				answer(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
				answer(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-1")

			want := refusal{
				code: "upstream_lied",
				details: []detail{
					{"request", articleCreationRequest(server.url, askedArticleFields)},
					{"article", "DEV-A-8"},
					{"mismatch", []any{
						[]detail{{"field", "parentArticle"}, {"written", "DEV-A-1"}, {"arrived", tc.arrived}},
					}},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

// The parent is asked for whatever the caller asks to print, since the check of the write has nothing to
// hold the answer against otherwise. The name is ytrack's own, so an answer that carries no readable id under
// the parent is the server disagreeing with the request rather than the caller writing a name that is not
// there.
func TestArticleCreateAsksForTheParentWhateverTheExpressionSays(t *testing.T) {
	t.Parallel()
	filed := `{"$type":"Article","idReadable":"DEV-A-8","summary":"x","content":null,` +
		`"project":{"$type":"Project","shortName":"DEV"},"parentArticle":{"$type":"Article","id":"177-1"}}`
	server := filingUnderAParent(t,
		answer(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
		answer(http.StatusOK, filed))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-1",
		"--fields", "idReadable")

	asked := "idReadable,summary,content,project(shortName),parentArticle(idReadable)"
	assert.Equal(t, []string{articleToWriteFields, asked}, server.sentFields())
	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Empty(t, got.stdout)
}

// A tree of the polygon, built and taken away for real: the child hangs from the parent on both sides, and
// deleting the parent takes the child with it, which is what makes one deletion enough to clean up after a
// whole scenario.
func TestArticleCreateFilesAnArticleUnderAParentOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	parent := fileArticle(t, dev, contractArticleTitle(t)+" parent")

	got := runWith(t, dev.env(), "article", "create", "DEV", "--summary", contractArticleTitle(t)+" child",
		"--parent", parent)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	child := nodeAt(t, mapping, "idReadable").Value
	require.Regexp(t, `^DEV-A-[0-9]+$`, child)
	assert.Equal(t, parent, nodeAt(t, mapping, "parentArticle", "idReadable").Value)

	under := runWith(t, dev.env(), "article", "show", parent, "--comments=0", "--fields", "childArticles(idReadable)")
	require.Equal(t, 0, under.code, "stderr: %s", under.stderr)
	assert.Contains(t, under.stdout, child)

	removed := runWith(t, dev.env(), "article", "delete", parent)
	assert.Equal(t, outcome{stdout: "idReadable: " + strconv.Quote(parent) + "\n"}, removed)
	gone := runWith(t, dev.env(), "article", "show", child, "--comments=0")
	assert.Equal(t, "not_found", requireRefusalDocument(t, gone).code,
		"a deletion takes the whole subtree, the child among it")
}

// The polygon has no such article, and the read says so: nothing is filed, so there is no article at the
// root of the knowledge base to find and delete afterwards.
func TestArticleCreateFilesNothingUnderAParentTheDevInstanceDoesNotHave(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "create", "DEV", "--summary", contractArticleTitle(t),
		"--parent", "DEV-A-99999")

	want := refusal{
		code: "not_found",
		details: []detail{
			{"request", articleToWriteRequest(dev.url, "DEV-A-99999")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Can't find article with id DEV-A-99999"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}

// DEMO-A-1 is a fixture of another project, and the polygon would file the article there under a 200: the
// read finds the project before the write and nothing is sent, so the fixture keeps the children it had.
func TestArticleCreateFilesNothingUnderAParentOfAnotherProjectOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "article", "create", "DEV", "--summary", contractArticleTitle(t),
		"--parent", "DEMO-A-1")

	want := refusal{
		code: "bad_usage",
		details: []detail{
			{"request", articleToWriteRequest(dev.url, "DEMO-A-1")},
			{"project", "DEV"},
			{"parent", "DEMO-A-1"},
			{"parent_project", "DEMO"},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
}
