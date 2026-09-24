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

// The collection every tag of a token stands in, which is where the resolver reads and where nothing a tagging
// sends ever goes.
const tagsCollection = "/api/tags"

// The one name the read before a tagging asks for: the readable id the write is addressed by and the document
// prints.
const taggedOwnerFields = "idReadable"

// Where the tags of an owner stand, which is the path of every tagging and of every removal.
func tagsOfOwnerPath(collection, readable string) string {
	return "/api/" + collection + "/" + readable + "/tags"
}

func taggingRequest(address, collection, readable, fields string) string {
	return "POST " + address + tagsOfOwnerPath(collection, readable) + "?fields=" + fields
}

// hangingATag is the server of a tagging: owner answers the read that settles the readable id, catalogue the
// read that resolves the name, and tagging the POST that follows the two.
func hangingATag(t *testing.T, owner, catalogue, tagging http.HandlerFunc) *upstream {
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

// The polygon was measured answering one token with two tags named amb, so the catalogue a scenario resolves
// against is the same one the deletion is resolved against.
func shownTags() http.HandlerFunc {
	return answer(http.StatusOK, tagsOfTwoOwners())
}

// noTagging stands for the write a refusal before it must not send.
func noTagging(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a tagging reached the server", "%s %s", r.Method, r.URL)
	}
}

// A tagging names the owner by the one argument and the tag by --name, and takes nothing else: a second
// argument would swallow a name beginning with a dash, and no flag of another verb stands here. An empty name
// and no name at all are different mistakes, and only the flag tells them apart.
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
		// The document is the owner and the tag that was hung, so there is no tree for a caller to choose.
		{name: "an expression of fields", argv: []string{"DEV-7", "--name", "x", "--fields", "name"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"tag", "add"}, tc.argv...)...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The whole of the call on the wire, for each kind of owner: the owner is read first, the catalogue after
// it, and the write goes to the readable id the server gave rather than to the argument the caller typed. The
// body is the id the name resolved to and not one key more — {name} there is answered 400 — and no request of
// either kind ever reaches the API of the other.
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
			server := hangingATag(t, answer(http.StatusOK, tc.owner), shownTags(),
				answer(http.StatusOK, shownTag("10-5", "Ready", "admin")))

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
			assert.Equal(t, []string{"", "", `{"id":"10-5"}`}, server.asks())
			assert.NotContains(t, strings.Join(server.sentPaths(), " "), tc.apart)
			requireResolvedWithoutTheServer(t, server, "ready")
		})
	}
}

// What is printed is the tag the write answered with and never the one the read before it found: the id is
// the whole of what the answer is held to, and the name and the owner beside it are the server's own word about
// that id. A tag renamed or handed to another owner between the two requests prints as the server has it, and
// the document the caller keeps is about the moment the tag went on.
func TestTagAddPrintsTheTagTheWriteAnsweredWith(t *testing.T) {
	t.Parallel()
	server := hangingATag(t, answer(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		answer(http.StatusOK, shownTag("10-5", "Готово", "dev.limited")))

	got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", "ready")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	want := "idReadable: \"DEV-7\"\n" + "added:\n  name: \"Готово\"\n  owner:\n    login: \"dev.limited\"\n"
	assert.Equal(t, want, got.stdout, "the document was built from the read that resolved the name")
}

// A 200 says the server took the body, not that the tag it hung is the tag the name resolved to. The owner
// carries something by then, which is what the exit code of 2 says, and the refusal names both halves of the
// call: the owner by the id the read gave and the tag by the name that was written.
func TestTagAddRefusesATagOtherThanTheOneResolved(t *testing.T) {
	t.Parallel()
	server := hangingATag(t, answer(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		answer(http.StatusOK, shownTag("10-6", "Ready", "admin")))

	got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", "ready")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Equal(t, []detail{
		{"request", taggingRequest(server.url, "issues", "DEV-7", resolvedTagFields)},
		{"issue", "DEV-7"},
		{"tag", "ready"},
		{"upstream_status", 200},
		{"upstream_body", shownTag("10-6", "Ready", "admin")},
	}, found.details)
}

