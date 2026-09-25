package youtrack_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const tagCatalogueTarget = "/api/tags?fields=id,name,owner(login)&$top=-1"

func tagText(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func tagOf(id, name, owner string) string {
	return `{"$type":"Tag","id":` + tagText(id) + `,"name":` + tagText(name) +
		`,"owner":{"$type":"User","login":` + tagText(owner) + `}}`
}

func tagCatalogue(tags ...string) string {
	return "[" + strings.Join(tags, ",") + "]"
}

// Out of order on purpose: a candidate list comes out sorted only if ytrack sorts it.
func tagsShown() string {
	return tagCatalogue(
		tagOf("10-1", "Early", "first"),
		tagOf("10-2", "mixed", "first"),
		tagOf("10-3", "Mixed", "second"),
		tagOf("10-4", "Mixed", "first"),
		tagOf("10-5", "10-1", "first"),
	)
}

func tagOwner(schema, readable string) string {
	return `{"$type":` + tagText(schema) + `,"idReadable":` + tagText(readable) + `}`
}

func tagNode(name, owner string) *render.Node {
	return render.NewMap(
		render.Pair{Key: "name", Value: render.NewString(name)},
		render.Pair{Key: "owner", Value: render.NewMap(render.Pair{Key: "login", Value: render.NewString(owner)})})
}

func tagCandidate(name, owner string) *render.Node {
	return render.NewMap(
		render.Pair{Key: "name", Value: render.NewString(name)},
		render.Pair{Key: "owner", Value: render.NewString(owner)})
}

func tagServer(t *testing.T, routes map[string]http.HandlerFunc) *fake.Server {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, handler := range routes {
		mux.Handle(pattern, handler)
	}
	return fake.Serve(t, mux.ServeHTTP)
}

func tagBodySent(t *testing.T, server *fake.Server) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(server.Last(t).Body), &body))
	return body
}

func tagCreationEchoed(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	fake.JSON(http.StatusOK, `{"$type":"Tag",`+strings.TrimPrefix(string(body), "{"))(w, r)
}

func TestTagRefusesACallBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func() (youtrack.Call, *diag.Fault)
	}{
		{
			name: "a creation with an empty name",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.CreateTag("", youtrack.TagSharing{}, nil) },
		},
		{
			name: "a creation with a name of no UTF-8",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.CreateTag("\xff", youtrack.TagSharing{}, nil) },
		},
		{
			name: "a deletion with an empty name",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.DeleteTag("", nil) },
		},
		{
			name: "a deletion with a name that begins with a space",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.DeleteTag(" Early", nil) },
		},
		{
			name: "a tagging with an empty name",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.AddTag("DEV-7", "", nil) },
		},
		{
			name: "a removal with an empty name",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.RemoveTag("DEV-7", "", nil) },
		},
		{
			name: "a deletion owned by an empty login",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.DeleteTag("Early", new("")) },
		},
		{
			name: "a tagging owned by an empty login",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.AddTag("DEV-7", "Early", new("")) },
		},
		{
			name: "a removal owned by an empty login",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.RemoveTag("DEV-7", "Early", new("")) },
		},
		{
			name: "a creation shared with a group of no name to see it",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateTag("Early", youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{""}}, nil)
			},
		},
		{
			name: "a creation shared with a group of no name to update it",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateTag("Early", youtrack.TagSharing{UpdatableBy: youtrack.UpdatableBy{""}}, nil)
			},
		},
		{
			name: "a creation shared with a group of no name to tag with it",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateTag("Early", youtrack.TagSharing{TaggableBy: youtrack.TaggableBy{""}}, nil)
			},
		},
		{
			name: "a creation shared with a group and a group of no name after it",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.CreateTag("Early", youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First", ""}}, nil)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := tc.call()
			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCreateTagRefusesANameTheServerWouldCut(t *testing.T) {
	t.Parallel()
	type edge struct {
		name    string
		written string
	}
	var tests []edge
	for _, r := range []rune{' ', '\t', '\n', '\r', '\v', '\f', 0x1C, 0x1D, 0x1E, 0x1F, 0xA0, 0x2028, 0x2029, 0x3000} {
		tests = append(tests,
			edge{name: fmt.Sprintf("U+%04X at the beginning", r), written: string(r) + "Early"},
			edge{name: fmt.Sprintf("U+%04X at the end", r), written: "Early" + string(r)})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.CreateTag(tc.written, youtrack.TagSharing{}, nil)
			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestCreateTagSendsTheNameAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
	}{
		{name: "brackets and a space", written: "[bug] fix login"},
		{name: "a tab inside", written: "two\twords"},
		{name: "a line feed inside", written: "two\nlines"},
		{name: "a carriage return inside", written: "two\rlines"},
		{name: "a line separator inside", written: "two\u2028lines"},
		{name: "a start of heading at the edge", written: "\x01Early"},
		{name: "a next line at the edge", written: "Early\u0085"},
		{name: "a zero width space at the edge", written: "\u200bEarly"},
		{name: "a byte order mark at the edge", written: "Early\ufeff"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Tag","name":`+tagText(tc.written)+`}`))
			call, fault := youtrack.CreateTag(tc.written, youtrack.TagSharing{}, new("name"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, map[string]any{"name": tc.written}, tagBodySent(t, server))
		})
	}
}

func TestCreateTagAsksForTheNameItChecks(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, tagOf("10-1", "Early", "first")))
	call, fault := youtrack.CreateTag("Early", youtrack.TagSharing{}, new("owner(login)"))
	require.Nil(t, fault)

	node, fault := call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(render.Pair{Key: "owner", Value: render.NewMap(
		render.Pair{Key: "login", Value: render.NewString("first")})}), node)
	assert.Equal(t, []string{"/api/tags?fields=owner(login),name"}, server.Targets())
}

