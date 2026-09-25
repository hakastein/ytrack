package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
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

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTagNarrowsTheNameByTheOwnerOfTheTag(t *testing.T) {
	t.Parallel()
	owner := fake.JSON(http.StatusOK, issueNamed("DEV-7"))
	tests := []struct {
		name    string
		argv    []string
		server  func(t *testing.T) *fake.Server
		methods []string
		paths   []string
	}{
		{
			name: "a deletion",
			argv: []string{"tag", "delete", "--name", "Shared", "--owned-by", "second"},
			server: func(t *testing.T) *fake.Server {
				return resolvingTags(t, tagsOfTwoOwners(), deletionDone())
			},
			methods: []string{http.MethodGet, http.MethodDelete},
			paths:   []string{tagsCollection, tagDeletionPath("10-7")},
		},
		{
			name: "a tagging",
			argv: []string{"tag", "add", "DEV-7", "--name", "Shared", "--owned-by", "second"},
			server: func(t *testing.T) *fake.Server {
				return addingATag(t, owner, shownTags(), fake.JSON(http.StatusOK, catalogueTag("10-7", "Shared", "second")))
			},
			methods: []string{http.MethodGet, http.MethodGet, http.MethodPost},
			paths:   []string{"/api/issues/DEV-7", tagsCollection, tagsOfOwnerPath("issues", "DEV-7")},
		},
		{
			name: "a removal",
			argv: []string{"tag", "remove", "DEV-7", "--name", "Shared", "--owned-by", "first"},
			server: func(t *testing.T) *fake.Server {
				return takingATagOff(t, owner, shownTags(), deletionDone())
			},
			methods: []string{http.MethodGet, http.MethodGet, http.MethodDelete},
			paths:   []string{"/api/issues/DEV-7", tagsCollection, tagOnOwnerPath("issues", "DEV-7", "10-6")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.Env(), tc.argv...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, tc.methods, server.Methods())
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}
