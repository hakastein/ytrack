package cli_test

import (
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestIssueWriteRefusesAFlagItCannotReadBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "create: a field with no =", argv: []string{"issue", "create", "DEV", "--summary", "x", "--field", "Type"}},
		{name: "update: a field with no =", argv: []string{"issue", "update", "DEV-1", "--field", "Type"}},
		{name: "create: an empty description", argv: []string{"issue", "create", "DEV", "--summary", "x", "--description", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}