func TestCreateTagRefusesANameTheServerKeptAsAnother(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		kept    string
		actual  *render.Node
	}{
		{
			name:    "a rune cut off the end after all",
			written: "Early\u0085",
			kept:    `{"$type":"Tag","name":"Early"}`,
			actual:  render.NewString("Early"),
		},
		{
			name:    "another letter case",
			written: "Early",
			kept:    `{"$type":"Tag","name":"early"}`,
			actual:  render.NewString("early"),
		},
		{
			name:    "no name at all",
			written: "Early",
			kept:    `{"$type":"Tag","name":null}`,
			actual:  render.NewNull(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.kept))
			call, fault := youtrack.CreateTag(tc.written, youtrack.TagSharing{}, new("name"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
				{Key: "request", Value: render.NewString("POST " + server.URL + "/api/tags?fields=name")},
				{Key: "tag", Value: render.NewString(tc.written)},
				{Key: "mismatch", Value: render.NewList(render.NewMap(
					render.Pair{Key: "field", Value: render.NewString("name")},
					render.Pair{Key: "expected", Value: render.NewString(tc.written)},
					render.Pair{Key: "actual", Value: tc.actual}))},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

// Out of order on purpose, as tagsShown is.
func tagGroups() string {
	return `[{"$type":"UserGroup","id":"6-1","name":"First"},{"$type":"UserGroup","id":"6-2","name":"Second"},` +
		`{"$type":"UserGroup","id":"6-4","name":"team"},{"$type":"UserGroup","id":"6-3","name":"Team"}]`
}

func tagGroupsRequest(server *fake.Server) string {
	return "GET " + server.URL + "/api/groups?fields=id,name&$top=-1"
}

func tagSharedWith(ids ...string) map[string]any {
	groups := make([]any, 0, len(ids))
	for _, id := range ids {
		groups = append(groups, map[string]any{"id": id})
	}
	return map[string]any{"permittedGroups": groups}
}

func TestCreateTagWritesEachSetOfGroupsItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		shared youtrack.TagSharing
		body   map[string]any
	}{
		{
			name:   "the groups that see it",
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First", "Second"}},
			body:   map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-1", "6-2")},
		},
		{
			name:   "the groups that update it",
			shared: youtrack.TagSharing{UpdatableBy: youtrack.UpdatableBy{"Second"}},
			body:   map[string]any{"name": "Early", "updateSharingSettings": tagSharedWith("6-2")},
		},
		{
			name:   "the groups that tag with it",
			shared: youtrack.TagSharing{TaggableBy: youtrack.TaggableBy{"First"}},
			body:   map[string]any{"name": "Early", "tagSharingSettings": tagSharedWith("6-1")},
		},
		{
			name: "each set of its own",
			shared: youtrack.TagSharing{
				VisibleFor:  youtrack.VisibleFor{"First"},
				UpdatableBy: youtrack.UpdatableBy{"Second"},
				TaggableBy:  youtrack.TaggableBy{"First", "Second"},
			},
			body: map[string]any{
				"name":                  "Early",
				"readSharingSettings":   tagSharedWith("6-1"),
				"updateSharingSettings": tagSharedWith("6-2"),
				"tagSharingSettings":    tagSharedWith("6-1", "6-2"),
			},
		},
		{
			name:   "a group named in another letter case",
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"FIRST"}},
			body:   map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-1")},
		},
		{
			name:   "one group named three times",
			shared: youtrack.TagSharing{TaggableBy: youtrack.TaggableBy{"first", "First", "FIRST"}},
			body:   map[string]any{"name": "Early", "tagSharingSettings": tagSharedWith("6-1")},
		},
		{
			name:   "the upper case one of two groups apart by letter case",
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"Team"}},
			body:   map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-3")},
		},
		{
			name:   "the lower case one of the two",
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"team"}},
			body:   map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-4")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tagServer(t, map[string]http.HandlerFunc{
				"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
				"POST /api/tags":  tagCreationEchoed,
			})
			call, fault := youtrack.CreateTag("Early", tc.shared, new("name"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.body, tagBodySent(t, server))
			assert.Equal(t, []string{"/api/groups", "/api/tags"}, server.Paths())
		})
	}
}

