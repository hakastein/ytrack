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

func TestProjectShowReachesTheAPIUnderThePathOfTheAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "a slash", path: "/", want: "/api/admin/projects/DEV"},
		{name: "a path", path: "/ctx", want: "/ctx/api/admin/projects/DEV"},
		{name: "a path ending in a slash", path: "/ctx/", want: "/ctx/api/admin/projects/DEV"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, projectDEV))

			got := runWith(t, []string{"YTRACK_URL=" + server.URL + tc.path, "YTRACK_TOKEN=" + fake.Token}, showDEV...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, []string{tc.want}, server.Paths())
		})
	}
}

func TestProjectShowRefusesAnAddressItCannotUse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
	}{
		{name: "no scheme", address: "localhost:8091"},
		{name: "another scheme", address: "ftp://h"},
		{name: "no host", address: "http:///ctx"},
		{name: "a broken escape", address: "http://h/%zz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := []string{"YTRACK_URL=" + tc.address, "YTRACK_TOKEN=" + fake.Token}

			got := runWith(t, env, "project", "show", "DEV")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoToken(t, got, fake.Token)
		})
	}
}

func TestProjectShowRefusesAnAddressWithAQueryOrAFragment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tail string
	}{
		{name: "a query", tail: "/?q=1"},
		{name: "a fragment", tail: "/#top"},
		{name: "an empty query", tail: "/?"},
		{name: "an empty fragment", tail: "/#"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			address := server.URL + tc.tail

			got := runWith(t, []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + fake.Token}, "project", "show", "DEV")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoToken(t, got, fake.Token)
		})
	}
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

func TestProjectShowRefusesATokenItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		token string
	}{
		{name: "the carriage return of a Windows line ending", token: fake.Token + "\r"},
		{name: "a line feed", token: fake.Token + "\n"},
		{name: "a delete", token: "\x7f" + fake.Token},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, []string{"YTRACK_URL=" + server.URL, "YTRACK_TOKEN=" + tc.token}, "project", "show", "DEV")

			want := faultDocument{code: "bad_usage"}
			assert.Equal(t, want, requireFault(t, got))
			assert.NotContains(t, got.stderr, fake.Token)
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
