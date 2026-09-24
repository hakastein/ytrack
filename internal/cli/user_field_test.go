package cli_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// Both places a bundle could keep the users a field allows, asked of every field of the project at once.
const allowedUsersAsked = "customFields(field(name),bundle(values(name),aggregatedUsers(login)))"

// The users a user field allows are bundle.aggregatedUsers. Under bundle.values lie the individuals and the
// groups the bundle was built from, so a login asked there answers a team instead. The spec declares no values
// on UserBundle at all, and the server sends the key anyway.
func TestProjectShowFindsTheUsersAUserFieldOfTheDevInstanceAllowsUnderAggregatedUsers(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "DEV", "--fields", allowedUsersAsked)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	fields := customFieldsByName(t, got.stdout)

	assignee := bundleOfField(t, fields, "Assignee")
	assert.Contains(t, assignee["aggregatedUsers"], map[string]any{"login": "admin"})
	assert.Contains(t, assignee["values"], map[string]any{"name": "DEVELOPMENT Team"})
	assert.NotContains(t, assignee["values"], map[string]any{"name": "admin"})

	// An empty bundle allows everyone rather than no one, and its values are empty just the same.
	together := bundleOfField(t, fields, "Соисполнители")
	assert.Contains(t, together, "aggregatedUsers")
	assert.Empty(t, together["aggregatedUsers"])
	assert.Contains(t, together, "values")
	assert.Empty(t, together["values"])

	// An enum bundle declares no aggregatedUsers, so the key is left out rather than refused.
	enum := bundleOfField(t, fields, "Type")
	assert.Contains(t, enum, "values")
	assert.NotContains(t, enum, "aggregatedUsers")

	require.Contains(t, fields, "Оценка")
	assert.NotContains(t, fields["Оценка"], "bundle")

	assert.Len(t, dev.requests(), 1)
}

// customFieldsByName reads the printed document and indexes the custom fields by the name of the field each
// one carries.
func customFieldsByName(t *testing.T, stdout string) map[string]map[string]any {
	t.Helper()
	var printed struct {
		CustomFields []map[string]any `yaml:"customFields"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(stdout), &printed), "stdout: %q", stdout)
	byName := map[string]map[string]any{}
	for _, item := range printed.CustomFields {
		field, ok := item["field"].(map[string]any)
		require.True(t, ok, "an item of customFields: %v", item)
		byName[fmt.Sprint(field["name"])] = item
	}
	return byName
}

func bundleOfField(t *testing.T, byName map[string]map[string]any, field string) map[string]any {
	t.Helper()
	require.Contains(t, byName, field)
	bundle, ok := byName[field]["bundle"].(map[string]any)
	require.True(t, ok, "the bundle of %s: %v", field, byName[field])
	return bundle
}
