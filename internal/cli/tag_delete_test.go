package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
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

func TestTagDeletePrintsWhatTheResolverFound(t *testing.T) {
	t.Parallel()
	server := resolvingTags(t, tagsOfTwoOwners(), deletionDone())

	got := runWith(t, server.Env(), "tag", "delete", "--name", "ready")

	want := `name: "Ready"` + "\n" + "owner:\n" + `  login: "first"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, server.Methods())
	assert.Equal(t, []string{"/api/tags?fields=" + resolvedTagFields + "&$top=-1", tagDeletionPath("10-5") + "?"},
		server.Targets(t))
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestTagDeleteRefusesANameNoTagCarries(t *testing.T) {
	t.Parallel()
	server := resolvingTags(t, tagsOfTwoOwners(), fake.Unexpected(t))

	got := runWith(t, server.Env(), "tag", "delete", "--name", "redy")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", tagsRequest(server.URL, resolvedTagFields, "-1")},
			{"unknown", []any{[]detail{{"tag", "redy"}, {"nearest", []any{"Ready"}}}}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}

func TestTagDeleteRefusesAnIDItCannotAddressTheDeletionBy(t *testing.T) {
	t.Parallel()
	catalogue := tagCatalogue(catalogueTag("..", "Ready", "first"))
	server := resolvingTags(t, catalogue, fake.Unexpected(t))

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
	assert.Equal(t, []string{http.MethodGet}, server.Methods())
}

func TestTagDeleteRefusesATagGoneBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	const missing = `{"error":"Not Found","error_description":"Entity with id 10-5 not found"}`
	server := resolvingTags(t, tagsOfTwoOwners(), fake.JSON(http.StatusNotFound, missing))

	got := runWith(t, server.Env(), "tag", "delete", "--name", "ready")

	want := faultDocument{
		code: "not_found",
		details: []detail{
			{"request", tagDeletionRequest(server.URL, "10-5")},
			{"tag", "ready"},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id 10-5 not found"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, server.Methods())
}
