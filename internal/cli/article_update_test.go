package cli_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func articleUpdateRequest(address, readable, fields string) string {
	return "POST " + address + "/api/articles/" + readable + "?fields=" + fields
}

func updatingAnArticle(t *testing.T, read, update http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
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

func sentChanges(t *testing.T, u *fake.Server) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(u.Last(t).Body), &body))
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
		{name: "a title twice", argv: []string{"DEV-A-7", "--summary", "a", "--summary", "b"}},
		{name: "content twice", argv: []string{"DEV-A-7", "--content", "a", "--content", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"article", "update"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestArticleUpdateWritesTheArticleTheReadFound(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "Title", content: asJSON(hostileText)}
	server := updatingAnArticle(t,
		fake.JSON(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		fake.JSON(http.StatusOK, filed.json()))

	got := runWith(t, server.Env(), "article", "update", "DEV-A-7", "--content", hostileText,
		"--fields", "idReadable,summary")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-A-7\"\nsummary: \"Title\"\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-7"}, server.Paths())
	assert.Equal(t, map[string]any{"content": hostileText}, sentChanges(t, server))
}

func TestArticleUpdateRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	filed := answeredArticle{readable: "DEV-A-7", summary: "Title", content: asJSON("text")}
	server := updatingAnArticle(t,
		fake.JSON(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
		fake.JSON(http.StatusOK, filed.json()))

	got := runWith(t, server.Env(), "article", "update", "DEV-A-7", "--content", "Text")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleUpdateRequest(server.URL, "DEV-A-7", articleShowFields)},
			{"article", "DEV-A-7"},
			{"mismatch", []any{[]detail{{"field", "content"}, {"expected", "Text"}, {"actual", "text"}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}

func TestArticleUpdateRefusesAnArticleTheReadDoesNotFind(t *testing.T) {
	t.Parallel()
	said := `{"error":"Not Found","error_description":"Can't find article with id DEV-A-99999"}`
	server := updatingAnArticle(t, fake.JSON(http.StatusNotFound, said), noUpdate(t))

	got := runWith(t, server.Env(), "article", "update", "DEV-A-99999", "--summary", "x")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", articleToWriteRequest(server.URL, "DEV-A-99999")},
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
	const body = `{"$type":"Article","id":"177-7","idReadable":"..","project":{"$type":"Project","shortName":"DEV"}}`
	server := updatingAnArticle(t, fake.JSON(http.StatusOK, body), noUpdate(t))

	got := runWith(t, server.Env(), "article", "update", "DEV-A-7", "--summary", "x")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleToWriteRequest(server.URL, "DEV-A-7")},
			{"upstream_status", 200},
			{"upstream_body", body},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestArticleUpdateIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := updatingAnArticle(t, fake.JSON(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")), breakOff)

	got := runWith(t, server.Env(), "article", "update", "DEV-A-7", "--summary", "x")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", articleUpdateRequest(server.URL, "DEV-A-7", articleShowFields)}},
		found.details)
	assert.Empty(t, got.stdout)
}