func TestCreateTagAsksForTheGroupsOfEachSetItWrote(t *testing.T) {
	t.Parallel()
	server := tagServer(t, map[string]http.HandlerFunc{
		"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
		"POST /api/tags":  tagCreationEchoed,
	})
	shared := youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First"}, TaggableBy: youtrack.TaggableBy{"Second"}}
	call, fault := youtrack.CreateTag("Early", shared, new("name"))
	require.Nil(t, fault)

	_, fault = call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, []string{
		"/api/groups?fields=id,name&$top=-1",
		"/api/tags?fields=name,readSharingSettings(permittedGroups(id)),tagSharingSettings(permittedGroups(id))",
	}, server.Targets())
}

func TestCreateTagRefusesGroupNamesItCannotResolve(t *testing.T) {
	t.Parallel()
	every := texts("First", "Second", "Team", "team")
	tests := []struct {
		name    string
		shared  youtrack.TagSharing
		details []render.Pair
	}{
		{
			name:   "a name near a group",
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"Frist"}},
			details: []render.Pair{{Key: "unknown", Value: render.NewList(render.NewMap(
				render.Pair{Key: "group", Value: render.NewString("Frist")},
				render.Pair{Key: "nearest", Value: texts("First")}))}},
		},
		{
			name: "a name near no group in each set",
			shared: youtrack.TagSharing{
				VisibleFor:  youtrack.VisibleFor{"Nobody"},
				UpdatableBy: youtrack.UpdatableBy{"Nil"},
				TaggableBy:  youtrack.TaggableBy{"None"},
			},
			details: []render.Pair{{Key: "unknown", Value: render.NewList(
				render.NewMap(render.Pair{Key: "group", Value: render.NewString("Nobody")}, render.Pair{Key: "nearest", Value: every}),
				render.NewMap(render.Pair{Key: "group", Value: render.NewString("Nil")}, render.Pair{Key: "nearest", Value: every}),
				render.NewMap(render.Pair{Key: "group", Value: render.NewString("None")}, render.Pair{Key: "nearest", Value: every}))}},
		},
		{
			name:   "a name two groups answer to by letter case",
			shared: youtrack.TagSharing{UpdatableBy: youtrack.UpdatableBy{"TEAM"}},
			details: []render.Pair{{Key: "ambiguous", Value: render.NewList(render.NewMap(
				render.Pair{Key: "group", Value: render.NewString("TEAM")},
				render.Pair{Key: "candidates", Value: texts("Team", "team")}))}},
		},
		{
			name: "an unknown name and an ambiguous one",
			shared: youtrack.TagSharing{
				VisibleFor: youtrack.VisibleFor{"Nobody"},
				TaggableBy: youtrack.TaggableBy{"TEAM"},
			},
			details: []render.Pair{
				{Key: "unknown", Value: render.NewList(render.NewMap(
					render.Pair{Key: "group", Value: render.NewString("Nobody")},
					render.Pair{Key: "nearest", Value: every}))},
				{Key: "ambiguous", Value: render.NewList(render.NewMap(
					render.Pair{Key: "group", Value: render.NewString("TEAM")},
					render.Pair{Key: "candidates", Value: texts("Team", "team")}))},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tagGroups()))
			call, fault := youtrack.CreateTag("Early", tc.shared, nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{
				Code:    diag.UnknownName,
				Details: append([]render.Pair{{Key: "request", Value: render.NewString(tagGroupsRequest(server))}}, tc.details...),
			}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{"/api/groups"}, server.Paths())
		})
	}
}

