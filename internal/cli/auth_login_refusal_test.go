package cli_test

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loginEnvironment(t *testing.T) (env []string, home, path string) {
	t.Helper()
	stated, _ := here(t)
	home, path = emptyHome(t)
	return append(serveNothing(t).env(), "HOME="+home, "PWD="+stated), home, path
}

func TestAuthLoginRefusesAStdinThatIsNotATerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		argv  []string
		stdin func(t *testing.T) *os.File
	}{
		{
			name:  "no stdin at all",
			argv:  []string{"auth", "login"},
			stdin: func(*testing.T) *os.File { return nil },
		},
		{
			name: "a stdin that holds nothing",
			argv: []string{"auth", "login"},
			stdin: func(t *testing.T) *os.File {
				t.Helper()
				empty, err := os.Open(os.DevNull)
				require.NoError(t, err)
				t.Cleanup(func() { _ = empty.Close() })
				return empty
			},
		},
		{
			name:  "no stdin at all, asked for everywhere",
			argv:  []string{"auth", "login", "--global"},
			stdin: func(*testing.T) *os.File { return nil },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, home, path := loginEnvironment(t)

			got := runOn(t, tc.stdin(t), env, tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoToken(t, got, token)
			assert.NoFileExists(t, path)
			assert.Empty(t, entries(t, home))
		})
	}
}

func TestAuthLoginReadsNothingOfThePipeItIsHanded(t *testing.T) {
	t.Parallel()
	env, home, path := loginEnvironment(t)
	const dialogue = "http://h\n" + token + "\n"
	read, write, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = read.Close() })
	_, err = io.WriteString(write, dialogue)
	require.NoError(t, err)
	require.NoError(t, write.Close())

	got := runOn(t, read, env, "auth", "login")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assertNoToken(t, got, token)
	unread, err := io.ReadAll(read)
	require.NoError(t, err)
	assert.Equal(t, dialogue, string(unread))
	assert.NoFileExists(t, path)
	assert.Empty(t, entries(t, home))
}
