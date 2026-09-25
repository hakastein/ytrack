package cli_test

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	metadataPath    = "/api/admin/projects/DEV"
	firstFieldPath  = metadataPath + "/customFields/180-1"
	secondFieldPath = metadataPath + "/customFields/180-2"
)

const (
	typeAsAUser = `{"$type":"UserProjectCustomField","id":"180-1","field":{"$type":"CustomField","name":"Type",` +
		`"localizedName":"Тип","fieldType":{"$type":"FieldType","valueType":"user","isMultiValue":false}}}`
	typeAsAUserAnswered = `{"$type":"UserProjectCustomField","field":{"$type":"CustomField","name":"Type",` +
		`"localizedName":"Тип","fieldType":{"$type":"FieldType","valueType":"user","isMultiValue":false}},` +
		`"canBeEmpty":false,"bundle":{"$type":"UserBundle","values":[],"aggregatedUsers":[{"$type":"User","login":"admin"}]}}`

	printedEnumType = `field:
  name: "Type"
  localizedName: "Тип"
  fieldType:
    valueType: "enum"
    isMultiValue: false
canBeEmpty: false
bundle:
  values: []
`
	printedUserType = `field:
  name: "Type"
  localizedName: "Тип"
  fieldType:
    valueType: "user"
    isMultiValue: false
canBeEmpty: false
bundle:
  aggregatedUsers:
    - {login: "admin"}
`
	printedDueDate = `field:
  name: "Срок"
  localizedName: null
  fieldType:
    valueType: "enum"
    isMultiValue: false
canBeEmpty: true
bundle:
  values: []
`
)

type instance struct {
	mu       sync.Mutex
	metadata string
	fields   map[string]string
}

func typeOnlyInstance() *instance {
	return &instance{
		metadata: projectMetadata(projectField("180-1", "Type", "Тип")),
		fields:   map[string]string{"180-1": oneField("Type", "Тип", false)},
	}
}

func (i *instance) setMetadata(metadata string, fields map[string]string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.metadata, i.fields = metadata, fields
}

func (i *instance) handle(w http.ResponseWriter, r *http.Request) {
	i.mu.Lock()
	metadata, fields := i.metadata, i.fields
	i.mu.Unlock()
	id, isOneField := strings.CutPrefix(r.URL.Path, metadataPath+"/customFields/")
	if !isOneField {
		respondWith(http.StatusOK, metadata)(w, r)
		return
	}
	held, there := fields[id]
	if !there {
		respondWith(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id `+id+` not found"}`)(w, r)
		return
	}
	respondWith(http.StatusOK, held)(w, r)
}

func atHome(u *upstream, home string) []string {
	return append(u.env(), "HOME="+home)
}

func aProject(t *testing.T, held *instance) (*upstream, string) {
	t.Helper()
	return serve(t, held.handle), t.TempDir()
}

func TestFieldShowTakesTheMetadataTheRunBeforeLeftOnDisk(t *testing.T) {
	t.Parallel()
	server, home := aProject(t, typeOnlyInstance())

	first := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	second := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: printedEnumType}, first)
	assert.Equal(t, first, second)
	assert.Equal(t, []string{metadataPath, firstFieldPath, firstFieldPath}, server.sentPaths())
}

func TestFieldShowReadsTheMetadataAgainForAFieldAddedSinceTheCacheWasWritten(t *testing.T) {
	t.Parallel()
	held := typeOnlyInstance()
	server, home := aProject(t, held)
	runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	held.setMetadata(
		projectMetadata(projectField("180-1", "Type", "Тип"), projectField("180-2", "Срок", "")),
		map[string]string{"180-1": oneField("Type", "Тип", false), "180-2": oneField("Срок", "", true)},
	)

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Срок")

	assert.Equal(t, outcome{stdout: printedDueDate}, got)
	assert.Equal(t, []string{metadataPath, firstFieldPath, metadataPath, secondFieldPath}, server.sentPaths())
}

func TestFieldShowRefusesAnUnknownNameOnlyAfterReadingTheMetadataAgain(t *testing.T) {
	t.Parallel()
	server, home := aProject(t, typeOnlyInstance())
	runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Нет")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", metadataRequest(server.url, "DEV")},
			{"project", "DEV"},
			{"unknown", []any{unknownEntry("Нет", "Type")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{metadataPath, firstFieldPath, metadataPath}, server.sentPaths())
}

