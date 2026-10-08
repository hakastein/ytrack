package cli_test

import (
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestTagRefusesAnOwnerOfNoLogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a deletion", argv: []string{"tag", "delete", "--name", "Shared", "--owned-by", ""}},
		{name: "a tagging", argv: []string{"tag", "add", "DEV-7", "--name", "Shared", "--owned-by", ""}},
		{name: "a removal", argv: []string{"tag", "remove", "DEV-7", "--name", "Shared", "--owned-by", ""}},
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
