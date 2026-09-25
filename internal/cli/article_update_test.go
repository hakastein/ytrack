package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

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

func TestArticleUpdateRefusesACallThatWritesNothing(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "article", "update", "DEV-A-7")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
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
	assert.Equal(t, map[string]any{"content": hostileText}, server.LastJSON(t))
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
