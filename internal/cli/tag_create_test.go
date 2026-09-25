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

func TestTagCreateRefusesAnEmptyName(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "tag", "create", "--name", "")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
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
