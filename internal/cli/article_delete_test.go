package cli_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func articleNamed(readable string) string {
	return `{"$type":"Article","idReadable":` + strconv.Quote(readable) + `}`
}

func articleReadRequest(address, id string) string {
	return "GET " + address + "/api/articles/" + url.PathEscape(id) + "?fields=" + deletedFields
}

func TestArticleDeleteReadsTheIDAndDeletesByIt(t *testing.T) {
	t.Parallel()
	server := deleting(t, fake.JSON(http.StatusOK, articleNamed("DEV-A-7")), deletionDone())

	got := runWith(t, server.Env(), "article", "delete", "dev-A-7")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-A-7\"\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, server.Methods())
	assert.Equal(t, []string{"/api/articles/dev-A-7", "/api/articles/DEV-A-7"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {deletedFields}}, {}}, server.Queries())
	assert.Equal(t, "Bearer "+fake.Token, server.Last(t).Header.Get("Authorization"))
	assert.Equal(t, []string{"", ""}, server.Bodies(), "neither request carries a body")
}

func TestArticleDeleteRefusesAReadableIDItCannotAddressBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
	}{
		{name: "two dots", received: `".."`},
		{name: "a path after the id", received: `"DEV-A-7/.."`},
		{name: "the id of an issue", received: `"DEV-1"`},
		{name: "a number", received: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Article","idReadable":` + tc.received + `}`
			server := deleting(t, fake.JSON(http.StatusOK, body), fake.Unexpected(t))

			got := runWith(t, server.Env(), "article", "delete", "dev-A-7")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", articleReadRequest(server.URL, "dev-A-7")},
					{"upstream_status", 200},
					{"upstream_body", body},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, server.Methods())
		})
	}
}
