package script_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/script"
)

// The descriptor is read instead of the value, since reading address looks up the login.
const surveying = `const api = require("ytrack/v1");
exports.command = { short: "Survey the API", long: "List what ytrack/v1 exports." };
exports.run = () => {
  const names = [];
  for (const key of Object.keys(api)) {
    const value = Object.getOwnPropertyDescriptor(api, key).value;
    if (value !== null && typeof value === "object") {
      names.push(...Object.keys(value).map((verb) => key + "." + verb));
    } else {
      names.push(key);
    }
  }
  return { names };
};
`

func runtimeSurface(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".ytrack", "scripts")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "survey.js"), []byte(surveying), 0o644))
	catalog := script.Load(nil, script.Places{Home: home})
	at := slices.IndexFunc(catalog.Commands, func(c *script.Command) bool { return slices.Equal(c.Path, []string{"survey"}) })
	require.GreaterOrEqual(t, at, 0)

	answer, _, fault := script.Run(t.Context(), catalog.Commands[at], script.Input{}, script.Host{})
	require.Nil(t, fault)

	listed, found := answer.Lookup("names")
	require.True(t, found)
	names := []string{}
	for _, item := range listed.Items() {
		names = append(names, item.Value())
	}
	return sorted(names)
}

var (
	declaredEntity = regexp.MustCompile(`^  export const (\w+): \{$`)
	declaredMember = regexp.MustCompile(`^    (\w+)\(`)
	declaredValue  = regexp.MustCompile(`^  export (?:function (\w+)\(|const (\w+): [^{]+;$)`)
)

func declaredSurface(t *testing.T) []string {
	t.Helper()
	declarations, err := os.ReadFile("v1.d.ts")
	require.NoError(t, err)
	var names []string
	var entity string
	for _, line := range strings.Split(string(declarations), "\n") {
		if found := declaredEntity.FindStringSubmatch(line); found != nil {
			entity = found[1]
		}
		if found := declaredMember.FindStringSubmatch(line); found != nil {
			names = append(names, entity+"."+found[1])
		}
		if found := declaredValue.FindStringSubmatch(line); found != nil {
			names = append(names, found[1]+found[2])
		}
	}
	return sorted(names)
}

func sorted(names []string) []string {
	slices.Sort(names)
	return names
}

func TestDeclarationsOfV1NameWhatTheEngineExports(t *testing.T) {
	t.Parallel()
	assert.Equal(t, runtimeSurface(t), declaredSurface(t))
}
