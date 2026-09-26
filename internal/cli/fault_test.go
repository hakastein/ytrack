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

func showRequest(address string) string {
	return "GET " + address + "/api/admin/projects/DEV?fields=shortName"
}

func TestAFaultPrintsTheErrorOfYouTrackWithEveryDetail(t *testing.T) {
	t.Parallel()
	const failed = `{"error":"server_error","error_description":"java.lang.NullPointerException",` +
		`"error_developer_message":"at jetbrains.gap"}`
	server := fake.Serve(t, fake.JSON(http.StatusInternalServerError, failed))

	got := runWith(t, envOf(server), showDEV...)

	want := faultDocument{
		code: "upstream_failed",
		details: []detail{
			{"request", showRequest(server.URL)},
			{"upstream_status", 500},
			{"upstream_error", "server_error"},
			{"upstream_message", "java.lang.NullPointerException"},
			{"upstream_body", failed},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
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
