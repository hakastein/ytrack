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

const articleToWriteFields = "id,idReadable,project(shortName)"

func articleToWriteRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + articleToWriteFields
}

func articleOfDEVToWrite(id, readable string) string {
	return `{"$type":"Article","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":"DEV"}}`
}

func parentNamed(readable string) string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(readable) + `,"summary":"Родительская статья"}`
}

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

			assert.Equal(t, "bad_usage", requireFault(t, got).code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestArticleCreateAddressesTheParentByTheIDTheReadGave(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-8", summary: "x", parent: parentNamed("DEV-A-1")}
	server := filingUnderAParent(t,
		respondWith(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
		respondWith(http.StatusOK, filed.json()))

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

func TestArticleCreateRefusesAParentTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := filingUnderAParent(t, respondWith(http.StatusNotFound, said), noCreation(t))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-99999")

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

func TestArticleCreateRefusesAParentOfAnotherProject(t *testing.T) {
	t.Parallel()
	found := `{"$type":"Article","id":"177-50","idReadable":"DEMO-A-1",` +
		`"project":{"$type":"Project","shortName":"DEMO"}}`
	server := filingUnderAParent(t, respondWith(http.StatusOK, found), noCreation(t))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEMO-A-1")

	want := faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", articleToWriteRequest(server.url, "DEMO-A-1")},
			{"project", "DEV"},
			{"parent", "DEMO-A-1"},
			{"parent_project", "DEMO"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestArticleCreateRefusesAParentLeftEmptyInTheResponse(t *testing.T) {
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
			server := filingUnderAParent(t, respondWith(http.StatusOK, tc.found), noCreation(t))

			got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-1")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleToWriteRequest(server.url, "DEV-A-1")},
					{"upstream_status", 200},
					{"upstream_body", tc.found},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestArticleCreateFilesUnderAParentOfTheProjectInAnotherLetterCase(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-8", summary: "x", parent: parentNamed("DEV-A-1")}
	server := filingUnderAParent(t,
		respondWith(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
		respondWith(http.StatusOK, filed.json()))

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

func TestArticleCreateRefusesAnAnswerCarryingAnotherParent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		parent   string
		received any
	}{
		{name: "no parent at all", parent: "null", received: nil},
		{name: "another parent", parent: parentNamed("DEV-A-2"), received: "DEV-A-2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filed := answeredArticle{readable: "DEV-A-8", summary: "x", parent: tc.parent}
			server := filingUnderAParent(t,
				respondWith(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
				respondWith(http.StatusOK, filed.json()))

			got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-1")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleCreationRequest(server.url, askedArticleFields)},
					{"article", "DEV-A-8"},
					{"mismatch", []any{
						[]detail{{"field", "parentArticle"}, {"expected", "DEV-A-1"}, {"actual", tc.received}},
					}},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestArticleCreateAsksForTheParentWhateverTheExpressionSays(t *testing.T) {
	t.Parallel()
	filed := `{"$type":"Article","idReadable":"DEV-A-8","summary":"x","content":null,` +
		`"project":{"$type":"Project","shortName":"DEV"},"parentArticle":{"$type":"Article","id":"177-1"}}`
	server := filingUnderAParent(t,
		respondWith(http.StatusOK, articleOfDEVToWrite("177-1", "DEV-A-1")),
		respondWith(http.StatusOK, filed))

	got := runWith(t, server.env(), "article", "create", "DEV", "--summary", "x", "--parent", "DEV-A-1",
		"--fields", "idReadable")

	asked := "idReadable,summary,content,project(shortName),parentArticle(idReadable)"
	assert.Equal(t, []string{articleToWriteFields, asked}, server.sentFields())
	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Empty(t, got.stdout)
}
