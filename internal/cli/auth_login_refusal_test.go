package cli_test

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const damagedRecords = "not a file of login records"

func loginEnvironment(t *testing.T) (env []string, path string) {
	t.Helper()
	stated, _ := here(t)
	home, path := homeWith(t, damagedRecords)
	return append(fake.ServeNothing(t).Env(), "HOME="+home, "PWD="+stated), path
}

func TestAuthLoginRefusesAStdinThatIsNotATerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stdin func(t *testing.T) *os.File
	}{
		{
			name:  "no stdin at all",
			stdin: func(*testing.T) *os.File { return nil },
		},
		{
			name: "a stdin that holds nothing",
			stdin: func(t *testing.T) *os.File {
				t.Helper()
				empty, err := os.Open(os.DevNull)
				require.NoError(t, err)
				t.Cleanup(func() { _ = empty.Close() })
				return empty
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, path := loginEnvironment(t)

			got := runOn(t, tc.stdin(t), env, "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Equal(t, damagedRecords, fileBytes(t, path))
		})
	}
}

func TestAuthLoginReadsNothingOfThePipeItIsHanded(t *testing.T) {
	t.Parallel()
	env, path := loginEnvironment(t)
	const dialogue = "http://h\n" + fake.Token + "\n"
	read, write, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = read.Close() })
	_, err = io.WriteString(write, dialogue)
	require.NoError(t, err)
	require.NoError(t, write.Close())

	got := runOn(t, read, env, "auth", "login")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	unread, err := io.ReadAll(read)
	require.NoError(t, err)
	assert.Equal(t, dialogue, string(unread))
	assert.Equal(t, damagedRecords, fileBytes(t, path))
}
