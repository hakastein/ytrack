package cli_test

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The environment holds an address and a token this command is not to use, and the file of login records is not to
// be written: a refusal that reached either would show up here.
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

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assertNoToken(t, got, token)
			assert.NoFileExists(t, path)
			assert.Empty(t, entries(t, home))
		})
	}
}

// Piping a token in is the thing the refusal exists to make impossible, and the proof is the pipe itself: after the
// call every byte written to it is still there to be read, so nothing of it reached the command.
func TestAuthLoginReadsNothingOfThePipeItIsHanded(t *testing.T) {
	t.Parallel()
	env, home, path := loginEnvironment(t)
	// What a caller who thought the dialogue could be answered from a script would have written into it.
	const dialogue = "http://h\n" + token + "\n"
	read, write, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = read.Close() })
	_, err = io.WriteString(write, dialogue)
	require.NoError(t, err)
	require.NoError(t, write.Close())

	got := runOn(t, read, env, "auth", "login")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assertNoToken(t, got, token)
	unread, err := io.ReadAll(read)
	require.NoError(t, err)
	assert.Equal(t, dialogue, string(unread))
	assert.NoFileExists(t, path)
	assert.Empty(t, entries(t, home))
}

func TestAuthLoginRefusesACallThatDoesNotAssemble(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an argument", argv: []string{"auth", "login", "extra"}},
		{name: "an argument after the flag", argv: []string{"auth", "login", "--global", "extra"}},
		{name: "a flag for the token", argv: []string{"auth", "login", "--token", token}},
		{name: "a flag for the token after =", argv: []string{"auth", "login", "--token=" + token}},
		{name: "a flag for the address", argv: []string{"auth", "login", "--base-url", "http://h"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, home, path := loginEnvironment(t)

			got := runWith(t, env, tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assertNoToken(t, got, token)
			assert.NoFileExists(t, path)
			assert.Empty(t, entries(t, home))
		})
	}
}
