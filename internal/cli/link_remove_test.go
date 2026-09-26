package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkRemovePrintsTheRemovedLink(t *testing.T) {
	t.Parallel()
	server := linking(t, deletionDone())

	got := runWith(t, envOf(server), "link", "remove", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\nremoved:\n  \"needs\":\n    - {idReadable: \"DEV-2\"}\n"}, got)
	assert.Contains(t, server.Routes(), "DELETE /api/issues/DEV-1/links/5-1t/issues/3-2")
}
