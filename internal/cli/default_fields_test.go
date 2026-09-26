package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectShowSendsTheTokenOfTheEnvironment(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectDEV))

	got := runWith(t, envOf(server), "project", "show", "DEV")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "Bearer "+fake.Token, server.Last(t).Header.Get("Authorization"))
}
