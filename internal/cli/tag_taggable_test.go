package cli_test

import (
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const devInstanceMembers = "Участники полигона"

func TestTagCreateRefusesAGroupOfNoNameForTagging(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--taggable-by", "")

	want := faultDocument{code: "bad_usage"}
	assert.Equal(t, want, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestTagCreateWritesTheGroupsThatMayAddTheTag(t *testing.T) {
	t.Parallel()
	const name = "карта"
	server := sharingATag(t, groupsOfTheInstance(), respondWith(http.StatusOK, taggableTag(name,
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}, {id: "101-0", name: groupWithAComma}})))

	got := runWith(t, server.env(), "tag", "create", "--name", name,
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
			"tagSharingSettings(permittedGroups(name,id),permittedUsers(login))"}, server.sentFields())
}

func TestTagCreateWritesNoTagSharingWhereNobodyMayAddIt(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), respondWith(http.StatusOK, sharedTag("карта",
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}}, nil)))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "DEVELOPMENT Team")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"name", "readSharingSettings"}, slices.Sorted(maps.Keys(sentBody(t, server))))
}

func TestTagCreateRefusesEveryGroupOfTheThreeFlagsAtOnce(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Нет", "--taggable-by", "Тоже")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.url)},
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
				respondWith(http.StatusOK, taggableTag(name, nil, tc.kept)))

			got := runWith(t, server.env(), "tag", "create", "--name", name,
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

func TestTagAddHangsTheTagAMemberOfThePolygonMayHang(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	member := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}
	issue := issueToTag(t, dev)
	hangs := contractTagName(t) + " hangs"
	shown := contractTagName(t) + " shown"

	made := runWith(t, dev.env(), "tag", "create", "--name", hangs,
		"--visible-for", devInstanceMembers, "--taggable-by", devInstanceMembers)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), hangs, "admin") })
	readOnly := runWith(t, dev.env(), "tag", "create", "--name", shown, "--visible-for", devInstanceMembers)
	require.Equal(t, 0, readOnly.code, "stderr: %s", readOnly.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), shown, "admin") })

	listed := requireTagListing(t, runWith(t, member, "tag", "list"))
	at := slices.IndexFunc(listed.Tags, func(record map[string]any) bool { return record["name"] == hangs })
	require.GreaterOrEqual(t, at, 0, "the shared tag stands in no list of the token it was shared with")
	assert.Equal(t, "admin", ownerOf(t, listed.Tags[at]))
	assert.Equal(t, []string{"name", "owner", "readSharingSettings"}, slices.Sorted(maps.Keys(listed.Tags[at])))

	hung := runWith(t, member, "tag", "add", issue, "--name", hangs)
	require.Equal(t, 0, hung.code, "stderr: %s", hung.stderr)
	assert.Equal(t, hangs, nodeAt(t, requireMapping(t, "stdout", hung.stdout), "added", "name").Value)
	assert.Equal(t, []string{hangs}, tagsOfTheIssue(t, dev, issue))

	refused := runWith(t, member, "tag", "add", issue, "--name", shown)
	found := requireFault(t, refused)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, []string{hangs}, tagsOfTheIssue(t, dev, issue), "a tag the token may not hang was hung")
}

func TestTagCreateRefusesTheMemberTheGroupsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	member := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}

	got := runWith(t, member, "tag", "create", "--name", contractTagName(t), "--taggable-by", devInstanceMembers)

	found := requireFault(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	assert.Equal(t, []string{"/api/groups"}, dev.sentPaths())
}
