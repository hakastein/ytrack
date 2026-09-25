package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestEveryCommandRefusesAnIDOfNoFormBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	attached := aFileToAttach(t)
	tests := []struct {
		name string
		argv []string
	}{
		{name: "issue show", argv: []string{"issue", "show", ".."}},
		{name: "issue create", argv: []string{"issue", "create", "..", "--summary", "x"}},
		{name: "issue update", argv: []string{"issue", "update", "..", "--summary", "x"}},
		{name: "issue delete", argv: []string{"issue", "delete", ".."}},
		{name: "article show", argv: []string{"article", "show", ".."}},
		{name: "article list --parent", argv: []string{"article", "list", "--parent", ".."}},
		{name: "article create", argv: []string{"article", "create", "..", "--summary", "x"}},
		{name: "article create --parent", argv: []string{"article", "create", "DEV", "--summary", "x", "--parent", ".."}},
		{name: "article update", argv: []string{"article", "update", "..", "--summary", "x"}},
		{name: "article update --parent", argv: []string{"article", "update", "DEV-A-1", "--parent", ".."}},
		{name: "article delete", argv: []string{"article", "delete", ".."}},
		{name: "comment list", argv: []string{"comment", "list", ".."}},
		{name: "comment create", argv: []string{"comment", "create", "..", "--text", "x"}},
		{name: "comment update, the owner", argv: []string{"comment", "update", "..", "7-1", "--text", "x"}},
		{name: "comment update, the comment", argv: []string{"comment", "update", "DEV-1", "..", "--text", "x"}},
		{name: "comment delete, the owner", argv: []string{"comment", "delete", "..", "7-1"}},
		{name: "comment delete, the comment", argv: []string{"comment", "delete", "DEV-1", ".."}},
		{name: "attachment list", argv: []string{"attachment", "list", ".."}},
		{name: "attachment create", argv: []string{"attachment", "create", "..", attached}},
		{name: "attachment delete, the owner", argv: []string{"attachment", "delete", "..", "12-1"}},
		{name: "attachment delete, the attachment", argv: []string{"attachment", "delete", "DEV-1", ".."}},
		{name: "tag add", argv: []string{"tag", "add", "..", "--name", "x"}},
		{name: "tag remove", argv: []string{"tag", "remove", "..", "--name", "x"}},
		{name: "link list", argv: []string{"link", "list", ".."}},
		{name: "link add, the source", argv: []string{"link", "add", "..", "needs", "DEV-2"}},
		{name: "link add, the target", argv: []string{"link", "add", "DEV-1", "needs", ".."}},
		{name: "link remove, the source", argv: []string{"link", "remove", "..", "needs", "DEV-2"}},
		{name: "link remove, the target", argv: []string{"link", "remove", "DEV-1", "needs", ".."}},
		{name: "time list", argv: []string{"time", "list", ".."}},
		{name: "time create", argv: []string{"time", "create", "..", "PT1H"}},
		{name: "time update, the issue", argv: []string{"time", "update", "..", "199-6", "--text", "x"}},
		{name: "time update, the work item", argv: []string{"time", "update", "DEV-1", "..", "--text", "x"}},
		{name: "time delete, the issue", argv: []string{"time", "delete", "..", "199-6"}},
		{name: "time delete, the work item", argv: []string{"time", "delete", "DEV-1", ".."}},
		{name: "activity list", argv: []string{"activity", "list", ".."}},
		{name: "project show", argv: []string{"project", "show", ".."}},
		{name: "field list", argv: []string{"field", "list", ".."}},
		{name: "field show", argv: []string{"field", "show", "..", "Type"}},
		{name: "user show", argv: []string{"user", "show", ".."}},
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
