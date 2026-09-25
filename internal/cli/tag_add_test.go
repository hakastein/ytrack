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

const tagsCollection = "/api/tags"

const taggedOwnerFields = "idReadable"

func tagsOfOwnerPath(collection, readable string) string {
	return "/api/" + collection + "/" + readable + "/tags"
}

func taggingRequest(address, collection, readable, fields string) string {
	return "POST " + address + tagsOfOwnerPath(collection, readable) + "?fields=" + fields
}

func addingATag(t *testing.T, owner, catalogue, tagging http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			tagging(w, r)
		case r.URL.Path == tagsCollection:
			catalogue(w, r)
		default:
			owner(w, r)
		}
	})
}

func shownTags() http.HandlerFunc {
	return respondWith(http.StatusOK, tagsOfTwoOwners())
}

func noTagging(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a tagging reached the server", "%s %s", r.Method, r.URL)
	}
}

func TestTagAddRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: nil},
		{name: "an owner and no name", argv: []string{"DEV-7"}},
		{name: "a name written as an argument", argv: []string{"DEV-7", "Ready"}},
		{name: "an argument beside the name", argv: []string{"DEV-7", "y", "--name", "Ready"}},
		{name: "an empty name", argv: []string{"DEV-7", "--name", ""}},
		{name: "a name that is no UTF-8", argv: []string{"DEV-7", "--name", "\xff"}},
		{name: "the name given twice", argv: []string{"DEV-7", "--name", "a", "--name", "b"}},
		{name: "an expression of fields", argv: []string{"DEV-7", "--name", "x", "--fields", "name"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"tag", "add"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTagAddReadsTheOwnerThenResolvesTheNameThenWrites(t *testing.T) {
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
			server := addingATag(t, respondWith(http.StatusOK, tc.owner), shownTags(),
				respondWith(http.StatusOK, catalogueTag("10-5", "Ready", "admin")))

			got := runWith(t, server.env(), "tag", "add", tc.written, "--name", "ready")

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			want := "idReadable: " + strconv.Quote(tc.readable) + "\n" +
				"added:\n  name: \"Ready\"\n  owner:\n    login: \"admin\"\n"
			assert.Equal(t, want, got.stdout)

			assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodPost}, sentMethods(server))
			assert.Equal(t, []string{
				"/api/" + tc.collection + "/" + tc.written,
				tagsCollection,
				tagsOfOwnerPath(tc.collection, tc.readable),
			}, server.sentPaths())
			assert.Equal(t, []string{taggedOwnerFields, resolvedTagFields, resolvedTagFields}, server.sentFields())
			assert.Equal(t, []string{"", "", `{"id":"10-5"}`}, server.asks(),
				"the server answers 400 to a body with the name")
			assert.NotContains(t, strings.Join(server.sentPaths(), " "), tc.apart)
			requireResolvedWithoutTheServer(t, server, "ready")
		})
	}
}

func TestTagAddPrintsTheTagTheWriteAnsweredWith(t *testing.T) {
	t.Parallel()
	server := addingATag(t, respondWith(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		respondWith(http.StatusOK, catalogueTag("10-5", "Готово", "dev.limited")))

	got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", "ready")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	want := "idReadable: \"DEV-7\"\n" + "added:\n  name: \"Готово\"\n  owner:\n    login: \"dev.limited\"\n"
	assert.Equal(t, want, got.stdout, "the document was built from the read that resolved the name")
}

func TestTagAddRefusesATagOtherThanTheOneResolved(t *testing.T) {
	t.Parallel()
	server := addingATag(t, respondWith(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		respondWith(http.StatusOK, catalogueTag("10-6", "Ready", "admin")))

	got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", "ready")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, []detail{
		{"request", taggingRequest(server.url, "issues", "DEV-7", resolvedTagFields)},
		{"issue", "DEV-7"},
		{"tag", "ready"},
		{"upstream_status", 200},
		{"upstream_body", catalogueTag("10-6", "Ready", "admin")},
	}, found.details)
}

func TestTagAddReadsWhatTheServerAnsweredTheWriteWith(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		said    string
		code    string
		message string
	}{
		{
			name:    "a token shown the tag and not allowed to hang it",
			status:  http.StatusForbidden,
			said:    `{"error":"Forbidden","error_description":"Не удалось отметить задачу тегом"}`,
			code:    "denied",
			message: "Не удалось отметить задачу тегом",
		},
		{
			name:    "a tag that went away between the read and the write",
			status:  http.StatusBadRequest,
			said:    `{"error":"bad_request","error_description":"Сущность типа Tag с идентификатором 10-5 не найдена"}`,
			code:    "rejected",
			message: "Сущность типа Tag с идентификатором 10-5 не найдена",
		},
		{
			name:    "an owner that went away between the read and the write",
			status:  http.StatusNotFound,
			said:    `{"error":"Not Found","error_description":"Entity with id DEV-7 not found"}`,
			code:    "not_found",
			message: "Entity with id DEV-7 not found",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := addingATag(t, respondWith(http.StatusOK, issueNamed("DEV-7")), shownTags(),
				respondWith(tc.status, tc.said))

			got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", "ready")

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, detail{"request", taggingRequest(server.url, "issues", "DEV-7", resolvedTagFields)},
				found.details[0])
			assert.Equal(t, []detail{{"issue", "DEV-7"}, {"tag", "ready"}}, found.details[1:3])
			assert.Equal(t, tc.message, detailNamed(t, found, "upstream_message"))
		})
	}
}

