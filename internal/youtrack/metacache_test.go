package youtrack_test

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	fieldMetaFirstPath  = fieldMetaBindingPath + "1-1"
	fieldMetaSecondPath = fieldMetaBindingPath + "1-2"
)

type fieldMetaInstance struct {
	mu       sync.Mutex
	projects map[string]string
	answers  map[string]http.HandlerFunc
}

func fieldMetaOneField() *fieldMetaInstance {
	naming := fieldMetaEnum("Field", `"Translated"`)
	return &fieldMetaInstance{
		projects: map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-1", naming))},
		answers:  map[string]http.HandlerFunc{"1-1": fieldMetaAnswering(naming)},
	}
}

func fieldMetaAnswering(naming string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, fieldMetaAnswer(naming))
}

func (i *fieldMetaInstance) change(projects map[string]string, answers map[string]http.HandlerFunc) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.projects, i.answers = projects, answers
}

func (i *fieldMetaInstance) serve(t *testing.T) *fake.Server {
	t.Helper()
	routes := http.NewServeMux()
	routes.HandleFunc("GET /api/admin/projects/{code}", func(w http.ResponseWriter, r *http.Request) {
		i.mu.Lock()
		metadata := i.projects[r.PathValue("code")]
		i.mu.Unlock()
		fake.JSON(http.StatusOK, metadata)(w, r)
	})
	routes.HandleFunc("GET /api/admin/projects/{code}/customFields/{id}", func(w http.ResponseWriter, r *http.Request) {
		i.mu.Lock()
		answer, held := i.answers[r.PathValue("id")]
		i.mu.Unlock()
		if !held {
			answer = fake.JSON(http.StatusNotFound, `{}`)
		}
		answer(w, r)
	})
	return fake.Serve(t, routes.ServeHTTP)
}

func fieldMetaCached(t *testing.T, server *fake.Server, root string) *youtrack.Client {
	t.Helper()
	return youtrack.New(server.Address(t), fake.Token, root)
}

func fieldMetaShowOf(t *testing.T, c *youtrack.Client, project, name string) (*render.Node, *diag.Fault) {
	t.Helper()
	call, fault := youtrack.ShowField(project, name, new("field(name)"))
	require.Nil(t, fault)
	return call(t.Context(), c)
}

func TestShowFieldAnswersFromTheMetadataItCached(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		naming string
	}{
		{name: "a field with a translation", naming: fieldMetaOf("Field", `"Translated"`, "enum", false)},
		{name: "a field with no translation", naming: fieldMetaOf("Field", `null`, "enum", false)},
		{name: "a field with an empty translation", naming: fieldMetaOf("Field", `""`, "enum", false)},
		{name: "a field of many values", naming: fieldMetaOf("Field", `null`, "enum", true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := &fieldMetaInstance{
				projects: map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-1", tc.naming))},
				answers:  map[string]http.HandlerFunc{"1-1": fieldMetaAnswering(tc.naming)},
			}
			server, root := held.serve(t), t.TempDir()
			first, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
			require.Nil(t, fault)

			second, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

			require.Nil(t, fault)
			assert.Equal(t, fieldMetaNamed("Field"), second)
			assert.Equal(t, first, second)
			assert.Equal(t, []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath}, server.Paths())
		})
	}
}