func TestCreateTagRefusesGroupsItCannotShareTheTagWith(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		groups string
		shared youtrack.TagSharing
	}{
		{
			name:   "an id with no dash",
			groups: `[{"$type":"UserGroup","id":"6","name":"First"}]`,
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First"}},
		},
		{
			name:   "an id of two dots",
			groups: `[{"$type":"UserGroup","id":"..","name":"First"}]`,
			shared: youtrack.TagSharing{UpdatableBy: youtrack.UpdatableBy{"First"}},
		},
		{
			name:   "an id with a letter after the dash",
			groups: `[{"$type":"UserGroup","id":"6-x","name":"First"}]`,
			shared: youtrack.TagSharing{TaggableBy: youtrack.TaggableBy{"First"}},
		},
		{
			name:   "a name that is no text",
			groups: `[{"$type":"UserGroup","id":"6-1","name":5}]`,
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First"}},
		},
		{
			name:   "an id that is no text",
			groups: `[{"$type":"UserGroup","id":7,"name":"First"}]`,
			shared: youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First"}},
		},
		{
			name: "a broken id beside names that resolve to nothing",
			groups: `[{"$type":"UserGroup","id":"6","name":"First"},{"$type":"UserGroup","id":"6-3","name":"Team"},` +
				`{"$type":"UserGroup","id":"6-4","name":"team"}]`,
			shared: youtrack.TagSharing{
				VisibleFor:  youtrack.VisibleFor{"First"},
				UpdatableBy: youtrack.UpdatableBy{"Nobody"},
				TaggableBy:  youtrack.TaggableBy{"TEAM"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.groups))
			call, fault := youtrack.CreateTag("Early", tc.shared, nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				{Key: "request", Value: render.NewString(tagGroupsRequest(server))},
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(tc.groups)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{"/api/groups"}, server.Paths())
		})
	}
}

func tagKeptWith(set, groups string) string {
	return `{"$type":"Tag","name":"Early","` + set + `":{"$type":"WatchFolderSharingSettings","permittedGroups":` + groups + `}}`
}