func TestTagAddSendsNoWriteWhereAReadBeforeItRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		owner   http.HandlerFunc
		written string
		code    string
		methods []string
		paths   []string
	}{
		{
			name:    "an owner the read does not find",
			owner:   respondWith(http.StatusNotFound, entityNotFound("DEV-7")),
			written: "ready",
			code:    "not_found",
			methods: []string{http.MethodGet},
			paths:   []string{"/api/issues/DEV-7"},
		},
		{
			name:    "a name no tag the token is shown carries",
			owner:   respondWith(http.StatusOK, issueNamed("DEV-7")),
			written: "redy",
			code:    "unknown_name",
			methods: []string{http.MethodGet, http.MethodGet},
			paths:   []string{"/api/issues/DEV-7", tagsCollection},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := addingATag(t, tc.owner, shownTags(), noTagging(t))

			got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", tc.written)

			assert.Equal(t, tc.code, requireFault(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
			assert.Equal(t, tc.paths, server.sentPaths())
		})
	}
}

func TestTagAddHangsATagOnAnIssueAndAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t)
	made := runWith(t, dev.env(), "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	issue := issueToTag(t, dev)
	article := articleToTag(t, dev)

	hung := runWith(t, dev.env(), "tag", "add", issue, "--name", name)

	require.Equal(t, 0, hung.code, "stderr: %s", hung.stderr)
	assert.Equal(t, issue, nodeAt(t, requireMapping(t, "stdout", hung.stdout), "idReadable").Value)
	assert.Equal(t, name, nodeAt(t, requireMapping(t, "stdout", hung.stdout), "added", "name").Value)
	assert.Equal(t, []string{name}, tagsOfTheIssue(t, dev, issue))

	again := runWith(t, dev.env(), "tag", "add", issue, "--name", name)
	require.Equal(t, 0, again.code, "stderr: %s", again.stderr)
	assert.Equal(t, []string{name}, tagsOfTheIssue(t, dev, issue), "the tag hangs twice after a second call")

	onTheArticle := runWith(t, dev.env(), "tag", "add", article, "--name", name)
	require.Equal(t, 0, onTheArticle.code, "stderr: %s", onTheArticle.stderr)
	assert.Equal(t, article, nodeAt(t, requireMapping(t, "stdout", onTheArticle.stdout), "idReadable").Value)
	assert.Equal(t, []string{name}, tagsOfTheArticle(t, dev, article))
}

func TestTagAddRefusesANameThePolygonHasNoTagUnder(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := issueToTag(t, dev)
	before := len(dev.requests())

	got := runWith(t, dev.env(), "tag", "add", issue, "--name", contractTagName(t)+" nope")

	assert.Equal(t, "unknown_name", requireFault(t, got).code)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethodsFrom(dev, before))
}

func TestTagAddRefusesAnOwnerTheTokenCannotReachOnTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)
	made := runWith(t, limited, "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, limited, name, "dev.limited") })
	issue := issueToTag(t, dev)

	for _, tc := range []struct {
		name  string
		owner string
	}{
		{name: "an issue of the admin the token is not shown", owner: issue},
		{name: "an issue nobody filed", owner: "DEV-99999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(dev.requests())

			got := runWith(t, limited, "tag", "add", tc.owner, "--name", name)

			assert.Equal(t, "not_found", requireFault(t, got).code)
			assert.Equal(t, []string{http.MethodGet}, sentMethodsFrom(dev, before))
		})
	}
}

func issueToTag(t *testing.T, dev *upstream) string {
	t.Helper()
	argv := append([]string{"issue", "create", "DEV", "--summary", contractTagName(t)}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

func articleToTag(t *testing.T, dev *upstream) string {
	t.Helper()
	article := fileArticle(t, dev, contractTagName(t))
	t.Cleanup(func() { removeArticle(t, dev, article) })
	return article
}

func tagsOfTheIssue(t *testing.T, dev *upstream, readable string) []string {
	t.Helper()
	return tagNamesOf(t, runWith(t, dev.env(), "issue", "show", readable, "--comments=0", "--fields", "tags(name)"))
}

func tagsOfTheArticle(t *testing.T, dev *upstream, readable string) []string {
	t.Helper()
	return tagNamesOf(t, runWith(t, dev.env(), "article", "show", readable, "--comments=0", "--fields", "tags(name)"))
}

func tagNamesOf(t *testing.T, got outcome) []string {
	t.Helper()
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	names := []string{}
	for _, tag := range nodeAt(t, requireMapping(t, "stdout", got.stdout), "tags").Content {
		names = append(names, nodeAt(t, tag, "name").Value)
	}
	slices.Sort(names)
	return names
}