func TestFieldShowLeavesTheCacheWarmAfterAFault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		refused  string
		metadata string
	}{
		{
			name:     "a name the project does not have",
			refused:  "Нет",
			metadata: projectMetadata(projectField("180-1", "Type", "Тип")),
		},
		{
			name:     "a name that resolves to an id no path can hold",
			refused:  "Срок",
			metadata: projectMetadata(projectField("180-1", "Type", "Тип"), projectField("..", "Срок", "")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := &instance{metadata: tc.metadata, fields: map[string]string{"180-1": oneField("Type", "Тип", false)}}
			server, home := aProject(t, held)
			require.Equal(t, 1, runWith(t, atHome(server, home), "field", "show", "DEV", tc.refused).code)

			got := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

			assert.Equal(t, outcome{stdout: printedEnumType}, got)
			assert.Equal(t, []string{metadataPath, firstFieldPath}, server.sentPaths())
		})
	}
}

func TestFieldShowRefusesAnIdItCannotAddressOverTheCacheAsWell(t *testing.T) {
	t.Parallel()
	metadata := projectMetadata(projectField("..", "Type", "Тип"))
	server, home := aProject(t, &instance{metadata: metadata, fields: map[string]string{}})

	first := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	second := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", metadataRequest(server.url, "DEV")},
			{"upstream_status", 200},
			{"upstream_body", metadata},
		},
	}
	assert.Equal(t, want, requireFault(t, first))
	assert.Equal(t, want, requireFault(t, second))
	assert.Equal(t, []string{metadataPath, metadataPath}, server.sentPaths())
}

func TestFieldShowRefusesAFieldRemovedSinceTheCacheWasWrittenByName(t *testing.T) {
	t.Parallel()
	held := &instance{
		metadata: projectMetadata(projectField("180-1", "Type", "Тип"), projectField("180-2", "Priority", "Приоритет")),
		fields:   map[string]string{"180-1": oneField("Type", "Тип", false), "180-2": oneField("Priority", "Приоритет", false)},
	}
	server, home := aProject(t, held)
	runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	held.setMetadata(
		projectMetadata(projectField("180-2", "Priority", "Приоритет")),
		map[string]string{"180-2": oneField("Priority", "Приоритет", false)},
	)

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	want := faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", metadataRequest(server.url, "DEV")},
			{"project", "DEV"},
			{"unknown", []any{unknownEntry("Type", "Priority")}},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, []string{metadataPath, firstFieldPath, firstFieldPath, metadataPath}, server.sentPaths())
}

func TestFieldShowAsksAgainForAFieldThatChangedTypeSinceTheCacheWasWritten(t *testing.T) {
	t.Parallel()
	held := typeOnlyInstance()
	server, home := aProject(t, held)
	runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	held.setMetadata(projectMetadata(typeAsAUser), map[string]string{"180-1": typeAsAUserAnswered})

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: printedUserType}, got)
	assert.Equal(t, []string{metadataPath, firstFieldPath, firstFieldPath, metadataPath, firstFieldPath}, server.sentPaths())
	assert.Equal(t, []string{metadataSent, fieldShowDefault(bundleValues), fieldShowDefault(bundleValues),
		metadataSent, fieldShowDefault(bundleUsers)}, server.sentFields())
}

func TestFieldShowTakesAFieldWithNoTranslationOffTheDiskAsItWas(t *testing.T) {
	t.Parallel()
	server, home := aProject(t, &instance{
		metadata: projectMetadata(projectField("180-1", "Срок", "")),
		fields:   map[string]string{"180-1": oneField("Срок", "", true)},
	})

	first := runWith(t, atHome(server, home), "field", "show", "DEV", "Срок")
	second := runWith(t, atHome(server, home), "field", "show", "DEV", "Срок")

	assert.Equal(t, outcome{stdout: printedDueDate}, first)
	assert.Equal(t, first, second)
	assert.Equal(t, []string{metadataPath, firstFieldPath, firstFieldPath}, server.sentPaths())
}

func TestFieldShowKeepsTheMetadataOfOneProjectOutOfAnothers(t *testing.T) {
	t.Parallel()
	type project struct {
		id       string
		metadata string
		field    string
	}
	held := map[string]project{
		"DEV":  {id: "180-1", metadata: projectMetadata(projectField("180-1", "Type", "Тип")), field: oneField("Type", "Тип", false)},
		"DOCS": {id: "181-1", metadata: projectMetadata(projectField("181-1", "Срок", "")), field: oneField("Срок", "", true)},
	}
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		code, id, isOneField := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/admin/projects/"), "/customFields/")
		project, known := held[code]
		if !assert.True(t, known, "path %s", r.URL.Path) {
			return
		}
		if !isOneField {
			respondWith(http.StatusOK, project.metadata)(w, r)
			return
		}
		assert.Equal(t, project.id, id)
		respondWith(http.StatusOK, project.field)(w, r)
	})
	home := t.TempDir()
	runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	runWith(t, atHome(server, home), "field", "show", "DOCS", "Срок")

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: printedEnumType}, got)
	assert.Equal(t, []string{metadataPath, firstFieldPath, "/api/admin/projects/DOCS",
		"/api/admin/projects/DOCS/customFields/181-1", firstFieldPath}, server.sentPaths())
}

