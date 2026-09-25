package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func TestShowProjectTakesWhitespaceAroundTheAnswer(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, " \t\r\n"+`{"$type":"Project","shortName":"DEV"}`+" \t\r\n"))
	call, fault := youtrack.ShowProject("DEV", "shortName")
	require.Nil(t, fault)

	node, fault := call(t.Context(), client(t, server))

	require.Nil(t, fault)
	assert.Equal(t, render.NewMap(render.Pair{Key: "shortName", Value: render.NewString("DEV")}), node)
}
