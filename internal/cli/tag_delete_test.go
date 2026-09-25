package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expression the resolver sends: the id the deletion is addressed by and the pair that tells one tag from
// another, which is what the deletion prints.
const resolvedTagFields = "id,name,owner(login)"

// The path of the deletion of one tag, which is where the internal id — and nothing a caller typed — stands.
func tagDeletionPath(id string) string {
	return "/api/tags/" + id
}

func tagDeletionRequest(address, id string) string {
	return "DELETE " + address + tagDeletionPath(id)
}

// catalogueTag is one record of the catalogue the resolver reads, as the server sends it.
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

// resolvingTags is the server of a deletion: the catalogue answers the read that resolves the name, and
// deletion the DELETE that follows it.
func resolvingTags(t *testing.T, catalogue string, deletion http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, readThenDeletion(respondWith(http.StatusOK, catalogue), deletion))
}

// requireResolvedWithoutTheServer holds the whole point of the resolver: the name was matched here and never
// sent, so neither it nor its lower form stands anywhere in any address, and no request asks the server to
// search by it.
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

// A deletion names one tag by --name and takes nothing else at all: neither a positional argument, which
// would swallow a name beginning with a dash, nor a flag of another verb. An empty name and no name at all are
// different mistakes, and only the flag tells them apart.
func TestTagDeleteRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: nil},
		{name: "a name written as an argument", argv: []string{"Ready"}},
		{name: "two arguments", argv: []string{"a", "b"}},
		{name: "an empty name", argv: []string{"--name", ""}},
		{name: "a name that is no UTF-8", argv: []string{"--name", "\xff"}},
		{name: "the name given twice", argv: []string{"--name", "a", "--name", "b"}},
		// Nothing is asked before the tag goes, so there is no flag that answers.
		{name: "a flag that would confirm it", argv: []string{"--name", "x", "--yes"}},
		{name: "a flag that would force it", argv: []string{"--name", "x", "--force"}},
		// The deletion prints what the resolver found, so there is no tree for a caller to choose.
		{name: "an expression of fields", argv: []string{"--name", "x", "--fields", "name"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"tag", "delete"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// A name becomes one tag here and nowhere else: letter case is the server's to fold, a name two tags carry
// is settled by writing one of them exactly, and a string shaped like an internal id is a name like any other —
// 10-5 names the tag called 10-5, whose id is 10-77 (5.13). The name is never sent, in any form.
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

// A name that resolves to no one tag ends the call before anything is destroyed, and the refusal carries
// what to write instead: the tags the name answers to with their owners, since the owner is the other half of
// what tells two tags apart, or the names nearest it where it answers to none. A caller near nothing at all is
// shown the whole catalogue in code point order, the name two tags carry standing twice.
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
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
			requireResolvedWithoutTheServer(t, server, tc.written)
		})
	}
}

// The whole of the call on the wire: the catalogue read once with the whole of it asked for, then the
// deletion of the id that read gave, carrying no body and no query at all. What is printed is what the read
// answered — the DELETE went to that id, so the name and the owner printed are the ones it took away.
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

// The id of the tag comes from the server and becomes a path segment, so it is held to the form of an
// internal id before anything is sent: ".." would reach /api/tags itself, the collection every tag stands in,
// and 10-x would reach a tag of an id YouTrack gives none. Nothing is destroyed either way.
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

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// A tag is told from another by the pair of its name and the login of its owner, so a catalogue where
// either of the two is not text is one no name can be resolved against: the refusal says which member is
// wrong rather than letting an empty string stand for it, since a candidate listed under an owner of "" would
// send the caller to mend a call that was written correctly.
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

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
			assert.Empty(t, got.stdout)
		})
	}
}

// What the server answers the deletion with is read the way a status is read everywhere, and the border of
// ADR-0005 runs through it: a refusal before the tag went leaves the caller the same call to send again, while
// an answer under a 200 that carries anything at all is the instance changed without the document being read.
// Every one of them names the tag by the name that was written, since the request carries the internal id and
// nothing the caller typed — the same key tag add and tag remove name theirs by.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tagDeletionRequest(server.url, "10-5")}, found.details[0])
			assert.Equal(t, detail{"tag", "ready"}, found.details[1])
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

// The catalogue is a request like any other, and one the server refuses leaves nothing to resolve against:
// a name held to a catalogue that never arrived would name a tag ytrack has no word for, so nothing is sent
// after it.
func TestTagDeleteSendsNoDeletionWhereTheCatalogueWasNotReceived(t *testing.T) {
	t.Parallel()
	server := serve(t, readThenDeletion(
		respondWith(http.StatusInternalServerError, `{"error":"Internal Server Error"}`), noDeletion(t)))

	got := runWith(t, server.env(), "tag", "delete", "--name", "Ready")

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, tagsRequest(server.url, resolvedTagFields, "-1"), detailNamed(t, found, "request"))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTagDeleteRefusesANameTheDevInstanceHasNoTagUnder(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "tag", "delete", "--name", "ytrack contract "+t.Name()+" nope")

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	assert.Equal(t, []string{"/api/tags"}, dev.sentPaths())
	assert.Equal(t, []string{resolvedTagFields}, dev.sentFields())
	unknown, isList := detailNamed(t, found, "unknown").([]any)
	require.True(t, isList, "the refusal named no unknown tag")
	assert.Len(t, unknown, 1)
}
