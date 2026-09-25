//go:build contract

package cli_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

const cassetteMode = recorder.ModeRecordOnly

func devTokens(t *testing.T) devInstanceTokens {
	t.Helper()
	state := os.Getenv("YTRACK_DEV_STATE")
	require.NotEmpty(t, state, "YTRACK_DEV_STATE is not set")
	return devInstanceTokens{
		admin:   readToken(t, state, "admin-token"),
		limited: readToken(t, state, "limited-token"),
		member:  readToken(t, state, "member-token"),
	}
}

func readToken(t *testing.T, state, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, name))
	require.NoError(t, err)
	secret := strings.TrimSpace(string(content))
	require.NotEmpty(t, secret, "%s is empty", filepath.Join(state, name))
	return secret
}

func realTransport(*testing.T) http.RoundTripper {
	return http.DefaultTransport
}