func TestShowFieldReadsTheMetadataAgainWhenTheCacheMisses(t *testing.T) {
	t.Parallel()
	field, added := fieldMetaEnum("Field", `"Translated"`), fieldMetaEnum("Added", `null`)
	user := fieldMetaOf("Field", `"Translated"`, "user", false)
	tests := []struct {
		name     string
		projects map[string]string
		answers  map[string]http.HandlerFunc
		asked    string
		paths    []string
	}{
		{
			name:     "a field added since",
			projects: map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-1", field), fieldMetaBinding("1-2", added))},
			answers:  map[string]http.HandlerFunc{"1-1": fieldMetaAnswering(field), "1-2": fieldMetaAnswering(added)},
			asked:    "Added",
			paths:    []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaSecondPath},
		},
		{
			name:     "a field that changed its type since",
			projects: map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-1", user))},
			answers:  map[string]http.HandlerFunc{"1-1": fieldMetaAnswering(user)},
			asked:    "Field",
			paths:    []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaFirstPath},
		},
		{
			name:     "a field bound again under another id since",
			projects: map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-2", field))},
			answers:  map[string]http.HandlerFunc{"1-2": fieldMetaAnswering(field)},
			asked:    "Field",
			paths:    []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaSecondPath},
		},
		{
			name:     "an answer to the request of the cached field that cannot be read",
			projects: map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-1", field))},
			answers:  map[string]http.HandlerFunc{"1-1": fake.InTurn(fake.JSON(http.StatusOK, `[]`), fieldMetaAnswering(field))},
			asked:    "Field",
			paths:    []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaFirstPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := fieldMetaOneField()
			server, root := held.serve(t), t.TempDir()
			_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
			require.Nil(t, fault)
			held.change(tc.projects, tc.answers)

			got, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", tc.asked)

			require.Nil(t, fault)
			assert.Equal(t, fieldMetaNamed(tc.asked), got)
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestShowFieldRefusesANameOnlyAfterReadingTheMetadataAgain(t *testing.T) {
	t.Parallel()
	kept := fieldMetaEnum("Kept", `null`)
	tests := []struct {
		name     string
		projects map[string]string
		answers  map[string]http.HandlerFunc
		asked    string
		nearest  []string
		paths    []string
	}{
		{
			name:     "a name the cache does not hold",
			projects: fieldMetaOneField().projects,
			answers:  fieldMetaOneField().answers,
			asked:    "Other",
			nearest:  []string{"Field"},
			paths:    []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaPath},
		},
		{
			name:     "a field removed since",
			projects: map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-2", kept))},
			answers:  map[string]http.HandlerFunc{"1-2": fieldMetaAnswering(kept)},
			asked:    "Field",
			nearest:  []string{"Kept"},
			paths:    []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath, fieldMetaPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := fieldMetaOneField()
			server, root := held.serve(t), t.TempDir()
			_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
			require.Nil(t, fault)
			held.change(tc.projects, tc.answers)

			_, fault = fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", tc.asked)

			assert.Equal(t, fieldMetaUnknown(t, server, tc.asked, tc.nearest...), refusal(t, fault))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestShowFieldReadsTheMetadataAgainForACachedTypeItDoesNotModel(t *testing.T) {
	t.Parallel()
	stateOfOne := fieldMetaOf("State", `null`, "state", false)
	held := &fieldMetaInstance{projects: map[string]string{
		"DEV": fieldMetaProject(fieldMetaBinding("1-1", fieldMetaOf("State", `null`, "state", true))),
	}}
	server, root := held.serve(t), t.TempDir()
	call, fault := youtrack.ShowField("DEV", "State", nil)
	require.Nil(t, fault)
	_, fault = call(t.Context(), fieldMetaCached(t, server, root))
	require.NotNil(t, fault)
	held.change(map[string]string{"DEV": fieldMetaProject(fieldMetaBinding("1-1", stateOfOne))},
		map[string]http.HandlerFunc{"1-1": fieldMetaAnswering(stateOfOne)})

	_, fault = call(t.Context(), fieldMetaCached(t, server, root))

	require.Nil(t, fault)
	assert.Equal(t, []string{fieldMetaPath, fieldMetaPath, fieldMetaFirstPath}, server.Paths())
}

func TestShowFieldLeavesTheCacheWarmAfterAFault(t *testing.T) {
	t.Parallel()
	field := fieldMetaEnum("Field", `"Translated"`)
	tests := []struct {
		name     string
		metadata string
		refused  string
	}{
		{name: "a name the project does not have", metadata: fieldMetaProject(fieldMetaBinding("1-1", field)), refused: "Other"},
		{
			name:     "a name whose id no path can hold",
			metadata: fieldMetaProject(fieldMetaBinding("1-1", field), fieldMetaBinding("..", fieldMetaEnum("Broken", `null`))),
			refused:  "Broken",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := &fieldMetaInstance{
				projects: map[string]string{"DEV": tc.metadata},
				answers:  map[string]http.HandlerFunc{"1-1": fieldMetaAnswering(field)},
			}
			server, root := held.serve(t), t.TempDir()
			_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", tc.refused)
			require.NotNil(t, fault)

			got, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

			require.Nil(t, fault)
			assert.Equal(t, fieldMetaNamed("Field"), got)
			assert.Equal(t, []string{fieldMetaPath, fieldMetaFirstPath}, server.Paths())
		})
	}
}

func TestShowFieldRefusesAnIdNoPathCanHoldOverTheCacheAsWell(t *testing.T) {
	t.Parallel()
	metadata := fieldMetaProject(fieldMetaBinding("..", fieldMetaEnum("Field", `null`)))
	held := &fieldMetaInstance{projects: map[string]string{"DEV": metadata}}
	server, root := held.serve(t), t.TempDir()
	_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
	require.NotNil(t, fault)

	_, fault = fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

	assert.Equal(t, unreadable(lastRequest(t, server), metadata), refusal(t, fault))
	assert.Equal(t, []string{fieldMetaPath, fieldMetaPath}, server.Paths())
}

func TestShowFieldPassesOnAFailureOfTheFieldUnderAWarmCache(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		code   diag.Code
		paths  []string
	}{
		{name: "401", status: http.StatusUnauthorized, code: diag.Denied, paths: []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath}},
		{name: "403", status: http.StatusForbidden, code: diag.Denied, paths: []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath}},
		{name: "400", status: http.StatusBadRequest, code: diag.Rejected, paths: []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath}},
		{name: "500", status: http.StatusInternalServerError, code: diag.UpstreamFailed, paths: []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath}},
		{name: "503", status: http.StatusServiceUnavailable, code: diag.UpstreamFailed, paths: []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath}},
		{
			name:   "404, which is a miss",
			status: http.StatusNotFound,
			code:   diag.NotFound,
			paths:  []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaFirstPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := fieldMetaOneField()
			server, root := held.serve(t), t.TempDir()
			_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
			require.Nil(t, fault)
			held.change(fieldMetaOneField().projects, map[string]http.HandlerFunc{"1-1": fake.JSON(tc.status, `{}`)})

			_, fault = fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

			want := diag.Fault{Code: tc.code, Details: []render.Pair{
				lastRequest(t, server),
				{Key: "upstream_status", Value: render.NewNumber(json.Number(strconv.Itoa(tc.status)))},
			}}
			assert.Equal(t, want, refusal(t, fault))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestShowFieldKeepsTheCacheOfOneTokenFromAnother(t *testing.T) {
	t.Parallel()
	server, root := fieldMetaOneField().serve(t), t.TempDir()
	_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
	require.Nil(t, fault)

	_, fault = fieldMetaShowOf(t, youtrack.New(server.Address(t), fake.Token+"-of-another-user", root), "DEV", "Field")

	require.Nil(t, fault)
	assert.Equal(t, []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaFirstPath}, server.Paths())
}

