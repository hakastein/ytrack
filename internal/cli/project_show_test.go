package cli_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/cli"
)

const projectDEV = `{"name":"DEVELOPMENT","plugins":{"timeTrackingSettings":{"workItemTypes":[` +
	`{"name":"First","$type":"WorkItemType"},{"name":"Second","$type":"WorkItemType"}],` +
	`"enabled":true,"$type":"ProjectTimeTrackingSettings"},"$type":"ProjectPlugins"},"$type":"Project","shortName":"DEV"}`

func lookedIn(places ...any) detail {
	return detail{"looked_in", places}
}

func TestProjectShowRefusesWithoutAToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		token   []string
	}{
		{name: "no token", address: "http://127.0.0.1:%s"},
		{name: "an empty token", address: "http://127.0.0.1:%s", token: []string{"YTRACK_TOKEN="}},
		{name: "an address with a password", address: "http://svc:secret@127.0.0.1:%s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			listening, err := url.Parse(server.URL)
			require.NoError(t, err)
			env := append([]string{"YTRACK_URL=" + fmt.Sprintf(tc.address, listening.Port())}, tc.token...)

			got := runWith(t, env, "project", "show", "DEV")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.NotContains(t, got.stderr, "secret")
			assertNoToken(t, got, fake.Token)
			assert.Empty(t, server.Requests())
		})
	}
}

func TestProjectRefusesACallForTheFaultCheckedFirst(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		env  []string
		want faultDocument
	}{
		{
			name: "a limit of no records, no address and no token",
			argv: []string{"project", "list", "--limit", "0"},
			want: faultDocument{code: "bad_usage"},
		},
		{
			name: "no address and no token",
			argv: []string{"project", "show", "DEV"},
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}},
		},
		{
			name: "an empty address and an empty token",
			argv: []string{"project", "show", "DEV"},
			env:  []string{"YTRACK_URL=", "YTRACK_TOKEN="},
			want: faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := runWith(t, tc.env, tc.argv...)

			assert.Equal(t, tc.want, requireFault(t, got))
			assertNoToken(t, got, fake.Token)
		})
	}
}

func TestProjectShowRefusesWhenStdoutFails(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectDEV))
	var stderr strings.Builder

	code := cli.Run(t.Context(), showDEV, envOf(server), nil, nil, failingWriter{}, &stderr)

	got := outcome{code: code, stderr: stderr.String()}
	assert.Equal(t, faultDocument{code: "upstream_failed"}, requireFault(t, got))
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("stdout is closed")
}
