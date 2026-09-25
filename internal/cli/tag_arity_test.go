package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The five verbs of tags take one shape each, and cobra settles that before ytrack runs anything, so
// no call of another shape costs a request. What the table is really for is the two pairs that would otherwise
// read as one another: a deletion given an owner is not a removal, and a removal given none is not a deletion.
// The owner is the one thing any of them takes as an argument, and every name goes in --name.
func TestTagRefusesEveryCallOfTheWrongShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "the group with no verb", argv: []string{"tag"}},
		{name: "an argument to the list", argv: []string{"tag", "list", "x"}},
		{name: "a creation with no name", argv: []string{"tag", "create"}},
		{name: "a creation given an argument", argv: []string{"tag", "create", "a", "--name", "b"}},
		{name: "a deletion with no name", argv: []string{"tag", "delete"}},
		{name: "a deletion given an owner", argv: []string{"tag", "delete", "DEV-7", "--name", "x"}},
		{name: "a tagging with no owner", argv: []string{"tag", "add", "--name", "x"}},
		{name: "a tagging with no name", argv: []string{"tag", "add", "DEV-7"}},
		{name: "a tagging given a second argument", argv: []string{"tag", "add", "DEV-7", "y", "--name", "x"}},
		{name: "a removal with no owner", argv: []string{"tag", "remove", "--name", "x"}},
		{name: "a removal with no name", argv: []string{"tag", "remove", "DEV-7"}},
		{name: "a removal given a second argument", argv: []string{"tag", "remove", "DEV-7", "y", "--name", "x"}},
		{
			name: "a deletion given a flag of the creation",
			argv: []string{"tag", "delete", "--name", "x", "--visible-for", "g"},
		},
		{
			name: "a removal given a flag of the list",
			argv: []string{"tag", "remove", "DEV-7", "--name", "x", "--limit", "5"},
		},
		{name: "a tagging given a flag no verb has", argv: []string{"tag", "add", "DEV-7", "--name", "x", "--yes"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}