const (
	typeAsAGroup = `{"$type":"GroupProjectCustomField","id":"180-1","field":{"$type":"CustomField","name":"Type",` +
		`"localizedName":"Тип","fieldType":{"$type":"FieldType","valueType":"group","isMultiValue":false}}}`
	typeAsAGroupAnswered = `{"$type":"GroupProjectCustomField","field":{"$type":"CustomField","name":"Type",` +
		`"localizedName":"Тип","fieldType":{"$type":"FieldType","valueType":"group","isMultiValue":false}},` +
		`"canBeEmpty":false}`
	typeWithoutTheBundle = `{"$type":"EnumProjectCustomField","field":{"$type":"CustomField","name":"Type",` +
		`"localizedName":"Тип","fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}},` +
		`"canBeEmpty":false}`

	printedGroupType = `field:
  name: "Type"
  localizedName: "Тип"
  fieldType:
    valueType: "group"
    isMultiValue: false
canBeEmpty: false
`
)

func TestFieldShowAsksAgainWhenTheResponseToACachedRequestIsInvalid(t *testing.T) {
	t.Parallel()
	held := typeOnlyInstance()
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/customFields/") && !strings.Contains(r.URL.Query().Get("fields"), "bundle") {
			respondWith(http.StatusOK, typeAsAGroupAnswered)(w, r)
			return
		}
		held.handle(w, r)
	})
	home := t.TempDir()
	runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	held.setMetadata(projectMetadata(typeAsAGroup), map[string]string{"180-1": typeWithoutTheBundle})

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: printedGroupType}, got)
	assert.Equal(t, []string{metadataPath, firstFieldPath, firstFieldPath, metadataPath, firstFieldPath}, server.sentPaths())
}

const (
	stateOfOne = `{"$type":"StateProjectCustomField","id":"180-1","field":{"$type":"CustomField","name":"State",` +
		`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"state","isMultiValue":false}}}`
	stateOfOneAnswered = `{"$type":"StateProjectCustomField","field":{"$type":"CustomField","name":"State",` +
		`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"state","isMultiValue":false}},` +
		`"canBeEmpty":true,"bundle":{"$type":"StateBundle","values":[]}}`

	printedStateType = `field:
  name: "State"
  localizedName: null
  fieldType:
    valueType: "state"
    isMultiValue: false
canBeEmpty: true
bundle:
  values: []
`
)

func TestFieldShowReadsTheMetadataAgainForACachedTypeOutsideTheCatalogue(t *testing.T) {
	t.Parallel()
	held := &instance{metadata: projectMetadata(stateOfMany), fields: map[string]string{}}
	server, home := aProject(t, held)
	require.Equal(t, 1, runWith(t, atHome(server, home), "field", "show", "DEV", "State").code)
	held.setMetadata(projectMetadata(stateOfOne), map[string]string{"180-1": stateOfOneAnswered})

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "State")

	assert.Equal(t, outcome{stdout: printedStateType}, got)
	assert.Equal(t, []string{metadataPath, metadataPath, firstFieldPath}, server.sentPaths())
}

func TestFieldShowKeepsTheMetadataOfOneIdentityOutOfAnothers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		again   func(first, second *upstream, home string) []string
		reached func(first, second *upstream) *upstream
	}{
		{
			name: "another token",
			again: func(first, _ *upstream, home string) []string {
				return []string{"YTRACK_URL=" + first.url, "YTRACK_TOKEN=" + token + "-of-another-user", "HOME=" + home}
			},
			reached: func(first, _ *upstream) *upstream { return first },
		},
		{
			name:    "another address",
			again:   func(_, second *upstream, home string) []string { return atHome(second, home) },
			reached: func(_, second *upstream) *upstream { return second },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := typeOnlyInstance()
			first, second := serve(t, held.handle), serve(t, held.handle)
			home := t.TempDir()
			runWith(t, atHome(first, home), "field", "show", "DEV", "Type")
			reached := tc.reached(first, second)
			already := len(reached.requests())

			got := runWith(t, tc.again(first, second, home), "field", "show", "DEV", "Type")

			assert.Equal(t, outcome{stdout: printedEnumType}, got)
			assert.Equal(t, []string{metadataPath, firstFieldPath}, reached.sentPaths()[already:])
		})
	}
}

