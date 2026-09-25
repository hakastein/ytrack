package youtrack_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const noMetadataCache = ""

func client(t *testing.T, server *fake.Server) *youtrack.Client {
	t.Helper()
	return youtrack.New(server.Address(t), fake.Token, noMetadataCache)
}

func refusal(t *testing.T, fault *diag.Fault) diag.Fault {
	t.Helper()
	require.NotNil(t, fault, "nothing was refused")
	assert.NotEmpty(t, fault.Message)
	kept := *fault
	kept.Message = ""
	return kept
}
