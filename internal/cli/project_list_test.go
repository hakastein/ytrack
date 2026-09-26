package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const listedDEV = `{"name":"DEVELOPMENT","$type":"Project","shortName":"DEV"}`

func TestProjectListPrintsTheProjects(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+listedDEV+`]`))

	got := runWith(t, envOf(server), "project", "list")

	want := "total: 1\nreturned: 1\ntruncated: false\nprojects:\n" + `  - {shortName: "DEV", name: "DEVELOPMENT"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	assert.Contains(t, server.Routes(), http.MethodGet+" /api/admin/projects")
}