func TestFieldShowKeepsNoCacheWithoutAnAbsoluteHome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		home []string
	}{
		{name: "no home directory"},
		{name: "a relative home directory", home: []string{"HOME=" + filepath.Join("relative", "home")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, _ := aProject(t, typeOnlyInstance())
			env := append(server.env(), tc.home...)

			first := runWith(t, env, "field", "show", "DEV", "Type")
			second := runWith(t, env, "field", "show", "DEV", "Type")

			assert.Equal(t, outcome{stdout: printedEnumType}, first)
			assert.Equal(t, first, second)
			assert.Equal(t, []string{metadataPath, firstFieldPath, metadataPath, firstFieldPath}, server.sentPaths())
		})
	}
}

func TestFieldShowWritesTheCacheUnderOneDirectoryOfItsOwn(t *testing.T) {
	t.Parallel()
	server, home := aProject(t, typeOnlyInstance())

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	require.Equal(t, outcome{stdout: printedEnumType}, got)
	var directories, files []string
	require.NoError(t, filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		if path == home {
			return nil
		}
		relative, err := filepath.Rel(home, path)
		require.NoError(t, err)
		info, err := entry.Info()
		require.NoError(t, err)
		if entry.IsDir() {
			assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "directory %s", relative)
			directories = append(directories, relative)
			return nil
		}
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "file %s", relative)
		files = append(files, relative)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(content), token, "file %s", relative)
		return nil
	}))
	cache := filepath.Join(".ytrack", "cache")
	require.Len(t, directories, 3, "directories: %v", directories)
	assert.Equal(t, []string{".ytrack", cache}, directories[:2])
	assert.Regexp(t, `^[0-9a-f]{64}$`, filepath.Base(directories[2]))
	assert.Equal(t, cache, filepath.Dir(directories[2]))
	require.Len(t, files, 1, "files: %v", files)
	assert.Regexp(t, `^[0-9a-f]{64}$`, filepath.Base(files[0]))
	assert.Equal(t, directories[2], filepath.Dir(files[0]))
}

func TestFieldShowSaysNothingOfACacheItCannotWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(t *testing.T, home string)
	}{
		{
			name: "a home directory nothing may be created in",
			prepare: func(t *testing.T, home string) {
				if os.Geteuid() == 0 {
					t.Skip("root creates a directory whatever the mode of the one above it")
				}
				require.NoError(t, os.Chmod(home, 0o500))
				t.Cleanup(func() { assert.NoError(t, os.Chmod(home, 0o700)) })
			},
		},
		{
			name: "a ytrack directory that is a file",
			prepare: func(t *testing.T, home string) {
				require.NoError(t, os.WriteFile(filepath.Join(home, ".ytrack"), nil, 0o600))
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, home := aProject(t, typeOnlyInstance())
			tc.prepare(t, home)

			first := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
			second := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

			assert.Equal(t, outcome{stdout: printedEnumType}, first)
			assert.Equal(t, first, second)
			assert.Equal(t, []string{metadataPath, firstFieldPath, metadataPath, firstFieldPath}, server.sentPaths())
		})
	}
}

func TestFieldShowReadsTheMetadataAgainWhenTheCacheDoesNotReadBack(t *testing.T) {
	t.Parallel()
	server, home := aProject(t, typeOnlyInstance())
	runWith(t, atHome(server, home), "field", "show", "DEV", "Type")
	require.NoError(t, filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		return os.Truncate(path, 0)
	}))

	got := runWith(t, atHome(server, home), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: printedEnumType}, got)
	assert.Equal(t, []string{metadataPath, firstFieldPath, metadataPath, firstFieldPath}, server.sentPaths())
}

func TestFieldShowReadsTheMetadataOfTheDevInstanceOnceForTwoRuns(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	home := t.TempDir()

	first := runWith(t, atHome(dev, home), "field", "show", "DEV", "Type")
	second := runWith(t, atHome(dev, home), "field", "show", "DEV", "Type")

	assert.Equal(t, outcome{stdout: typeOfDEV}, first)
	assert.Equal(t, first, second)
	paths := dev.sentPaths()
	require.Len(t, paths, 3, "paths: %v", paths)
	assert.Equal(t, metadataPath, paths[0])
	assert.Regexp(t, `^`+metadataPath+`/customFields/[0-9]+-[0-9]+$`, paths[1])
	assert.Equal(t, paths[1], paths[2])
}
