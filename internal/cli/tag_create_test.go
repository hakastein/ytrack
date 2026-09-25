package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const createdTagFields = tagFields +
	",updateSharingSettings(permittedGroups(name),permittedUsers(login))" +
	",tagSharingSettings(permittedGroups(name),permittedUsers(login))"

func tagCreationRequest(address, fields string) string {
	return "POST " + address + "/api/tags?fields=" + fields
}

func creatingATag(t *testing.T, creation http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a creation of a tag sends one POST and nothing else") {
			return
		}
		creation(w, r)
	})
}

func madeTag(name string) string {
	return sharedTag(name, nil, nil)
}

func sentTag(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	asks := u.asks()
	require.Len(t, asks, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(asks[0]), &body))
	return body
}

func runesTheServerCutsOffTheEdges() []rune {
	return []rune{' ', '\t', '\n', '\r', '\v', '\f', 0x1C, 0x1D, 0x1E, 0x1F, 0xA0, 0x2028, 0x2029, 0x3000}
}

func contractTagName(t *testing.T) string {
	t.Helper()
	return "ytrack contract " + t.Name()
}

func removeTag(t *testing.T, ownersEnv []string, name, owner string) {
	t.Helper()
	notCancelledAtCleanup := context.Background()
	deleted := runInContext(t, notCancelledAtCleanup, ownersEnv, "tag", "delete", "--name", name)
	want := "name: " + strconv.Quote(name) + "\nowner:\n  login: " + strconv.Quote(owner) + "\n"
	assert.Equal(t, outcome{stdout: want}, deleted)
}

func TestTagCreateRefusesACallOfAnyOtherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "nothing at all", argv: nil},
		{name: "a name written as an argument", argv: []string{"карта"}},
		{name: "two arguments", argv: []string{"a", "b"}},
		{name: "an empty name", argv: []string{"--name", ""}},
		{name: "a name that is no UTF-8", argv: []string{"--name", "\xff"}},
		{name: "the name given twice", argv: []string{"--name", "a", "--name", "b"}},
		{name: "a limit, which belongs to the list", argv: []string{"--name", "x", "--limit", "5"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), append([]string{"tag", "create"}, tc.argv...)...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
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
			server := serveNothing(t)

			got := runWith(t, server.env(), "tag", "create", "--name", tc.written)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests(), "the tag would already exist when the answer showed the cut name")
		})
	}
}

func TestTagCreateHelpNamesTheDefault(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"tag", "create", "--help"})

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, createdTagFields)
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
		{name: "a name beginning with a dash", written: "-x"},
	}
	for _, tc := range keptAsWrittenByTheServer {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creatingATag(t, respondWith(http.StatusOK, madeTag(tc.written)))

			got := runWith(t, server.env(), "tag", "create", "--name", tc.written)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, map[string]any{"name": tc.written}, sentTag(t, server))
			assert.Equal(t, []string{createdTagFields}, server.sentFields())
		})
	}
}

func TestTagCreatePrintsTheTagTheServerMade(t *testing.T) {
	t.Parallel()
	server := creatingATag(t, respondWith(http.StatusOK, madeTag("[bug] fix login")))

	got := runWith(t, server.env(), "tag", "create", "--name", "[bug] fix login")

	want := `name: "[bug] fix login"` + "\n" +
		"owner:\n  login: \"admin\"\n" +
		"readSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n" +
		"updateSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n" +
		"tagSharingSettings:\n  permittedGroups: []\n  permittedUsers: []\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Equal(t, []string{"/api/tags"}, server.sentPaths())
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
			server := creatingATag(t, respondWith(http.StatusOK, tc.kept))

			got := runWith(t, server.env(), "tag", "create", "--name", tc.written, "--fields", "name")

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", tagCreationRequest(server.url, "name")},
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
			server := creatingATag(t, respondWith(tc.status, tc.body))

			got := runWith(t, server.env(), "tag", "create", "--name", "карта")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", tagCreationRequest(server.url, createdTagFields)},
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

	got := runWith(t, server.env(), "tag", "create", "--name", "карта")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", tagCreationRequest(server.url, createdTagFields)}}, found.details)
	assert.Empty(t, got.stdout)
}

func TestTagCreateChecksMoreThanItPrints(t *testing.T) {
	t.Parallel()
	server := creatingATag(t, respondWith(http.StatusOK, madeTag("карта")))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--fields", "owner(login)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "owner:\n  login: \"admin\"\n", got.stdout)
	assert.Equal(t, []string{"owner(login),name"}, server.sentFields())
}