func TestCreateTagRefusesASetOfGroupsThatCameBackAsAnother(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		shared   youtrack.TagSharing
		kept     string
		fields   string
		field    string
		expected *render.Node
		actual   *render.Node
	}{
		{
			name:     "one of the two that see it dropped",
			shared:   youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First", "Second"}},
			kept:     tagKeptWith("readSharingSettings", `[{"id":"6-1"}]`),
			fields:   "name,readSharingSettings(permittedGroups(id))",
			field:    "readSharingSettings.permittedGroups",
			expected: texts("6-1", "6-2"),
			actual:   texts("6-1"),
		},
		{
			name:     "a group nobody wrote left among those that update it",
			shared:   youtrack.TagSharing{UpdatableBy: youtrack.UpdatableBy{"First"}},
			kept:     tagKeptWith("updateSharingSettings", `[{"id":"6-1"},{"id":"6-2"}]`),
			fields:   "name,updateSharingSettings(permittedGroups(id))",
			field:    "updateSharingSettings.permittedGroups",
			expected: texts("6-1"),
			actual:   texts("6-1", "6-2"),
		},
		{
			name:     "another group among those that tag with it",
			shared:   youtrack.TagSharing{TaggableBy: youtrack.TaggableBy{"First"}},
			kept:     tagKeptWith("tagSharingSettings", `[{"id":"6-2"}]`),
			fields:   "name,tagSharingSettings(permittedGroups(id))",
			field:    "tagSharingSettings.permittedGroups",
			expected: texts("6-1"),
			actual:   texts("6-2"),
		},
		{
			name:     "no list of groups at all",
			shared:   youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First"}},
			kept:     tagKeptWith("readSharingSettings", `null`),
			fields:   "name,readSharingSettings(permittedGroups(id))",
			field:    "readSharingSettings.permittedGroups",
			expected: texts("6-1"),
			actual:   render.NewNull(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tagServer(t, map[string]http.HandlerFunc{
				"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
				"POST /api/tags":  fake.JSON(http.StatusOK, tc.kept),
			})
			call, fault := youtrack.CreateTag("Early", tc.shared, new("name"))
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
				{Key: "request", Value: render.NewString("POST " + server.URL + "/api/tags?fields=" + tc.fields)},
				{Key: "tag", Value: render.NewString("Early")},
				{Key: "mismatch", Value: render.NewList(render.NewMap(
					render.Pair{Key: "field", Value: render.NewString(tc.field)},
					render.Pair{Key: "expected", Value: tc.expected},
					render.Pair{Key: "actual", Value: tc.actual}))},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestCreateTagTakesASetOfGroupsThatCameBackInAnotherOrder(t *testing.T) {
	t.Parallel()
	server := tagServer(t, map[string]http.HandlerFunc{
		"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
		"POST /api/tags":  fake.JSON(http.StatusOK, tagKeptWith("readSharingSettings", `[{"id":"6-2"},{"id":"6-1"}]`)),
	})
	call, fault := youtrack.CreateTag("Early", youtrack.TagSharing{VisibleFor: youtrack.VisibleFor{"First", "Second"}}, new("name"))
	require.Nil(t, fault)

	node, fault := call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(render.Pair{Key: "name", Value: render.NewString("Early")}), node)
}

func TestDeleteTagResolvesTheNameAgainstTheTagsItIsShown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		ownedBy *string
		id      string
	}{
		{name: "another letter case", written: "EARLY", id: "10-1"},
		{name: "the exact spelling among names apart by letter case", written: "mixed", id: "10-2"},
		{name: "a name shaped like an internal id", written: "10-1", id: "10-5"},
		{name: "one name of two owners, of one of them", written: "Mixed", ownedBy: new("second"), id: "10-3"},
		{name: "the same name of the other owner", written: "Mixed", ownedBy: new("first"), id: "10-4"},
		{name: "a login in another letter case", written: "Mixed", ownedBy: new("SECOND"), id: "10-3"},
		{name: "no exact spelling, of the owner of one of them", written: "MIXED", ownedBy: new("second"), id: "10-3"},
		{name: "the exact spelling among the tags of one owner", written: "mixed", ownedBy: new("first"), id: "10-2"},
		{name: "the one tag of that name, of its owner", written: "early", ownedBy: new("first"), id: "10-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tagServer(t, map[string]http.HandlerFunc{
				"GET /api/tags":         fake.JSON(http.StatusOK, tagsShown()),
				"DELETE /api/tags/{id}": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
			})
			call, fault := youtrack.DeleteTag(tc.written, tc.ownedBy)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, []string{"/api/tags", "/api/tags/" + tc.id}, server.Paths())
		})
	}
}

