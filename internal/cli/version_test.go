package cli_test

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestVersionPrintsTheStampOfTheBuildAsOneDocument(t *testing.T) {
	t.Parallel()
	const revision = "3e002df0d6bb4e0e2b1e5e6a8ad2b4c39f7ca0d1"
	tests := []struct {
		name     string
		build    *debug.BuildInfo
		document string
	}{
		{
			name: "a build of a checkout as it was committed",
			build: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.1.0"},
				Settings: []debug.BuildSetting{
					{Key: "-compiler", Value: "gc"},
					{Key: "vcs", Value: "git"},
					{Key: "vcs.revision", Value: revision},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			document: "version: \"v0.1.0\"\nrevision: \"" + revision + "\"\nmodified: false\n",
		},
		{
			name: "a build of a checkout that had been edited",
			build: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.modified", Value: "true"},
					{Key: "vcs.revision", Value: revision},
				},
			},
			document: "version: \"(devel)\"\nrevision: \"" + revision + "\"\nmodified: true\n",
		},
		{
			name: "a build made outside a checkout, or with -buildvcs=false",
			build: &debug.BuildInfo{
				Main:     debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{{Key: "-buildvcs", Value: "false"}},
			},
			document: "version: \"(devel)\"\nrevision: null\nmodified: null\n",
		},
		{
			name:     "a binary the Go toolchain did not build",
			build:    nil,
			document: "version: null\nrevision: null\nmodified: null\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runBuiltFrom(t, tc.build, nil, server.Env(), "--version")

			assert.Equal(t, 0, got.code)
			assert.Empty(t, got.stderr)
			assert.Equal(t, tc.document, got.stdout)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestVersionWithAWordIsRefusedAsACommand(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "--version", "bogus")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}
