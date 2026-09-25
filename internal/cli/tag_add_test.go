package cli_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const tagsCollection = "/api/tags"

const taggedOwnerFields = "idReadable"

func tagsOfOwnerPath(collection, readable string) string {
	return "/api/" + collection + "/" + readable + "/tags"
}

func taggingRequest(address, collection, readable, fields string) string {
	return "POST " + address + tagsOfOwnerPath(collection, readable) + "?fields=" + fields
}

func addingATag(t *testing.T, owner, catalogue, tagging http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
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
	return fake.JSON(http.StatusOK, tagsOfTwoOwners())
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
		{name: "an empty name", argv: []string{"DEV-7", "--name", ""}},
		{name: "a name that is no UTF-8", argv: []string{"DEV-7", "--name", "\xff"}},
		{name: "the name given twice", argv: []string{"DEV-7", "--name", "a", "--name", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"tag", "add"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
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
			server := addingATag(t, fake.JSON(http.StatusOK, tc.owner), shownTags(),
				fake.JSON(http.StatusOK, catalogueTag("10-5", "Ready", "admin")))

			got := runWith(t, server.Env(), "tag", "add", tc.written, "--name", "ready")

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
			}, server.Paths())
			assert.Equal(t, []string{taggedOwnerFields, resolvedTagFields, resolvedTagFields}, server.Fields())
			assert.Equal(t, []string{"", "", `{"id":"10-5"}`}, server.Bodies(),
				"the server answers 400 to a body with the name")
			assert.NotContains(t, strings.Join(server.Paths(), " "), tc.apart)
			requireResolvedWithoutTheServer(t, server, "ready")
		})
	}
}

func TestTagAddPrintsTheTagTheWriteAnsweredWith(t *testing.T) {
	t.Parallel()
	server := addingATag(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		fake.JSON(http.StatusOK, catalogueTag("10-5", "Готово", "dev.limited")))

	got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", "ready")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	want := "idReadable: \"DEV-7\"\n" + "added:\n  name: \"Готово\"\n  owner:\n    login: \"dev.limited\"\n"
	assert.Equal(t, want, got.stdout, "the document was built from the read that resolved the name")
}

func TestTagAddRefusesATagOtherThanTheOneResolved(t *testing.T) {
	t.Parallel()
	server := addingATag(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		fake.JSON(http.StatusOK, catalogueTag("10-6", "Ready", "admin")))

	got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", "ready")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, []detail{
		{"request", taggingRequest(server.URL, "issues", "DEV-7", resolvedTagFields)},
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
			server := addingATag(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(),
				fake.JSON(tc.status, tc.said))

			got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", "ready")

			found := requireFault(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, detail{"request", taggingRequest(server.URL, "issues", "DEV-7", resolvedTagFields)},
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
			owner:   fake.JSON(http.StatusNotFound, entityNotFound("DEV-7")),
			written: "ready",
			code:    "not_found",
			methods: []string{http.MethodGet},
			paths:   []string{"/api/issues/DEV-7"},
		},
		{
			name:    "a name no tag the token is shown carries",
			owner:   fake.JSON(http.StatusOK, issueNamed("DEV-7")),
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

			got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", tc.written)

			assert.Equal(t, tc.code, requireFault(t, got).code)
			assert.Equal(t, tc.methods, sentMethods(server))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}
