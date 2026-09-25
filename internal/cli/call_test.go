package cli_test

import (
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func TestEveryCommandRefusesACallItCannotSendBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "tag add: an empty name", argv: []string{"tag", "add", "DEV-7", "--name", ""}},
		{name: "field list: fields that are no expression", argv: []string{"field", "list", "DEV", "--fields", "field("}},
		{name: "link remove: an empty phrase", argv: []string{"link", "remove", "DEV-1", "", "DEV-2"}},
		{name: "user show: fields that do not parse", argv: []string{"user", "show", "first", "--fields", "+"}},
		{name: "activity list: a limit that leaves no room for the activity past it", argv: []string{"activity", "list", "DEV-1", "--limit", "2147483647"}},
		{name: "time list: a name under the duration", argv: []string{"time", "list", "DEV-1", "--fields", "duration(minutes)"}},
		{name: "attachment list: the content of a file", argv: []string{"attachment", "list", "DEV-1", "--fields", "+base64Content"}},
		{name: "link list: a name under a slot of a target issue", argv: []string{"link", "list", "DEV-1", "--fields", "+links(id)"}},
		{name: "article show: comments asked for in the expression", argv: []string{"article", "show", "DEV-A-1", "--fields", "+comments"}},
		{name: "issue show: comments asked for in the expression", argv: []string{"issue", "show", "DEV-1", "--fields", "+comments"}},
		{name: "link add: an empty phrase", argv: []string{"link", "add", "DEV-1", "", "DEV-2"}},
		{name: "field show: an empty name", argv: []string{"field", "show", "DEV", ""}},
		{name: "comment update: no text", argv: []string{"comment", "update", "DEV-1", "7-1"}},
		{name: "time create: a duration longer than the server keeps", argv: []string{"time", "create", "DEV-1", "PT2147483648M"}},
		{name: "tag delete: an empty name", argv: []string{"tag", "delete", "--name", ""}},
		{name: "tag create: an empty name", argv: []string{"tag", "create", "--name", ""}},
		{name: "tag list: fields that do not parse", argv: []string{"tag", "list", "--fields", "name,,owner"}},
		{name: "time update: nothing to write", argv: []string{"time", "update", "DEV-1", "199-6"}},
		{name: "article show: --comments neither all nor a count", argv: []string{"article", "show", "DEV-A-1", "--comments=-1"}},
		{name: "issue show: --comments neither all nor a count", argv: []string{"issue", "show", "DEV-1", "--comments=every"}},
		{name: "comment create: no text", argv: []string{"comment", "create", "DEV-1"}},
		{name: "issue create: no title", argv: []string{"issue", "create", "DEV"}},
		{name: "article update: nothing to write", argv: []string{"article", "update", "DEV-A-7"}},
		{name: "issue update: nothing to write", argv: []string{"issue", "update", "DEV-1"}},
		{name: "article list: a parent and a search together", argv: []string{"article", "list", "--parent", "DEV-A-1", "--query", ""}},
		{name: "issue list: comments asked for in the expression", argv: []string{"issue", "list", "--query", "", "--fields", "+comments"}},
		{name: "user list: fields that do not parse", argv: []string{"user", "list", "--query", "fir", "--fields", "+"}},
		{name: "article list: no search", argv: []string{"article", "list"}},
		{name: "user list: no search", argv: []string{"user", "list"}},
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