// What the server answers the write with is read the way a status is read everywhere, and the words it
// used pass on as they stand: being shown a tag is not being allowed to hang it, and YouTrack keeps a set of
// its own for that right. Every one of these names the owner and the tag the call was about.
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
			server := hangingATag(t, answer(http.StatusOK, issueNamed("DEV-7")), shownTags(),
				answer(tc.status, tc.said))

			got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", "ready")

			found := requireRefusal(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, detail{"request", taggingRequest(server.url, "issues", "DEV-7", resolvedTagFields)},
				found.details[0])
			assert.Equal(t, []detail{{"issue", "DEV-7"}, {"tag", "ready"}}, found.details[1:3])
			assert.Equal(t, tc.message, detailNamed(t, found, "upstream_message"))
		})
	}
}

// Each read stands before the next, and a refusal from either leaves the owner exactly as it was: an owner
// the token cannot see is answered before a catalogue of tags is ever asked for, and a name that resolves to no
// one tag is answered before anything is written.
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
			owner:   answer(http.StatusNotFound, entityNotFound("DEV-7")),
			written: "ready",
			code:    "not_found",
			methods: []string{http.MethodGet},
			paths:   []string{"/api/issues/DEV-7"},
		},
		{
			name:    "a name no tag the token is shown carries",
			owner:   answer(http.StatusOK, issueNamed("DEV-7")),
			written: "redy",
			code:    "unknown_name",
			methods: []string{http.MethodGet, http.MethodGet},
			paths:   []string{"/api/issues/DEV-7", tagsCollection},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := hangingATag(t, tc.owner, shownTags(), noTagging(t))

			got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", tc.written)

			assert.Equal(t, tc.code, requireRefusal(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
			assert.Equal(t, tc.paths, server.sentPaths())
		})
	}
}

// A tag of this scenario's own hung on an issue and on an article of its own, against the
// polygon: the owner carries it afterwards, and hanging it a second time is answered as the first call was and
// leaves it carried once. Nothing of the polygon's own is touched — the tag, the issue and the article are all
// made here and taken away again.
func TestTagAddHangsATagOnAnIssueAndAnArticleOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t)
	made := runWith(t, dev.env(), "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	issue := taggedIssue(t, dev)
	article := taggedArticle(t, dev)

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

// A name the polygon has no tag under is answered by the resolver: the owner was read and the catalogue
// after it, and nothing was written. The name carries this test's own words, so no fixture can answer to it.
func TestTagAddRefusesANameThePolygonHasNoTagUnder(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := taggedIssue(t, dev)
	before := len(dev.requests())

	got := runWith(t, dev.env(), "tag", "add", issue, "--name", contractTagName(t)+" nope")

	assert.Equal(t, "unknown_name", requireRefusal(t, got).code)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, sentMethodsFrom(dev, before))
}

// The read before the write is what turns an owner the token cannot reach into one refusal and one
// request: an issue the limited token may not see and an issue nobody filed are answered the same way, and
// neither costs a catalogue of tags.
func TestTagAddRefusesAnOwnerTheTokenCannotReachOnTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)
	made := runWith(t, limited, "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, limited, name, "dev.limited") })
	issue := taggedIssue(t, dev)

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

			assert.Equal(t, "not_found", requireRefusal(t, got).code)
			assert.Equal(t, []string{http.MethodGet}, sentMethodsFrom(dev, before))
		})
	}
}

// taggedIssue is the fixture of a contract test that needs an issue of its own to hang tags on: it is filed by
// the command that files issues, with the custom fields DEV requires, and taken away again afterwards.
func taggedIssue(t *testing.T, dev *upstream) string {
	t.Helper()
	argv := append([]string{"issue", "create", "DEV", "--summary", contractTagName(t)}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

// taggedArticle is the same fixture for the knowledge base.
func taggedArticle(t *testing.T, dev *upstream) string {
	t.Helper()
	article := fileArticle(t, dev, contractTagName(t))
	t.Cleanup(func() { removeArticle(t, dev, article) })
	return article
}

// tagsOfTheIssue is the names of the tags the polygon holds on that issue, read back by the show of it, which
// is where the tags of an owner are read at all.
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