func TestShowFieldKeepsTheCacheOfOneAddressFromAnother(t *testing.T) {
	t.Parallel()
	held, root := fieldMetaOneField(), t.TempDir()
	first, second := held.serve(t), held.serve(t)
	_, fault := fieldMetaShowOf(t, fieldMetaCached(t, first, root), "DEV", "Field")
	require.Nil(t, fault)

	_, fault = fieldMetaShowOf(t, fieldMetaCached(t, second, root), "DEV", "Field")

	require.Nil(t, fault)
	assert.Equal(t, []string{fieldMetaPath, fieldMetaFirstPath}, second.Paths())
}

func TestShowFieldKeepsTheCacheOfOneProjectFromAnother(t *testing.T) {
	t.Parallel()
	field, other := fieldMetaEnum("Field", `null`), fieldMetaEnum("Other", `null`)
	server := (&fieldMetaInstance{
		projects: map[string]string{
			"DEV":  fieldMetaProject(fieldMetaBinding("1-1", field)),
			"DOCS": fieldMetaProject(fieldMetaBinding("2-1", other)),
		},
		answers: map[string]http.HandlerFunc{"1-1": fieldMetaAnswering(field), "2-1": fieldMetaAnswering(other)},
	}).serve(t)
	root := t.TempDir()
	_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
	require.Nil(t, fault)
	_, fault = fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DOCS", "Other")
	require.Nil(t, fault)

	got, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

	require.Nil(t, fault)
	assert.Equal(t, fieldMetaNamed("Field"), got)
	assert.Equal(t, []string{fieldMetaPath, fieldMetaFirstPath, "/api/admin/projects/DOCS",
		"/api/admin/projects/DOCS/customFields/2-1", fieldMetaFirstPath}, server.Paths())
}

