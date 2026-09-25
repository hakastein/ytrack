package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestFieldsRefusesTheContentOfAFileWhereverItIsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{
			name: "under the attachments of an issue",
			argv: []string{"issue", "show", "DEV-1", "--fields", "attachments(base64Content)"},
		},
		{
			name: "beside a name of the default",
			argv: []string{"issue", "show", "DEV-1", "--fields", "+attachments(name,base64Content)"},
		},
		{
			name: "under the attachments of an article",
			argv: []string{"article", "show", "DEV-A-1", "--fields", "attachments(base64Content)"},
		},
		{
			name: "in a record of a list",
			argv: []string{"issue", "list", "--query", "x", "--fields", "+attachments(base64Content)"},
		},
		{
			name: "at a place that holds no file at all",
			argv: []string{"project", "show", "DEV", "--fields", "base64Content"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, found.details)
			assert.Empty(t, server.Requests())
		})
	}
}
