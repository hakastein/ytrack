//go:build unix

package cli_test

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFsRefusesANamedPipeRatherThanWaitForIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call string
	}{
		{name: "reading", call: `fs.readFileSync(at)`},
		{name: "writing", call: `fs.writeFileSync(at, "x")`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pipe := filepath.Join(t.TempDir(), "pipe")
			require.NoError(t, syscall.Mkfifo(pipe, 0o600))

			got := runScripts(t, fake.ServeNothing(t), onPath(`exports.command = (at) => { `+tc.call+`; return {}; };`), "run", pipe)

			assert.Equal(t, faultDocument{code: "bad_usage", details: []detail{{"path", pipe}}}, requireFault(t, got))
		})
	}
}
