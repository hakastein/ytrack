package cli_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagRefusesAnOwnerOfNoLogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a deletion", argv: []string{"tag", "delete", "--name", "amb", "--owned-by", ""}},
		{name: "a tagging", argv: []string{"tag", "add", "DEV-7", "--name", "amb", "--owned-by", ""}},
		{name: "a removal", argv: []string{"tag", "remove", "DEV-7", "--name", "amb", "--owned-by", ""}},
		{
			name: "the owner given twice",
			argv: []string{"tag", "delete", "--name", "amb", "--owned-by", "admin", "--owned-by", "dev.limited"},
		},
		{name: "a list of one owner's tags", argv: []string{"tag", "list", "--owned-by", "admin"}},
		{name: "a creation of another user's tag", argv: []string{"tag", "create", "--name", "amb", "--owned-by", "admin"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTagDeleteNarrowsTheNameByTheOwnerOfTheTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		owner   string
		id      string
	}{
		{name: "one name and two owners", written: "amb", owner: "admin", id: "10-22"},
		{name: "the other owner of it", written: "amb", owner: "dev.limited", id: "10-23"},
		{name: "a login in another letter case", written: "amb", owner: "ADMIN", id: "10-22"},
		{name: "a name neither of the two is spelled as", written: "Case", owner: "admin", id: "10-20"},
		{name: "the other owner of that name", written: "Case", owner: "dev.limited", id: "10-19"},
		{name: "a name spelled as the other owner's tag", written: "case", owner: "admin", id: "10-20"},
		{name: "the same the other way round", written: "CASE", owner: "dev.limited", id: "10-19"},
		{name: "a name one tag carries, of its owner", written: "ready", owner: "admin", id: "10-5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagsOfTwoOwners(), deletionDone())

			got := runWith(t, server.env(), "tag", "delete", "--name", tc.written, "--owned-by", tc.owner)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
			assert.Equal(t, []string{tagsCollection, tagDeletionPath(tc.id)}, server.sentPaths())
			requireResolvedWithoutTheServer(t, server, tc.written)
			for _, target := range server.sentTargets() {
				assert.NotContains(t, target, tc.owner, "the login reached the server")
			}
		})
	}
}

func TestTagDeleteRefusesANameNoTagOfThatOwnerCarries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		written    string
		owner      string
		candidates []any
	}{
		{
			name:    "a login no tag of that name belongs to",
			written: "amb",
			owner:   "nobody",
			candidates: []any{
				[]detail{{"name", "amb"}, {"owner", "admin"}},
				[]detail{{"name", "amb"}, {"owner", "dev.limited"}},
			},
		},
		{
			name:       "the one tag of that name belonging to someone else",
			written:    "ready",
			owner:      "dev.limited",
			candidates: []any{[]detail{{"name", "Ready"}, {"owner", "admin"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := resolvingTags(t, tagsOfTwoOwners(), noDeletion(t))

			got := runWith(t, server.env(), "tag", "delete", "--name", tc.written, "--owned-by", tc.owner)

			want := faultDocument{
				code: "unknown_name",
				details: []detail{
					{"request", tagsRequest(server.url, resolvedTagFields, "-1")},
					{"unknown", []any{[]detail{
						{"tag", tc.written},
						{"owned_by", tc.owner},
						{"candidates", tc.candidates},
					}}},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestTagAddAndRemoveNarrowTheNameByTheOwnerOfTheTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		argv    []string
		serving func(*testing.T, http.HandlerFunc, http.HandlerFunc, http.HandlerFunc) *upstream
		write   http.HandlerFunc
		method  string
		path    string
	}{
		{
			name:    "a tagging",
			argv:    []string{"tag", "add", "DEV-7", "--name", "amb", "--owned-by", "dev.limited"},
			serving: addingATag,
			write:   respondWith(http.StatusOK, catalogueTag("10-23", "amb", "dev.limited")),
			method:  http.MethodPost,
			path:    tagsOfOwnerPath("issues", "DEV-7"),
		},
		{
			name:    "a removal",
			argv:    []string{"tag", "remove", "DEV-7", "--name", "amb", "--owned-by", "admin"},
			serving: takingATagOff,
			write:   deletionDone(),
			method:  http.MethodDelete,
			path:    tagOnOwnerPath("issues", "DEV-7", "10-22"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.serving(t, respondWith(http.StatusOK, issueNamed("DEV-7")), shownTags(), tc.write)

			got := runWith(t, server.env(), tc.argv...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{http.MethodGet, http.MethodGet, tc.method}, sentMethods(server))
			assert.Equal(t, []string{"/api/issues/DEV-7", tagsCollection, tc.path}, server.sentPaths())
		})
	}
}

func TestTagDeleteDestroysTheTagOfTheNamedOwnerOnTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)

	own := runWith(t, limited, "tag", "create", "--name", name)
	require.Equal(t, 0, own.code, "stderr: %s", own.stderr)
	t.Cleanup(func() { removeTagIfPresent(t, limited, name, "dev.limited") })

	ambiguousForTheLimited := runWith(t, dev.env(), "tag", "create", "--name", name, "--visible-for", everyoneRegistered)
	require.Equal(t, 0, ambiguousForTheLimited.code, "stderr: %s", ambiguousForTheLimited.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	before := len(dev.requests())

	got := runWith(t, limited, "tag", "delete", "--name", name, "--owned-by", "dev.limited")

	want := "name: " + strconv.Quote(name) + "\nowner:\n  login: \"dev.limited\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethodsFrom(dev, before))
	assert.True(t, tagIsListed(t, dev, name), "the admin's tag went with the one of the token that named it")
}

func removeTagIfPresent(t *testing.T, env []string, name, owner string) {
	t.Helper()
	got := runInContext(t, context.Background(), env, "tag", "delete", "--name", name, "--owned-by", owner)
	if got.code != 0 {
		assert.Contains(t, got.stderr, "unknown_name", "the tag was left on the dev instance")
	}
}
