package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		catalogueTag("10-5", "Ready", "admin"),
		catalogueTag("10-8", "wayfinder:map", "admin"),
		catalogueTag("10-19", "case", "dev.limited"),
		catalogueTag("10-20", "CASE", "admin"),
		catalogueTag("10-22", "amb", "admin"),
		catalogueTag("10-23", "amb", "dev.limited"),
		catalogueTag("10-77", "10-5", "admin"),
	)
}

func resolvingTags(t *testing.T, catalogue string, deletion http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, readThenDeletion(respondWith(http.StatusOK, catalogue), deletion))
}

func requireResolvedWithoutTheServer(t *testing.T, server *upstream, name string) {
	t.Helper()
	for _, target := range server.sentTargets() {
		assert.NotContains(t, target, name, "the name reached the server")
		assert.NotContains(t, target, strings.ToLower(name), "the lower case of the name reached the server")
	}
	for _, query := range server.sentQueries() {
		assert.Empty(t, query["query"], "a request asked the server to search by name")
	}
}

func TestTagDeleteRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an empty name", argv: []string{"--name", ""}},
		{name: "a name that is no UTF-8", argv: []string{"--name", "\xff"}},
		{name: "the name given twice", argv: []string{"--name", "a", "--name", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"tag", "delete"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTagDeleteResolvesTheNameAgainstTheTagsItIsShown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		id      string
	}{
		{name: "another letter case", written: "ready", id: "10-5"},
		{name: "a name of punctuation in upper case", written: "WAYFINDER:MAP", id: "10-8"},
		{name: "one of two that differ by letter case", written: "case", id: "10-19"},
		{name: "the other of the two", written: "CASE", id: "10-20"},
		{name: "a name shaped like an internal id", written: "10-5", id: "10-77"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagsOfTwoOwners(), deletionDone())

			got := runWith(t, server.env(), "tag", "delete", "--name", tc.written)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
			assert.Equal(t, []string{"/api/tags", tagDeletionPath(tc.id)}, server.sentPaths())
			requireResolvedWithoutTheServer(t, server, tc.written)
		})
	}
}

func TestTagDeleteRefusesANameThatNamesNoOneTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		details []detail
	}{
		{
			name:    "two tags the name answers to by letter case",
			written: "Case",
			details: []detail{{"ambiguous", []any{[]detail{
				{"tag", "Case"},
				{"candidates", []any{
					[]detail{{"name", "CASE"}, {"owner", "admin"}},
					[]detail{{"name", "case"}, {"owner", "dev.limited"}},
				}},
			}}}},
		},
		{
			name:    "two tags of one name and two owners",
			written: "amb",
			details: []detail{{"ambiguous", []any{[]detail{
				{"tag", "amb"},
				{"candidates", []any{
					[]detail{{"name", "amb"}, {"owner", "admin"}},
					[]detail{{"name", "amb"}, {"owner", "dev.limited"}},
				}},
			}}}},
		},
		{
			name:    "a name one letter away from a tag",
			written: "redy",
			details: []detail{{"unknown", []any{[]detail{
				{"tag", "redy"},
				{"nearest", []any{"Ready"}},
			}}}},
		},
		{
			name:    "a name near nothing there is",
			written: "zzzzzz",
			details: []detail{{"unknown", []any{[]detail{
				{"tag", "zzzzzz"},
				{"nearest", []any{"10-5", "CASE", "Ready", "amb", "amb", "case", "wayfinder:map"}},
			}}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagsOfTwoOwners(), noDeletion(t))

			got := runWith(t, server.env(), "tag", "delete", "--name", tc.written)

			want := faultDocument{
				code:    "unknown_name",
				details: append([]detail{{"request", tagsRequest(server.url, resolvedTagFields, "-1")}}, tc.details...),
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
			requireResolvedWithoutTheServer(t, server, tc.written)
		})
	}
}

func TestTagDeletePrintsWhatTheResolverFound(t *testing.T) {
	t.Parallel()
	server := resolvingTags(t, tagsOfTwoOwners(), deletionDone())

	got := runWith(t, server.env(), "tag", "delete", "--name", "ready")

	want := `name: "Ready"` + "\n" + "owner:\n" + `  login: "admin"` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)

	asked := server.requests()
	require.Len(t, asked, 2)
	assert.Equal(t, http.MethodGet, asked[0].Method)
	assert.Equal(t, "/api/tags", asked[0].URL.Path)
	assert.Equal(t, []string{resolvedTagFields, ""}, server.sentFields())
	assert.Equal(t, "-1", asked[0].URL.Query().Get("$top"))
	assert.Equal(t, http.MethodDelete, asked[1].Method)
	assert.Equal(t, tagDeletionPath("10-5"), asked[1].URL.Path)
	assert.Empty(t, asked[1].URL.RawQuery)
	assert.Equal(t, []string{"", ""}, server.asks())
}

func TestTagDeleteRefusesAnIDItCannotAddressTheDeletionBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "two dots", id: ".."},
		{name: "digits, a dash and a letter", id: "10-x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagCatalogue(catalogueTag(tc.id, "Ready", "admin")), noDeletion(t))

			got := runWith(t, server.env(), "tag", "delete", "--name", "Ready")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestTagDeleteRefusesACatalogueItCannotTellTagsApartBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  string
	}{
		{name: "a name that is a number", tag: `{"$type":"Tag","id":"10-5","name":5,"owner":{"$type":"User","login":"admin"}}`},
		{name: "an owner whose login is null", tag: `{"$type":"Tag","id":"10-5","name":"Ready","owner":{"$type":"User","login":null}}`},
		{name: "an id that is a number", tag: `{"$type":"Tag","id":105,"name":"Ready","owner":{"$type":"User","login":"admin"}}`},
		{name: "an id that is null", tag: `{"$type":"Tag","id":null,"name":"Ready","owner":{"$type":"User","login":"admin"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagCatalogue(tc.tag), noDeletion(t))

			got := runWith(t, server.env(), "tag", "delete", "--name", "Ready")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
			assert.Empty(t, got.stdout)
		})
	}
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
			deletion: respondWith(http.StatusForbidden, `{"error":"Forbidden","error_description":"Insufficient rights"}`),
			code:     "denied",
			exit:     1,
		},
		{
			name:     "a tag that went away between the two requests",
			deletion: respondWith(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id 10-5 not found"}`),
			code:     "not_found",
			exit:     1,
		},
		{
			name:     "an answer carrying a body where the call is answered with none",
			deletion: respondWith(http.StatusOK, `{"x":1}`),
			code:     "upstream_invalid",
			exit:     2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagsOfTwoOwners(), tc.deletion)

			got := runWith(t, server.env(), "tag", "delete", "--name", "ready")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tagDeletionRequest(server.url, "10-5")}, found.details[0])
			assert.Equal(t, detail{"tag", "ready"}, found.details[1])
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

func TestTagDeleteSendsNoDeletionWhereTheCatalogueWasNotReceived(t *testing.T) {
	t.Parallel()
	server := serve(t, readThenDeletion(
		respondWith(http.StatusInternalServerError, `{"error":"Internal Server Error"}`), noDeletion(t)))

	got := runWith(t, server.env(), "tag", "delete", "--name", "Ready")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, tagsRequest(server.url, resolvedTagFields, "-1"), detailNamed(t, found, "request"))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}
