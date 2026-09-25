package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestTagCreateRefusesAGroupOfNoNameForTagging(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "tag", "create", "--name", "карта", "--taggable-by", "")

	want := faultDocument{code: "bad_usage"}
	assert.Equal(t, want, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestTagCreateWritesTheGroupsThatMayAddTheTag(t *testing.T) {
	t.Parallel()
	const name = "карта"
	server := sharingATag(t, groupsOfTheInstance(), fake.JSON(http.StatusOK, taggableTag(name,
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}, {id: "101-0", name: groupWithAComma}})))

	got := runWith(t, server.Env(), "tag", "create", "--name", name,
		"--visible-for", "development team",
		"--taggable-by", "DEVELOPMENT TEAM",
		"--taggable-by", groupWithAComma,
		"--taggable-by", "DEVELOPMENT Team")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	assert.Equal(t, map[string]any{
		"name":                name,
		"readSharingSettings": map[string]any{"permittedGroups": []any{map[string]any{"id": "6-1"}}},
		"tagSharingSettings": map[string]any{"permittedGroups": []any{
			map[string]any{"id": "6-1"},
			map[string]any{"id": "101-0"},
		}},
	}, sentBody(t, server))
	assert.Equal(t, []string{shownGroupFields,
		"name,owner(login),readSharingSettings(permittedGroups(name,id),permittedUsers(login))," +
			"updateSharingSettings(permittedGroups(name),permittedUsers(login))," +
			"tagSharingSettings(permittedGroups(name,id),permittedUsers(login))"}, server.Fields())
}

func TestTagCreateRefusesEveryGroupOfTheThreeFlagsAtOnce(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), noCreation(t))

	got := runWith(t, server.Env(), "tag", "create", "--name", "карта",
		"--visible-for", "Нет", "--taggable-by", "Тоже")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.URL)},
			{"unknown", []any{
				[]detail{{"group", "Нет"}, {"nearest", everyGroupName()}},
				[]detail{{"group", "Тоже"}, {"nearest", everyGroupName()}},
			}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestTagCreateChecksTheTagSharingItWroteAgainstTheOneThatCameBack(t *testing.T) {
	t.Parallel()
	const name = "карта"
	tests := []struct {
		name     string
		kept     []sharedGroup
		mismatch []any
	}{
		{
			name: "the same two the other way round",
			kept: []sharedGroup{{id: "101-0", name: groupWithAComma}, {id: "6-1", name: "DEVELOPMENT Team"}},
		},
		{
			name: "a group nobody wrote left in the set",
			kept: []sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
			mismatch: []any{[]detail{
				{"field", "tagSharingSettings.permittedGroups"},
				{"expected", []any{"6-1", "101-0"}},
				{"actual", []any{"6-1"}},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := sharingATag(t, groupsOfTheInstance(),
				fake.JSON(http.StatusOK, taggableTag(name, nil, tc.kept)))

			got := runWith(t, server.Env(), "tag", "create", "--name", name,
				"--taggable-by", "DEVELOPMENT Team", "--taggable-by", groupWithAComma)

			if tc.mismatch == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				return
			}
			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
			assert.Empty(t, got.stdout)
		})
	}
}

func taggableTag(name string, read, hang []sharedGroup) string {
	return `{"$type":"Tag","name":` + asJSON(name) + `,"owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":` + sharingOf(read) + `,"updateSharingSettings":` + sharingOf(nil) +
		`,"tagSharingSettings":` + taggableBy(hang) + `}`
}
