package cli_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const articleToWriteFields = "id,idReadable,project(shortName)"

func articleToWriteRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + articleToWriteFields
}

func articleOfDEVToWrite(id, readable string) string {
	return `{"$type":"Article","id":` + strconv.Quote(id) + `,"idReadable":` + strconv.Quote(readable) +
		`,"project":{"$type":"Project","shortName":"DEV"}}`
}

func filingUnderAParent(t *testing.T, read, creation http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestArticleCreateRefusesAParentLeftEmptyInTheResponse(t *testing.T) {
	t.Parallel()
	const found = `{"$type":"Article","id":null,"idReadable":"DEV-A-1","project":{"$type":"Project","shortName":"DEV"}}`
	server := filingUnderAParent(t, fake.JSON(http.StatusOK, found), fake.Unexpected(t))

	got := runWith(t, server.Env(), "article", "create", "DEV", "--summary", "Title", "--parent", "DEV-A-1")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", articleToWriteRequest(server.URL, "DEV-A-1")},
			{"upstream_status", 200},
			{"upstream_body", found},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}
