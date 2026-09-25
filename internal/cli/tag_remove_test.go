package cli_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tagOnOwnerPath(collection, readable, tag string) string {
	return tagsOfOwnerPath(collection, readable) + "/" + tag
}

func tagRemovalRequest(address, collection, readable, tag string) string {
	return "DELETE " + address + tagOnOwnerPath(collection, readable, tag)
}

func takingATagOff(t *testing.T, owner, catalogue, removal http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			removal(w, r)
		case r.URL.Path == tagsCollection:
			catalogue(w, r)
		default:
			owner(w, r)
		}
	})
}

func TestTagRemoveTakesTheTagOffTheOwnerAndNotOutOfTheInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		written    string
		owner      string
		collection string
		readable   string
		apart      string
	}{
		{
			name:       "an issue in lower case",
			written:    "dev-7",
			owner:      issueNamed("DEV-7"),
			collection: "issues",
			readable:   "DEV-7",
			apart:      "/api/articles",
		},
		{
			name:       "an article in mixed case",
			written:    "dev-A-7",
			owner:      articleNamed("DEV-A-7"),
			collection: "articles",
			readable:   "DEV-A-7",
			apart:      "/api/issues",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := takingATagOff(t, respondWith(http.StatusOK, tc.owner), shownTags(), deletionDone())

			got := runWith(t, server.env(), "tag", "remove", tc.written, "--name", "ready")

			want := "idReadable: " + strconv.Quote(tc.readable) + "\n" +
				"removed:\n  name: \"Ready\"\n  owner:\n    login: \"admin\"\n"
			assert.Equal(t, outcome{stdout: want}, got)

			assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
			assert.Equal(t, []string{
				"/api/" + tc.collection + "/" + tc.written,
				tagsCollection,
				tagOnOwnerPath(tc.collection, tc.readable, "10-5"),
			}, server.sentPaths())
			assert.Equal(t, []string{taggedOwnerFields, resolvedTagFields, ""}, server.sentFields())
			assert.Equal(t, []string{"", "", ""}, server.asks())
			assert.Empty(t, server.requests()[2].URL.RawQuery)
			assert.NotContains(t, strings.Join(server.sentPaths(), " "), tc.apart)
			assert.NotContains(t, strings.Join(server.sentPaths(), " "), tagDeletionPath("10-5"))
			requireResolvedWithoutTheServer(t, server, "ready")
		})
	}
}

func TestTagRemoveReadsWhatTheServerAnsweredTheRemovalWith(t *testing.T) {
	t.Parallel()
	const missing = `{"error":"Not Found","error_description":"Entity with id 10-5 not found"}`
	tests := []struct {
		name    string
		removal http.HandlerFunc
		code    string
		exit    int
		details []detail
	}{
		{
			name:    "a tag the owner does not carry",
			removal: respondWith(http.StatusNotFound, missing),
			code:    "not_found",
			exit:    1,
			details: []detail{
				{"upstream_status", 404},
				{"upstream_error", "Not Found"},
				{"upstream_message", "Entity with id 10-5 not found"},
			},
		},
		{
			name:    "an answer carrying a body where the call is answered with none",
			removal: respondWith(http.StatusOK, `{"x":1}`),
			code:    "upstream_invalid",
			exit:    2,
			details: []detail{
				{"upstream_status", 200},
				{"upstream_body", `{"x":1}`},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := takingATagOff(t, respondWith(http.StatusOK, issueNamed("DEV-7")), shownTags(), tc.removal)

			got := runWith(t, server.env(), "tag", "remove", "DEV-7", "--name", "ready")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.exit, got.code)
			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", tagRemovalRequest(server.url, "issues", "DEV-7", "10-5")},
					{"issue", "DEV-7"},
					{"tag", "ready"},
				}, tc.details...),
			}
			assert.Equal(t, want, found)
			assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

func TestTagRemoveLeavesTheTagStandingOnTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t)
	made := runWith(t, dev.env(), "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	issue := issueToTag(t, dev)
	hung := runWith(t, dev.env(), "tag", "add", issue, "--name", name)
	require.Equal(t, 0, hung.code, "stderr: %s", hung.stderr)

	got := runWith(t, dev.env(), "tag", "remove", issue, "--name", name)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	taken := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, issue, nodeAt(t, taken, "idReadable").Value)
	assert.Equal(t, name, nodeAt(t, taken, "removed", "name").Value)
	assert.Equal(t, "admin", nodeAt(t, taken, "removed", "owner", "login").Value)

	assert.True(t, tagIsListed(t, dev, name), "the tag is gone from the list after it came off the issue")
	assert.Empty(t, tagsOfTheIssue(t, dev, issue))

	before := len(dev.requests())
	again := runWith(t, dev.env(), "tag", "remove", issue, "--name", name)
	assert.Equal(t, "not_found", requireFault(t, again).code)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethodsFrom(dev, before))

	back := runWith(t, dev.env(), "tag", "add", issue, "--name", name)
	require.Equal(t, 0, back.code, "stderr: %s", back.stderr)
	assert.Equal(t, []string{name}, tagsOfTheIssue(t, dev, issue))

	destroyed := runWith(t, dev.env(), "tag", "delete", "--name", name)
	require.Equal(t, 0, destroyed.code, "stderr: %s", destroyed.stderr)
	assert.False(t, tagIsListed(t, dev, name), "the tag stands in the list after it was destroyed")
}

func TestTagRemoveTakesATagOffAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t)
	made := runWith(t, dev.env(), "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	article := articleToTag(t, dev)
	hung := runWith(t, dev.env(), "tag", "add", article, "--name", name)
	require.Equal(t, 0, hung.code, "stderr: %s", hung.stderr)

	got := runWith(t, dev.env(), "tag", "remove", article, "--name", name)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	taken := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, article, nodeAt(t, taken, "idReadable").Value)
	assert.Equal(t, name, nodeAt(t, taken, "removed", "name").Value)

	again := runWith(t, dev.env(), "tag", "remove", article, "--name", name)
	found := requireFault(t, again)
	assert.Equal(t, "not_found", found.code)

	assert.True(t, tagIsListed(t, dev, name), "the tag is gone from the list after it came off the article")
}

func tagIsListed(t *testing.T, dev *upstream, name string) bool {
	t.Helper()
	listed := requireTagListing(t, runWith(t, dev.env(), "tag", "list"))
	return slices.ContainsFunc(listed.Tags, func(record map[string]any) bool { return record["name"] == name })
}
