package cli_test

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The document names the version, the revision and whether the checkout was dirty, in that order,
// and each of the three comes off the stamp go build left in the binary. cmd/ytrack reads that stamp and hands
// it to Run, so a probe hands one of its own: under go test the binary is the module's test binary, and a test
// binary carries no vcs setting at all — read here, the two lines that make the revision and the dirty bit
// would be reachable from nothing.
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
			// The keys of the document are the document's own, in its own order, whatever order the stamp
			// carries them in.
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
			server := serveNothing(t)

			got := runBuiltFrom(t, tc.build, nil, server.env(), "--version")

			assert.Equal(t, 0, got.code)
			assert.Empty(t, got.stderr)
			assert.Equal(t, tc.document, got.stdout)
			assert.Empty(t, server.requests())
		})
	}
}

// The flag is the root's own and no command inherits it, which is what "only on the root" means.
func TestVersionStandsOnTheRootAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a group", argv: []string{"project", "--version"}},
		{name: "a command", argv: []string{"project", "show", "--version"}},
		{name: "a list", argv: []string{"issue", "list", "--version"}},
		// cobra finds the command with the flags stripped, so the word after the flag routes the call and the
		// flag is then read against that command rather than against the root.
		{name: "a group named after the flag", argv: []string{"--version", "project"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// No shorthand is taken, so -v stays free for whatever earns it later.
func TestVersionHasNoShorthand(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "-v")

	assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// A word beside the flag is a command, and an unknown one is refused as it is anywhere else: the flag
// neither swallows the word nor answers in its place.
func TestVersionWithAWordIsRefusedAsACommand(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "--version", "bogus")

	assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// Cobra answers the help flag before it runs anything, so --help wins over --version. The help names the
// flag, which is how a caller finds it at all.
func TestVersionBehindHelpPrintsTheHelp(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "--version", "--help")

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "--version")
	assert.Contains(t, availableCommands(t, got.stdout), "project")
	assert.Empty(t, server.requests())
}