func TestDeleteTagReadsEveryTagOnceAndPrintsTheOneItDeleted(t *testing.T) {
	t.Parallel()
	server := tagServer(t, map[string]http.HandlerFunc{
		"GET /api/tags":         fake.JSON(http.StatusOK, tagsShown()),
		"DELETE /api/tags/{id}": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	})
	call, fault := youtrack.DeleteTag("early", nil)
	require.Nil(t, fault)

	node, fault := call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, tagNode("Early", "first"), node)
	assert.Equal(t, []string{tagCatalogueTarget, "/api/tags/10-1?"}, server.Targets())
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestDeleteTagRefusesANameThatNamesNoOneTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		ownedBy *string
		detail  render.Pair
	}{
		{
			name:    "a name near a tag",
			written: "Erly",
			detail: render.Pair{Key: "unknown", Value: render.NewList(render.NewMap(
				render.Pair{Key: "tag", Value: render.NewString("Erly")},
				render.Pair{Key: "nearest", Value: texts("Early")}))},
		},
		{
			name:    "a name near no tag",
			written: "zzzzzz",
			detail: render.Pair{Key: "unknown", Value: render.NewList(render.NewMap(
				render.Pair{Key: "tag", Value: render.NewString("zzzzzz")},
				render.Pair{Key: "nearest", Value: texts("10-1", "Early", "Mixed", "Mixed", "mixed")}))},
		},
		{
			name:    "a name of several tags and no exact spelling",
			written: "MIXED",
			detail: render.Pair{Key: "ambiguous", Value: render.NewList(render.NewMap(
				render.Pair{Key: "tag", Value: render.NewString("MIXED")},
				render.Pair{Key: "candidates", Value: render.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("Mixed", "second"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "an exact spelling two owners hold",
			written: "Mixed",
			detail: render.Pair{Key: "ambiguous", Value: render.NewList(render.NewMap(
				render.Pair{Key: "tag", Value: render.NewString("Mixed")},
				render.Pair{Key: "candidates", Value: render.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("Mixed", "second"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "no exact spelling among the tags of one owner",
			written: "MIXED",
			ownedBy: new("first"),
			detail: render.Pair{Key: "ambiguous", Value: render.NewList(render.NewMap(
				render.Pair{Key: "tag", Value: render.NewString("MIXED")},
				render.Pair{Key: "candidates", Value: render.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "a login no tag of that name belongs to",
			written: "Mixed",
			ownedBy: new("third"),
			detail: render.Pair{Key: "unknown", Value: render.NewList(render.NewMap(
				render.Pair{Key: "tag", Value: render.NewString("Mixed")},
				render.Pair{Key: "owned_by", Value: render.NewString("third")},
				render.Pair{Key: "candidates", Value: render.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("Mixed", "second"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "the one tag of that name belonging to someone else",
			written: "early",
			ownedBy: new("second"),
			detail: render.Pair{Key: "unknown", Value: render.NewList(render.NewMap(
				render.Pair{Key: "tag", Value: render.NewString("early")},
				render.Pair{Key: "owned_by", Value: render.NewString("second")},
				render.Pair{Key: "candidates", Value: render.NewList(tagCandidate("Early", "first"))}))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tagsShown()))
			call, fault := youtrack.DeleteTag(tc.written, tc.ownedBy)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UnknownName, Details: []render.Pair{
				{Key: "request", Value: render.NewString("GET " + server.URL + tagCatalogueTarget)},
				tc.detail,
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{"/api/tags"}, server.Paths())
		})
	}
}

func TestDeleteTagRefusesTagsItCannotAddressTheDeletionBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  string
	}{
		{name: "a name that is no text", tag: `{"$type":"Tag","id":"10-1","name":5,"owner":{"$type":"User","login":"first"}}`},
		{name: "an owner whose login is null", tag: `{"$type":"Tag","id":"10-1","name":"Early","owner":{"$type":"User","login":null}}`},
		{name: "an id that is no text", tag: `{"$type":"Tag","id":101,"name":"Early","owner":{"$type":"User","login":"first"}}`},
		{name: "an id that is null", tag: `{"$type":"Tag","id":null,"name":"Early","owner":{"$type":"User","login":"first"}}`},
		{name: "an id of two dots", tag: tagOf("..", "Early", "first")},
		{name: "an id with a letter after the dash", tag: tagOf("10-x", "Early", "first")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			catalogue := tagCatalogue(tc.tag)
			server := fake.Serve(t, fake.JSON(http.StatusOK, catalogue))
			call, fault := youtrack.DeleteTag("Early", nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				{Key: "request", Value: render.NewString("GET " + server.URL + tagCatalogueTarget)},
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(catalogue)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{"/api/tags"}, server.Paths())
		})
	}
}

func TestAddAndRemoveTagRefuseAnOwnerTheReadNamedByAnIDOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		call  func() (youtrack.Call, *diag.Fault)
		owner string
		read  string
	}{
		{
			name:  "a tagging of an issue answered with two dots",
			call:  func() (youtrack.Call, *diag.Fault) { return youtrack.AddTag("DEV-7", "Early", nil) },
			owner: tagOwner("Issue", ".."),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a tagging of an issue answered with the id of an article",
			call:  func() (youtrack.Call, *diag.Fault) { return youtrack.AddTag("DEV-7", "Early", nil) },
			owner: tagOwner("Issue", "DEV-A-7"),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a tagging of an article answered with the id of an issue",
			call:  func() (youtrack.Call, *diag.Fault) { return youtrack.AddTag("DEV-A-7", "Early", nil) },
			owner: tagOwner("Article", "DEV-7"),
			read:  "/api/articles/DEV-A-7",
		},
		{
			name:  "a tagging of an issue answered with no readable id",
			call:  func() (youtrack.Call, *diag.Fault) { return youtrack.AddTag("DEV-7", "Early", nil) },
			owner: `{"$type":"Issue","idReadable":null}`,
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a removal from an issue answered with two dots",
			call:  func() (youtrack.Call, *diag.Fault) { return youtrack.RemoveTag("DEV-7", "Early", nil) },
			owner: tagOwner("Issue", ".."),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a removal from an issue answered with the id of an article",
			call:  func() (youtrack.Call, *diag.Fault) { return youtrack.RemoveTag("DEV-7", "Early", nil) },
			owner: tagOwner("Issue", "DEV-A-7"),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a removal from an article answered with the id of an issue",
			call:  func() (youtrack.Call, *diag.Fault) { return youtrack.RemoveTag("DEV-A-7", "Early", nil) },
			owner: tagOwner("Article", "DEV-7"),
			read:  "/api/articles/DEV-A-7",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.owner))
			call, fault := tc.call()
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				{Key: "request", Value: render.NewString("GET " + server.URL + tc.read + "?fields=idReadable")},
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(tc.owner)},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, []string{tc.read}, server.Paths())
		})
	}
}

func TestAddTagWritesUnderTheIDsTheReadsFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		owner   string
		targets []string
	}{
		{
			name:    "an issue in lower case",
			written: "dev-7",
			owner:   tagOwner("Issue", "DEV-7"),
			targets: []string{
				"/api/issues/dev-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/issues/DEV-7/tags?fields=id,name,owner(login)",
			},
		},
		{
			name:    "an article in mixed case",
			written: "dev-A-7",
			owner:   tagOwner("Article", "DEV-A-7"),
			targets: []string{
				"/api/articles/dev-A-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/articles/DEV-A-7/tags?fields=id,name,owner(login)",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tagServer(t, map[string]http.HandlerFunc{
				"GET /api/issues/{id}":         fake.JSON(http.StatusOK, tc.owner),
				"GET /api/articles/{id}":       fake.JSON(http.StatusOK, tc.owner),
				"GET /api/tags":                fake.JSON(http.StatusOK, tagsShown()),
				"POST /api/issues/{id}/tags":   fake.JSON(http.StatusOK, tagOf("10-1", "Early", "first")),
				"POST /api/articles/{id}/tags": fake.JSON(http.StatusOK, tagOf("10-1", "Early", "first")),
			})
			call, fault := youtrack.AddTag(tc.written, "early", nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.targets, server.Targets())
			assert.Equal(t, []string{"", "", `{"id":"10-1"}`}, server.Bodies())
		})
	}
}

func TestAddTagPrintsTheTagTheWriteAnsweredWith(t *testing.T) {
	t.Parallel()
	server := tagServer(t, map[string]http.HandlerFunc{
		"GET /api/issues/{id}":       fake.JSON(http.StatusOK, tagOwner("Issue", "DEV-7")),
		"GET /api/tags":              fake.JSON(http.StatusOK, tagsShown()),
		"POST /api/issues/{id}/tags": fake.JSON(http.StatusOK, tagOf("10-1", "Late", "second")),
	})
	call, fault := youtrack.AddTag("dev-7", "early", nil)
	require.Nil(t, fault)

	node, fault := call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(
		render.Pair{Key: "idReadable", Value: render.NewString("DEV-7")},
		render.Pair{Key: "added", Value: tagNode("Late", "second")}), node)
}

func TestAddTagRefusesATagOtherThanTheOneResolved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		owner   string
		kind    string
		write   string
	}{
		{
			name:    "on an issue",
			written: "DEV-7",
			owner:   tagOwner("Issue", "DEV-7"),
			kind:    "issue",
			write:   "/api/issues/DEV-7/tags",
		},
		{
			name:    "on an article",
			written: "DEV-A-7",
			owner:   tagOwner("Article", "DEV-A-7"),
			kind:    "article",
			write:   "/api/articles/DEV-A-7/tags",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			other := tagOf("10-2", "Early", "first")
			server := tagServer(t, map[string]http.HandlerFunc{
				"GET /api/issues/{id}":         fake.JSON(http.StatusOK, tc.owner),
				"GET /api/articles/{id}":       fake.JSON(http.StatusOK, tc.owner),
				"GET /api/tags":                fake.JSON(http.StatusOK, tagsShown()),
				"POST /api/issues/{id}/tags":   fake.JSON(http.StatusOK, other),
				"POST /api/articles/{id}/tags": fake.JSON(http.StatusOK, other),
			})
			call, fault := youtrack.AddTag(tc.written, "early", nil)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, AfterWrite: true, Details: []render.Pair{
				{Key: "request", Value: render.NewString("POST " + server.URL + tc.write + "?fields=id,name,owner(login)")},
				{Key: tc.kind, Value: render.NewString(tc.written)},
				{Key: "tag", Value: render.NewString("early")},
				{Key: "upstream_status", Value: render.NewNumber("200")},
				{Key: "upstream_body", Value: render.NewString(other)},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}

func TestRemoveTagTakesTheTagOffUnderTheIDsTheReadsFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		written  string
		owner    string
		readable string
		targets  []string
	}{
		{
			name:     "an issue in lower case",
			written:  "dev-7",
			owner:    tagOwner("Issue", "DEV-7"),
			readable: "DEV-7",
			targets: []string{
				"/api/issues/dev-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/issues/DEV-7/tags/10-1?",
			},
		},
		{
			name:     "an article in mixed case",
			written:  "dev-A-7",
			owner:    tagOwner("Article", "DEV-A-7"),
			readable: "DEV-A-7",
			targets: []string{
				"/api/articles/dev-A-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/articles/DEV-A-7/tags/10-1?",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			removed := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
			server := tagServer(t, map[string]http.HandlerFunc{
				"GET /api/issues/{id}":                 fake.JSON(http.StatusOK, tc.owner),
				"GET /api/articles/{id}":               fake.JSON(http.StatusOK, tc.owner),
				"GET /api/tags":                        fake.JSON(http.StatusOK, tagsShown()),
				"DELETE /api/issues/{id}/tags/{tag}":   removed,
				"DELETE /api/articles/{id}/tags/{tag}": removed,
			})
			call, fault := youtrack.RemoveTag(tc.written, "early", nil)
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, render.NewMap(
				render.Pair{Key: "idReadable", Value: render.NewString(tc.readable)},
				render.Pair{Key: "removed", Value: tagNode("Early", "first")}), node)
			assert.Equal(t, tc.targets, server.Targets())
			assert.Equal(t, []string{"", "", ""}, server.Bodies())
		})
	}
}