type fieldMetaCacheEntry struct {
	path string
	dir  bool
	mode fs.FileMode
}

func fieldMetaCacheEntries(t *testing.T, root string) (entries []fieldMetaCacheEntry, content string) {
	t.Helper()
	digest := regexp.MustCompile(`[0-9a-f]{64}`)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		relative, err := filepath.Rel(root, path)
		require.NoError(t, err)
		info, err := entry.Info()
		require.NoError(t, err)
		entries = append(entries, fieldMetaCacheEntry{
			path: digest.ReplaceAllString(filepath.ToSlash(relative), "<sha256>"),
			dir:  entry.IsDir(),
			mode: info.Mode().Perm(),
		})
		if !entry.IsDir() {
			held, err := os.ReadFile(path)
			require.NoError(t, err)
			content += string(held)
		}
		return nil
	}))
	return entries, content
}

func TestShowFieldCachesTheMetadataForTheLoginAlone(t *testing.T) {
	t.Parallel()
	server, root := fieldMetaOneField().serve(t), filepath.Join(t.TempDir(), "cache")

	_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

	require.Nil(t, fault)
	entries, content := fieldMetaCacheEntries(t, root)
	assert.Equal(t, []fieldMetaCacheEntry{
		{path: ".", dir: true, mode: 0o700},
		{path: "<sha256>", dir: true, mode: 0o700},
		{path: "<sha256>/<sha256>", mode: 0o600},
	}, entries)
	assert.NotContains(t, content, fake.Token)
}

func TestShowFieldSaysNothingOfACacheItCannotWrite(t *testing.T) {
	t.Parallel()
	server, home := fieldMetaOneField().serve(t), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "file"), nil, 0o600))
	root := filepath.Join(home, "file", "cache")
	_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
	require.Nil(t, fault)

	got, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

	require.Nil(t, fault)
	assert.Equal(t, fieldMetaNamed("Field"), got)
	assert.Equal(t, []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaFirstPath}, server.Paths())
}

func fieldMetaTruncateCache(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		return os.Truncate(path, 0)
	}))
}

func TestShowFieldReadsTheMetadataAgainWhenTheCacheDoesNotReadBack(t *testing.T) {
	t.Parallel()
	server, root := fieldMetaOneField().serve(t), t.TempDir()
	_, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")
	require.Nil(t, fault)
	fieldMetaTruncateCache(t, root)

	got, fault := fieldMetaShowOf(t, fieldMetaCached(t, server, root), "DEV", "Field")

	require.Nil(t, fault)
	assert.Equal(t, fieldMetaNamed("Field"), got)
	assert.Equal(t, []string{fieldMetaPath, fieldMetaFirstPath, fieldMetaPath, fieldMetaFirstPath}, server.Paths())
}
