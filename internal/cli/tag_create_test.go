package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func sentTag(t *testing.T, u *fake.Server) map[string]any {
	t.Helper()
	asks := u.Bodies()
	require.Len(t, asks, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(asks[0]), &body))
	return body
}

func runesTheServerCutsOffTheEdges() []rune {
	return []rune{' ', '\t', '\n', '\r', '\v', '\f', 0x1C, 0x1D, 0x1E, 0x1F, 0xA0, 0x2028, 0x2029, 0x3000}
}

func TestTagCreateRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an empty name", argv: []string{"--name", ""}},
		{name: "a name that is no UTF-8", argv: []string{"--name", "\xff"}},
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

func TestTagCreateRefusesANameTheServerWouldCut(t *testing.T) {
	t.Parallel()
	type edge struct {
		name    string
		written string
	}
	var tests []edge
	for _, r := range runesTheServerCutsOffTheEdges() {
		tests = append(tests,
			edge{name: fmt.Sprintf("U+%04X at the beginning", r), written: string(r) + "карта"},
			edge{name: fmt.Sprintf("U+%04X at the end", r), written: "карта" + string(r)})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "tag", "create", "--name", tc.written)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests(), "the tag would already exist when the answer showed the cut name")
		})
	}
}

func TestTagCreateSendsTheNameAndNothingElse(t *testing.T) {
	t.Parallel()
	keptAsWrittenByTheServer := []struct {
		name    string
		written string
	}{
		{name: "brackets and a space", written: "[bug] fix login"},
		{name: "a tab inside the name", written: "два\tслова"},
		{name: "a line feed inside the name", written: "две\nстроки"},
		{name: "a carriage return inside the name", written: "две\rстроки"},
		{name: "a line separator inside the name", written: "две" + string(rune(0x2028)) + "строки"},
		{name: "a start of heading at the edge", written: string(rune(0x01)) + "карта"},
		{name: "a next line at the edge", written: "карта" + string(rune(0x85))},
		{name: "a zero width space at the edge", written: string(rune(0x200B)) + "карта"},
		{name: "a byte order mark at the edge", written: "карта" + string(rune(0xFEFF))},
	}
	for _, tc := range keptAsWrittenByTheServer {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creatingATag(t, fake.JSON(http.StatusOK, madeTag(tc.written)))

			got := runWith(t, server.Env(), "tag", "create", "--name", tc.written)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, map[string]any{"name": tc.written}, sentTag(t, server))
			assert.Equal(t, []string{createdTagFields}, server.Fields())
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
	assert.Equal(t, []string{"/api/tags"}, server.Paths())
}

func TestTagCreateRefusesANameTheServerKeptAsAnother(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		written  string
		kept     string
		mismatch []any
	}{
		{
			name:     "a rune cut off the end after all",
			written:  "карта" + string(rune(0x85)),
			kept:     madeTag("карта"),
			mismatch: []any{[]detail{{"field", "name"}, {"expected", "карта" + string(rune(0x85))}, {"actual", "карта"}}},
		},
		{
			name:     "another letter case",
			written:  "Карта",
			kept:     madeTag("карта"),
			mismatch: []any{[]detail{{"field", "name"}, {"expected", "Карта"}, {"actual", "карта"}}},
		},
		{
			name:     "no name on the tag at all",
			written:  "карта",
			kept:     `{"$type":"Tag","name":null,"owner":{"$type":"User","login":"admin"}}`,
			mismatch: []any{[]detail{{"field", "name"}, {"expected", "карта"}, {"actual", nil}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creatingATag(t, fake.JSON(http.StatusOK, tc.kept))

			got := runWith(t, server.Env(), "tag", "create", "--name", tc.written, "--fields", "name")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", tagCreationRequest(server.URL, "name")},
					{"tag", tc.written},
					{"mismatch", tc.mismatch},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestTagCreateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	const duplicate = `{"error":"invalid_properties","error_description":"Property Tag.name is invalid",` +
		`"error_children":[{"error":"Tag.name-is-invalid","error_description":"Property Tag.name is invalid",` +
		`"error_developer_message":"У пользователя уже есть тег с именем карта","error_field":"name"}]}`
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

			got := runWith(t, server.Env(), "tag", "create", "--name", "карта")

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

	got := runWith(t, server.Env(), "tag", "create", "--name", "карта")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", tagCreationRequest(server.URL, createdTagFields)}}, found.details)
	assert.Empty(t, got.stdout)
}

func TestTagCreateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := creatingATag(t, fake.JSON(http.StatusOK, madeTag("карта")))

	got := runWith(t, server.Env(), "tag", "create", "--name", "карта", "--fields", "owner(login)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "owner:\n  login: \"admin\"\n", got.stdout)
	assert.Equal(t, []string{"owner(login),name"}, server.Fields())
}