func TestTagCreateMakesATagOfTheDevInstanceAndDeletesIt(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t)

	got := runWith(t, dev.env(), "tag", "create", "--name", name)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, name, nodeAt(t, mapping, "name").Value)
	assert.Equal(t, "admin", nodeAt(t, mapping, "owner", "login").Value)
	for _, set := range []string{"readSharingSettings", "updateSharingSettings", "tagSharingSettings"} {
		for _, of := range []string{"permittedGroups", "permittedUsers"} {
			assert.Empty(t, nodeAt(t, mapping, set, of).Content, "%s.%s is not empty", set, of)
		}
	}

	listed := requireTagListing(t, runWith(t, dev.env(), "tag", "list"))
	assert.True(t, slices.ContainsFunc(listed.Tags, func(record map[string]any) bool { return record["name"] == name }),
		"the new tag stands in no list")

	deleted := runWith(t, dev.env(), "tag", "delete", "--name", name)
	assert.Equal(t, outcome{stdout: "name: " + strconv.Quote(name) + "\nowner:\n  login: \"admin\"\n"}, deleted)

	gone := requireTagListing(t, runWith(t, dev.env(), "tag", "list"))
	assert.False(t, slices.ContainsFunc(gone.Tags, func(record map[string]any) bool { return record["name"] == name }),
		"the tag stands in the list after it was destroyed")

	again := runWith(t, dev.env(), "tag", "delete", "--name", name)
	assert.Equal(t, "unknown_name", requireFault(t, again).code)
	assert.Equal(t, []string{http.MethodPost, http.MethodGet, http.MethodGet, http.MethodDelete,
		http.MethodGet, http.MethodGet}, sentMethods(dev))
}

func TestTagCreateRefusesANameThePolygonAlreadyHasATagUnder(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t)

	made := runWith(t, dev.env(), "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })

	for _, tc := range []struct {
		written string
		said    string
	}{
		{written: name, said: "уже есть тег"},
		{written: strings.ToUpper(name), said: "Тег с таким именем уже существует"},
	} {
		got := runWith(t, dev.env(), "tag", "create", "--name", tc.written)

		found := requireFault(t, got)
		assert.Equal(t, "rejected", found.code)
		body, isText := detailNamed(t, found, "upstream_body").(string)
		require.True(t, isText)
		assert.Contains(t, body, "Tag.name-is-invalid")
		assert.Contains(t, body, tc.said)
	}
}

func TestTagCreateMakesATagOfANameAnotherUserAlreadyOwns(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	limited := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited}
	name := contractTagName(t)

	made := runWith(t, dev.env(), "tag", "create", "--name", name)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })

	got := runWith(t, limited, "tag", "create", "--name", name)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	t.Cleanup(func() { removeTag(t, limited, name, "dev.limited") })
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, name, nodeAt(t, mapping, "name").Value)
	assert.Equal(t, "dev.limited", nodeAt(t, mapping, "owner", "login").Value)
}

func TestTagCreateRefusesANameOfCharactersThePolygonWillNotKeep(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	withAComma := contractTagName(t) + ", x"

	got := runWith(t, dev.env(), "tag", "create", "--name", withAComma)

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	body, isText := detailNamed(t, found, "upstream_body").(string)
	require.True(t, isText)
	assert.Contains(t, body, "неподдерживаемых символов")
	assert.Contains(t, body, "&quot;", "the server HTML-escapes its message")

	listed := requireTagListing(t, runWith(t, dev.env(), "tag", "list"))
	listedWithAComma := slices.ContainsFunc(listed.Tags, func(record map[string]any) bool {
		return record["name"] == withAComma
	})
	assert.False(t, listedWithAComma, "a tag stands in the list although the server refused to make one")
}

func TestTagCreateThenListCountsBeyondTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	for _, suffix := range []string{" one", " two"} {
		name := contractTagName(t) + suffix
		made := runWith(t, dev.env(), "tag", "create", "--name", name)
		require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
		t.Cleanup(func() { removeTag(t, dev.env(), name, "admin") })
	}
	before := len(dev.requests())

	got := runWith(t, dev.env(), "tag", "list", "--limit", "1")

	printed := requireTagListing(t, got)
	assert.Equal(t, 1, printed.Returned)
	assert.True(t, printed.Truncated)
	assert.GreaterOrEqual(t, printed.Total, 2)
	assert.Equal(t, countingTags("1"), sentQueriesFrom(dev, before))
}

func sentQueriesFrom(u *upstream, at int) []url.Values {
	return u.sentQueries()[at:]
}

func TestTagCreateKeepsALineFeedInsideTheName(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	name := contractTagName(t) + "\nвторая строка"

	got := runWith(t, dev.env(), "tag", "create", "--name", name)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	printed := nodeAt(t, requireMapping(t, "stdout", got.stdout), "name")
	assert.Equal(t, name, printed.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, printed.Style, "a line feed keeps the name out of a plain scalar")

	deleted := runWith(t, dev.env(), "tag", "delete", "--name", name)
	require.Equal(t, 0, deleted.code, "stderr: %s", deleted.stderr)
	destroyed := requireMapping(t, "stdout", deleted.stdout)
	assert.Equal(t, name, nodeAt(t, destroyed, "name").Value)
	assert.Equal(t, "admin", nodeAt(t, destroyed, "owner", "login").Value)
}
