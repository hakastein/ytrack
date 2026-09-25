package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

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

func TestTagAddRefusesAnEmptyName(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", "")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestTagAddReadsTheOwnerThenResolvesTheNameThenWrites(t *testing.T) {
	t.Parallel()
	server := addingATag(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		fake.JSON(http.StatusOK, catalogueTag("10-5", "Ready", "first")))

	got := runWith(t, server.Env(), "tag", "add", "dev-7", "--name", "ready")

	want := "idReadable: \"DEV-7\"\n" + "added:\n  name: \"Ready\"\n  owner:\n    login: \"first\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodPost}, server.Methods())
	assert.Equal(t, []string{
		"/api/issues/dev-7?fields=" + taggedOwnerFields,
		tagsCollection + "?fields=" + resolvedTagFields + "&$top=-1",
		tagsOfOwnerPath("issues", "DEV-7") + "?fields=" + resolvedTagFields,
	}, server.Targets(t))
	assert.Equal(t, []string{"", "", `{"id":"10-5"}`}, server.Bodies(),
		"the server answers 400 to a body with the name")
}

func TestTagAddRefusesWhatTheReadsBeforeTheWriteDoNotAllow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		owner   string
		written string
		want    func(address string) faultDocument
		methods []string
	}{
		{
			name:    "an owner the read names by two dots",
			owner:   issueNamed(".."),
			written: "ready",
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", issueRequest(address, "DEV-7", taggedOwnerFields)},
					{"upstream_status", 200},
					{"upstream_body", issueNamed("..")},
				}}
			},
			methods: []string{http.MethodGet},
		},
		{
			name:    "a name no tag carries",
			owner:   issueNamed("DEV-7"),
			written: "redy",
			want: func(address string) faultDocument {
				return faultDocument{code: "unknown_name", details: []detail{
					{"request", tagsRequest(address, resolvedTagFields, "-1")},
					{"unknown", []any{[]detail{{"tag", "redy"}, {"nearest", []any{"Ready"}}}}},
				}}
			},
			methods: []string{http.MethodGet, http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := addingATag(t, fake.JSON(http.StatusOK, tc.owner), shownTags(), fake.Unexpected(t))

			got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", tc.written)

			assert.Equal(t, tc.want(server.URL), requireFault(t, got))
			assert.Equal(t, tc.methods, server.Methods())
		})
	}
}

func TestTagAddRefusesATagOtherThanTheOneResolved(t *testing.T) {
	t.Parallel()
	server := addingATag(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		fake.JSON(http.StatusOK, catalogueTag("10-6", "Ready", "first")))

	got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", "ready")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", taggingRequest(server.URL, "issues", "DEV-7", resolvedTagFields)},
			{"issue", "DEV-7"},
			{"tag", "ready"},
			{"upstream_status", 200},
			{"upstream_body", catalogueTag("10-6", "Ready", "first")},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}

func TestTagAddRefusesATagGoneBetweenTheReadAndTheWrite(t *testing.T) {
	t.Parallel()
	const said = `{"error":"bad_request","error_description":"No tag 10-5"}`
	server := addingATag(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(),
		fake.JSON(http.StatusBadRequest, said))

	got := runWith(t, server.Env(), "tag", "add", "DEV-7", "--name", "ready")

	want := faultDocument{
		code: "rejected",
		details: []detail{
			{"request", taggingRequest(server.URL, "issues", "DEV-7", resolvedTagFields)},
			{"issue", "DEV-7"},
			{"tag", "ready"},
			{"upstream_status", 400},
			{"upstream_error", "bad_request"},
			{"upstream_message", "No tag 10-5"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
}
