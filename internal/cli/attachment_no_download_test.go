package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// base64Content is the file's own bytes, which ytrack never downloads, so --fields is refused wherever it
// names that field, before any request goes out.
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
			name: "in a record of a selection",
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
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, found.details)
			assert.Empty(t, server.requests())
		})
	}
}
