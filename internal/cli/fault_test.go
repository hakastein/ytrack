package cli_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/cli"
)

var showDEV = []string{"project", "show", "DEV", "--fields", "shortName"}

func TestAFaultPrintsTheDetailsOfTheErrorOfYouTrackWithTheirTypes(t *testing.T) {
	t.Parallel()
	const failed = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	server := fake.Serve(t, fake.JSON(http.StatusInternalServerError, failed))

	got := runWith(t, envOf(server), showDEV...)

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, 500, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, "java.lang.NullPointerException", detailNamed(t, found, "upstream_message"))
}

func TestProjectShowRefusesWhenNoResponseComesInTime(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	var stdout, stderr bytes.Buffer

	code := cli.Run(ctx, showDEV, envOf(server), nil, nil, &stdout, &stderr)

	got := outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
	assert.Equal(t, "upstream_failed", requireFault(t, got).code)
}
