package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/youtrack"
)

func TestListUsersSendsTheSearchAsWrittenWithThePageAndTheCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "an empty search", search: ""},
		{name: "spaces around a name", search: "  First Last  "},
		{name: "characters a query escapes", search: "a&b=c+d%#"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"$type":"User","id":"1-1","login":"first"}]`))
			call, fault := youtrack.ListUsers(tc.search, "login", youtrack.Page{Limit: 1})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, []string{tc.search}, server.Request(t, 0).URL.Query()["query"])
			assert.Equal(t, []string{tc.search}, server.Request(t, 1).URL.Query()["query"])
		})
	}
}
