package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const resolvedTagFields = "id,name,owner(login)"

func tagDeletionPath(id string) string {
	return "/api/tags/" + id
}

func tagDeletionRequest(address, id string) string {
	return "DELETE " + address + tagDeletionPath(id)
}

func catalogueTag(id, name, owner string) string {
	return `{"$type":"Tag","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) +
		`,"owner":{"$type":"User","login":` + strconv.Quote(owner) + `}}`
}

func tagCatalogue(tags ...string) string {
	return "[" + strings.Join(tags, ",") + "]"
}

func tagsOfTwoOwners() string {
	return tagCatalogue(
		catalogueTag("10-5", "Ready", "first"),
		catalogueTag("10-6", "Shared", "first"),
		catalogueTag("10-7", "Shared", "second"),
	)
}

func resolvingTags(t *testing.T, catalogue string, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, readThenDeletion(fake.JSON(http.StatusOK, catalogue), deletion))
}

func TestTagDeleteRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an empty name", argv: []string{"--name", ""}},
		{name: "the name given twice", argv: []string{"--name", "a", "--name", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"tag", "delete"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTagDeletePrintsWhatTheResolverFound(t *testing.T) {
	t.Parallel()
	server := resolvingTags(t, tagsOfTwoOwners(), deletionDone())

	got := runWith(t, server.Env(), "tag", "delete", "--name", "ready")

	want := `name: "Ready"` + "\n" + "owner:\n" + `  login: "first"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{"/api/tags?fields=" + resolvedTagFields + "&$top=-1", tagDeletionPath("10-5") + "?"},
		server.Targets())
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestTagDeleteRefusesANameNoTagCarries(t *testing.T) {
	t.Parallel()
	server := resolvingTags(t, tagsOfTwoOwners(), noDeletion(t))

	got := runWith(t, server.Env(), "tag", "delete", "--name", "redy")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", tagsRequest(server.URL, resolvedTagFields, "-1")},
			{"unknown", []any{[]detail{{"tag", "redy"}, {"nearest", []any{"Ready"}}}}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTagDeleteRefusesAnIDItCannotAddressTheDeletionBy(t *testing.T) {
	t.Parallel()
	catalogue := tagCatalogue(catalogueTag("..", "Ready", "first"))
	server := resolvingTags(t, catalogue, noDeletion(t))

	got := runWith(t, server.Env(), "tag", "delete", "--name", "Ready")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", tagsRequest(server.URL, resolvedTagFields, "-1")},
			{"upstream_status", 200},
			{"upstream_body", catalogue},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTagDeleteReadsWhatTheServerAnsweredTheDeletionWith(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		deletion http.HandlerFunc
		code     string
		exit     int
	}{
		{
			name:     "a token that may see the tag and not destroy it",
			deletion: fake.JSON(http.StatusForbidden, `{"error":"Forbidden","error_description":"Insufficient rights"}`),
			code:     "denied",
			exit:     1,
		},
		{
			name:     "a tag that went away between the two requests",
			deletion: fake.JSON(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id 10-5 not found"}`),
			code:     "not_found",
			exit:     1,
		},
		{
			name:     "an answer carrying a body where the call is answered with none",
			deletion: fake.JSON(http.StatusOK, `{"x":1}`),
			code:     "upstream_invalid",
			exit:     2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagsOfTwoOwners(), tc.deletion)

			got := runWith(t, server.Env(), "tag", "delete", "--name", "ready")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tagDeletionRequest(server.URL, "10-5")}, found.details[0])
			assert.Equal(t, detail{"tag", "ready"}, found.details[1])
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

func TestTagDeleteSendsNoDeletionWhereTheCatalogueWasNotReceived(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, readThenDeletion(
		fake.JSON(http.StatusInternalServerError, `{"error":"Internal Server Error"}`), noDeletion(t)))

	got := runWith(t, server.Env(), "tag", "delete", "--name", "Ready")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, tagsRequest(server.URL, resolvedTagFields, "-1"), detailNamed(t, found, "request"))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}
