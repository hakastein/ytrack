package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func tagOnOwnerPath(collection, readable, tag string) string {
	return tagsOfOwnerPath(collection, readable) + "/" + tag
}

func tagRemovalRequest(address, collection, readable, tag string) string {
	return "DELETE " + address + tagOnOwnerPath(collection, readable, tag)
}

func takingATagOff(t *testing.T, owner, catalogue, removal http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
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
	server := takingATagOff(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(), deletionDone())

	got := runWith(t, server.Env(), "tag", "remove", "dev-7", "--name", "ready")

	want := "idReadable: \"DEV-7\"\n" + "removed:\n  name: \"Ready\"\n  owner:\n    login: \"first\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
	assert.Equal(t, []string{
		"/api/issues/dev-7?fields=" + taggedOwnerFields,
		tagsCollection + "?fields=" + resolvedTagFields + "&$top=-1",
		tagOnOwnerPath("issues", "DEV-7", "10-5") + "?",
	}, server.Targets())
	assert.Equal(t, []string{"", "", ""}, server.Bodies())
}

func TestTagRemoveRefusesWhatTheReadsBeforeTheRemovalDoNotAllow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		owner   string
		written string
		want    func(address string) faultDocument
		methods []string
	}{
		{
			name:    "an owner the read names by the id of an article",
			owner:   issueNamed("DEV-A-7"),
			written: "ready",
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", issueRequest(address, "DEV-7", taggedOwnerFields)},
					{"upstream_status", 200},
					{"upstream_body", issueNamed("DEV-A-7")},
				}}
			},
			methods: []string{http.MethodGet},
		},
		{
			name:    "a name two tags carry",
			owner:   issueNamed("DEV-7"),
			written: "shared",
			want: func(address string) faultDocument {
				return faultDocument{code: "unknown_name", details: []detail{
					{"request", tagsRequest(address, resolvedTagFields, "-1")},
					{"ambiguous", []any{[]detail{{"tag", "shared"}, {"candidates", []any{
						[]detail{{"name", "Shared"}, {"owner", "first"}},
						[]detail{{"name", "Shared"}, {"owner", "second"}},
					}}}}},
				}}
			},
			methods: []string{http.MethodGet, http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := takingATagOff(t, fake.JSON(http.StatusOK, tc.owner), shownTags(), noDeletion(t))

			got := runWith(t, server.Env(), "tag", "remove", "DEV-7", "--name", tc.written)

			assert.Equal(t, tc.want(server.URL), requireFault(t, got))
			assert.Equal(t, tc.methods, sentMethods(server))
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
			removal: fake.JSON(http.StatusNotFound, missing),
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
			removal: fake.JSON(http.StatusOK, `{"x":1}`),
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
			server := takingATagOff(t, fake.JSON(http.StatusOK, issueNamed("DEV-7")), shownTags(), tc.removal)

			got := runWith(t, server.Env(), "tag", "remove", "DEV-7", "--name", "ready")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.exit, got.code)
			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", tagRemovalRequest(server.URL, "issues", "DEV-7", "10-5")},
					{"issue", "DEV-7"},
					{"tag", "ready"},
				}, tc.details...),
			}
			assert.Equal(t, want, found)
			assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}
