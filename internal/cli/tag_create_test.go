package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const createdTagFields = tagFields +
	",updateSharingSettings(permittedGroups(name),permittedUsers(login))" +
	",tagSharingSettings(permittedGroups(name),permittedUsers(login))"

func tagCreationRequest(address, fields string) string {
	return "POST " + address + "/api/tags?fields=" + fields
}

func creatingATag(t *testing.T, creation http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a creation of a tag sends one POST and nothing else") {
			return
		}
		creation(w, r)
	})
}

func madeTag(name string) string {
	return sharedTag(name, nil, nil)
}

func TestTagCreateRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an empty name", argv: []string{"--name", ""}},
		{name: "the name given twice", argv: []string{"--name", "a", "--name", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), append([]string{"tag", "create"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTagCreatePrintsTheTagTheServerMade(t *testing.T) {
	t.Parallel()
	server := creatingATag(t, fake.JSON(http.StatusOK, madeTag("[bug] fix login")))

	got := runWith(t, server.Env(), "tag", "create", "--name", "[bug] fix login")

	want := `name: "[bug] fix login"` + "\n" +
		"owner:\n  login: \"admin\"\n" +
		"readSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n" +
		"updateSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n" +
		"tagSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/tags?fields=" + createdTagFields}, server.Targets())
	assert.Equal(t, []string{`{"name":"[bug] fix login"}`}, server.Bodies())
}

func TestTagCreateRefusesANameTheServerKeptAsAnother(t *testing.T) {
	t.Parallel()
	server := creatingATag(t, fake.JSON(http.StatusOK, madeTag("early")))

	got := runWith(t, server.Env(), "tag", "create", "--name", "Early", "--fields", "name")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", tagCreationRequest(server.URL, "name")},
			{"tag", "Early"},
			{"mismatch", []any{[]detail{{"field", "name"}, {"expected", "Early"}, {"actual", "early"}}}},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
}

func TestTagCreateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	const duplicate = `{"error":"invalid_properties","error_description":"Property Tag.name is invalid",` +
		`"error_children":[{"error":"Tag.name-is-invalid","error_field":"name"}]}`
	tests := []struct {
		name    string
		status  int
		body    string
		code    string
		details []detail
	}{
		{
			name:   "a name the token already owns a tag under",
			status: http.StatusBadRequest,
			body:   duplicate,
			code:   "rejected",
			details: []detail{
				{"upstream_error", "invalid_properties"},
				{"upstream_message", "Property Tag.name is invalid"},
				{"upstream_body", duplicate},
			},
		},
		{
			name:   "a token that may not make tags",
			status: http.StatusForbidden,
			body:   `{"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`,
			code:   "denied",
			details: []detail{
				{"upstream_error", "Forbidden"},
				{"upstream_message", "HTTP 403 Forbidden"},
				authFromEnv(),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creatingATag(t, fake.JSON(tc.status, tc.body))

			got := runWith(t, server.Env(), "tag", "create", "--name", "Early")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", tagCreationRequest(server.URL, createdTagFields)},
					{"upstream_status", tc.status},
				}, tc.details...),
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestTagCreateIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := creatingATag(t, breakOff)

	got := runWith(t, server.Env(), "tag", "create", "--name", "Early")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", tagCreationRequest(server.URL, createdTagFields)}}, found.details)
	assert.Empty(t, got.stdout)
}
