package cli_test

import (
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestEveryCommandRefusesACallItCannotSendBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "comment update: no text", argv: []string{"comment", "update", "DEV-1", "7-1"}},
		{name: "time create: a duration of days", argv: []string{"time", "create", "DEV-1", "P1D"}},
		{name: "article show: --comments neither all nor a count", argv: []string{"article", "show", "DEV-A-1", "--comments=-1"}},
		{name: "issue show: --comments neither all nor a count", argv: []string{"issue", "show", "DEV-1", "--comments=every"}},
		{name: "comment create: no text", argv: []string{"comment", "create", "DEV-1"}},
		{name: "issue create: no title", argv: []string{"issue", "create", "DEV"}},
		{name: "article list: a parent and a search together", argv: []string{"article", "list", "--parent", "DEV-A-1", "--query", ""}},
		{name: "article list: no search", argv: []string{"article", "list"}},
		{name: "user list: no search", argv: []string{"user", "list"}},
		{name: "project list: a limit wider than 32 bits", argv: []string{"project", "list", "--limit", "3000000000"}},
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
