package cli_test

import (
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestTagRefusesACallWithNoName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a creation with no name", argv: []string{"tag", "create"}},
		{name: "a deletion with no name", argv: []string{"tag", "delete"}},
		{name: "a tagging with no name", argv: []string{"tag", "add", "DEV-7"}},
		{name: "a removal with no name", argv: []string{"tag", "remove", "DEV-7"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}
