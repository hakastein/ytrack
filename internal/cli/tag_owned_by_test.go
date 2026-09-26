package cli_test

import (
	"net/http"
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

func TestTagHandsTheOwnerToTheCall(t *testing.T) {
	t.Parallel()
	owner := fake.JSON(http.StatusOK, issueNamed("DEV-7"))
	tests := []struct {
		name    string
		argv    []string
		server  func(t *testing.T) *fake.Server
		printed string
	}{
		{
			name: "a deletion",
			argv: []string{"tag", "delete", "--name", "Shared", "--owned-by", "second"},
			server: func(t *testing.T) *fake.Server {
				return resolvingTags(t, tagsOfTwoOwners(), deletionDone())
			},
			printed: "name: \"Shared\"\nowner:\n  login: \"second\"\n",
		},
		{
			name: "a tagging",
			argv: []string{"tag", "add", "DEV-7", "--name", "Shared", "--owned-by", "second"},
			server: func(t *testing.T) *fake.Server {
				return addingATag(t, owner, shownTags(), fake.JSON(http.StatusOK, catalogueTag("10-7", "Shared", "second")))
			},
			printed: "idReadable: \"DEV-7\"\nadded:\n  name: \"Shared\"\n  owner:\n    login: \"second\"\n",
		},
		{
			name: "a removal",
			argv: []string{"tag", "remove", "DEV-7", "--name", "Shared", "--owned-by", "first"},
			server: func(t *testing.T) *fake.Server {
				return takingATagOff(t, owner, shownTags(), deletionDone())
			},
			printed: "idReadable: \"DEV-7\"\nremoved:\n  name: \"Shared\"\n  owner:\n    login: \"first\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, envOf(server), tc.argv...)

			assert.Equal(t, outcome{stdout: tc.printed}, got)
		})
	}
}
