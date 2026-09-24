package cli_test

import (
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The group of the polygon the one user who is neither an administrator nor shut out of every project stands
// in, which is the group a scenario shares a tag with when it wants that token to hang it.
const polygonMembers = "Участники полигона"

// The third flag is refused an empty value where the other two are, and for the same reason: YouTrack keeps
// no group under an empty name, so nothing is sent.
func TestTagCreateRefusesAGroupOfNoNameForTagging(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--taggable-by", "")

	want := refusal{code: "bad_usage"}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// The names of the groups become the third set of the body, which is the one YouTrack reads the right to
// hang a tag out of. A group that may hang the tag and one that is shown it are written apart, since the two
// rights are apart on the server.
func TestTagCreateWritesTheGroupsThatMayHangTheTag(t *testing.T) {
	t.Parallel()
	const name = "карта"
	server := sharingATag(t, groupsOfTheInstance(), answer(http.StatusOK, taggableTag(name,
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}, {id: "101-0", name: `ООО "РОМАШКА", Москва`}})))

	got := runWith(t, server.env(), "tag", "create", "--name", name,
		"--visible-for", "development team",
		"--taggable-by", "DEVELOPMENT TEAM",
		"--taggable-by", `ООО "РОМАШКА", Москва`,
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

// A tag nobody may hang is the tag the flag was never written for: the set stays out of the body, and
// YouTrack is left to settle it, exactly as the other two are.
func TestTagCreateWritesNoTagSharingWhereNobodyMayHangIt(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), answer(http.StatusOK, sharedTag("карта",
		[]sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}}, nil)))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта", "--visible-for", "DEVELOPMENT Team")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, []string{"name", "readSharingSettings"}, slices.Sorted(maps.Keys(sentBody(t, server))))
}

// The names of all three flags are read before any of them is refused, so a call that names one group
// nobody has under either right is answered once.
func TestTagCreateRefusesEveryGroupOfTheThreeFlagsAtOnce(t *testing.T) {
	t.Parallel()
	server := sharingATag(t, groupsOfTheInstance(), noCreation(t))

	got := runWith(t, server.env(), "tag", "create", "--name", "карта",
		"--visible-for", "Нет", "--taggable-by", "Тоже")

	want := refusal{
		code: "unknown_name",
		details: []detail{
			{"request", groupsRequest(server.url)},
			{"unknown", []any{
				[]detail{{"group", "Нет"}, {"nearest", everyGroupName()}},
				[]detail{{"group", "Тоже"}, {"nearest", everyGroupName()}},
			}},
		},
	}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// The third set is held against what went out the way the other two are: a 200 says the server took the
// body, not that the groups it kept are the groups that were written, and a tag anyone may hang where the call
// named one group is a refusal over a tag that by then exists.
func TestTagCreateHoldsTheTagSharingItWroteAgainstTheOneThatCameBack(t *testing.T) {
	t.Parallel()
	const name = "карта"
	tests := []struct {
		name     string
		kept     []sharedGroup
		mismatch []any
	}{
		{
			name: "the same two the other way round",
			kept: []sharedGroup{{id: "101-0", name: `ООО "РОМАШКА", Москва`}, {id: "6-1", name: "DEVELOPMENT Team"}},
		},
		{
			name: "a group nobody wrote left in the set",
			kept: []sharedGroup{{id: "6-1", name: "DEVELOPMENT Team"}},
			mismatch: []any{[]detail{
				{"field", "tagSharingSettings.permittedGroups"},
				{"written", []any{"6-1", "101-0"}},
				{"arrived", []any{"6-1"}},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := sharingATag(t, groupsOfTheInstance(),
				answer(http.StatusOK, taggableTag(name, nil, tc.kept)))

			got := runWith(t, server.env(), "tag", "create", "--name", name,
				"--taggable-by", "DEVELOPMENT Team", "--taggable-by", `ООО "РОМАШКА", Москва`)

			if tc.mismatch == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				return
			}
			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
			assert.Empty(t, got.stdout)
		})
	}
}

// taggableTag is the answer of the server to a creation that named who may hang the tag: the third set stands
// beside the two a creation prints by default, and it is asked for only because it was written.
func taggableTag(name string, read, hang []sharedGroup) string {
	return `{"$type":"Tag","name":` + asJSON(name) + `,"owner":{"$type":"User","login":"admin"},` +
		`"readSharingSettings":` + sharingOf(read) + `,"updateSharingSettings":` + sharingOf(nil) +
		`,"tagSharingSettings":` + taggingOf(hang) + `}`
}

// The right to hang a tag against the polygon, with the one token that is neither an
// administrator nor shut out of the projects: a tag shared with the group that token stands in is one it is
// shown either way, and whether it may hang the tag turns on --taggable-by and on nothing else. The same
// listing answers what a token is shown of a tag it does not own — the one place on the polygon where that can
// be read at all.
func TestTagAddHangsTheTagAMemberOfThePolygonMayHang(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	member := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}
	issue := taggedIssue(t, dev)
	hangs := contractTagName(t) + " hangs"
	shown := contractTagName(t) + " shown"

	made := runWith(t, dev.env(), "tag", "create", "--name", hangs,
		"--visible-for", polygonMembers, "--taggable-by", polygonMembers)
	require.Equal(t, 0, made.code, "stderr: %s", made.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), hangs, "admin") })
	readOnly := runWith(t, dev.env(), "tag", "create", "--name", shown, "--visible-for", polygonMembers)
	require.Equal(t, 0, readOnly.code, "stderr: %s", readOnly.stderr)
	t.Cleanup(func() { removeTag(t, dev.env(), shown, "admin") })

	listed := requireTagListing(t, runWith(t, member, "tag", "list"))
	at := slices.IndexFunc(listed.Tags, func(record map[string]any) bool { return record["name"] == hangs })
	require.GreaterOrEqual(t, at, 0, "the shared tag stands in no list of the token it was shared with")
	assert.Equal(t, "admin", ownerOf(t, listed.Tags[at]))
	// The one measurement of what a token is shown of a tag it does not own: a key the server withholds there
	// is one the default of the list cannot carry.
	assert.Equal(t, []string{"name", "owner", "readSharingSettings"}, slices.Sorted(maps.Keys(listed.Tags[at])))

	hung := runWith(t, member, "tag", "add", issue, "--name", hangs)
	require.Equal(t, 0, hung.code, "stderr: %s", hung.stderr)
	assert.Equal(t, hangs, nodeAt(t, requireMapping(t, "stdout", hung.stdout), "added", "name").Value)
	assert.Equal(t, []string{hangs}, tagsOfTheIssue(t, dev, issue))

	refused := runWith(t, member, "tag", "add", issue, "--name", shown)
	found := requireRefusal(t, refused)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, []string{hangs}, tagsOfTheIssue(t, dev, issue), "a tag the token may not hang was hung")
}

// The member of the polygon may not read the groups at all — group-read is a right of the
// administrator — so the third flag is out of that token's reach exactly as the other two are, and no tag is
// made.
func TestTagCreateRefusesTheMemberTheGroupsOfThePolygon(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	member := []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member}

	got := runWith(t, member, "tag", "create", "--name", contractTagName(t), "--taggable-by", polygonMembers)

	found := requireRefusal(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	assert.Equal(t, []string{"/api/groups"}, dev.sentPaths())
}
