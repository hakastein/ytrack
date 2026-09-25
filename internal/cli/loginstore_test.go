package cli_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const recordToken = "perm-ytrack-test-record"

const (
	envUser    = "from.env"
	recordUser = "from.record"
)

func homeWith(t *testing.T, records string) (home, path string) {
	t.Helper()
	home, path = emptyHome(t)
	require.NoError(t, os.Mkdir(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(records), 0o600))
	return home, path
}

func emptyHome(t *testing.T) (home, path string) {
	t.Helper()
	home = t.TempDir()
	return home, filepath.Join(home, ".ytrack", "auth.json")
}

func recordFile(records ...string) string {
	return "[" + strings.Join(records, ",") + "]"
}

func unscopedRecord(address, secret string) string {
	return fmt.Sprintf(`{"url":%q,"token":%q}`, address, secret)
}

func scopedRecord(scope, address, secret string) string {
	return fmt.Sprintf(`{"scope":%q,"url":%q,"token":%q}`, scope, address, secret)
}

func globalRecord(address, secret string) string {
	return recordFile(unscopedRecord(address, secret))
}

func serveUserOfTheToken(t *testing.T, users map[string]string) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		user, known := users[secret]
		if !assert.True(t, known, "the server was sent a token no scenario handed out") {
			http.Error(w, "unknown token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"login":%q,"fullName":%q,"$type":"Me"}`, user, user))
	})
}

func status(address, from, login, fullName string) string {
	return fmt.Sprintf("url: %q\nauth_from: %q\nuser:\n  login: %q\n  fullName: %q\n", address, from, login, fullName)
}

func fileDetail(path string) detail {
	return detail{"file", path}
}

func directoryDetail(path string) detail {
	return detail{"directory", filepath.Dir(path)}
}

func TestAuthStatusTakesBothValuesFromTheGlobalRecord(t *testing.T) {
	t.Parallel()
	server := serveUserOfTheToken(t, map[string]string{recordToken: recordUser})
	home, _ := homeWith(t, globalRecord(server.URL, recordToken))

	got := runWith(t, []string{"HOME=" + home}, "auth", "status")

	want := status(server.URL, "settings", recordUser, recordUser)
	assert.Equal(t, outcome{stdout: want}, got)
	assertNoToken(t, got, recordToken)
}

func TestNoCommandTakesHalfALoginFromTheEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(asked *fake.Server) string
	}{
		{name: "an address and no token", set: func(asked *fake.Server) string { return "YTRACK_URL=" + asked.URL }},
		{name: "a fake.Token and no address", set: func(*fake.Server) string { return "YTRACK_TOKEN=" + fake.Token }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			asked, recorded := fake.ServeNothing(t), fake.ServeNothing(t)
			home, _ := homeWith(t, globalRecord(recorded.URL, recordToken))

			got := runWith(t, []string{"HOME=" + home, tc.set(asked)}, "auth", "status")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoToken(t, got, recordToken)
			assertNoToken(t, got, fake.Token)
			assert.Empty(t, asked.Requests())
			assert.Empty(t, recorded.Requests())
		})
	}
}

func TestAuthStatusDoesNotReadTheFileWhenTheEnvironmentHasBothValues(t *testing.T) {
	t.Parallel()
	server := serveUserOfTheToken(t, map[string]string{fake.Token: envUser})
	home, _ := homeWith(t, "not a file of login records")

	got := runWith(t, []string{"HOME=" + home, "YTRACK_URL=" + server.URL, "YTRACK_TOKEN=" + fake.Token}, "auth", "status")

	want := status(server.URL, "environment", envUser, envUser)
	assert.Equal(t, outcome{stdout: want}, got)
	assertNoToken(t, got, fake.Token)
}

func TestNoCommandUsesAFileOfLoginRecordsItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		records string
	}{
		{name: "not JSON", records: `[{"url":`},
		{name: "one record rather than an array of them", records: `{"url":"http://h","token":"perm-x"}`},
		{name: "null rather than an array of records", records: "null"},
		{name: "a second value after the array", records: `[{"url":"http://h","token":"perm-x"}] []`},
		{name: "a record that is not an object", records: `[5]`},
		{name: "a key no record has", records: `[{"url":"http://h","token":"perm-x","expires":"never"}]`},
		{name: "a url that is not a string", records: `[{"url":1,"token":"perm-x"}]`},
		{name: "no url", records: `[{"token":"perm-x"}]`},
		{name: "an empty url", records: `[{"url":"","token":"perm-x"}]`},
		{name: "an empty token", records: `[{"url":"http://h","token":""}]`},
		{name: "a url of another scheme", records: `[{"url":"ftp://h","token":"perm-x"}]`},
		{
			name:    "two records without a scope",
			records: `[{"url":"http://h","token":"perm-x"},{"url":"http://g","token":"perm-y"}]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home, path := homeWith(t, tc.records)

			got := runWith(t, []string{"HOME=" + home}, "auth", "status")

			want := faultDocument{code: "bad_usage", details: []detail{fileDetail(path)}}
			assert.Equal(t, want, requireFault(t, got))
		})
	}
}

func withoutTheRightToWrite(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes into a directory whatever its mode says")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	require.NoError(t, os.Chmod(dir, 0o500))
}

func TestNoCommandMendsAFileOfLoginRecordsTheSystemKeepsFromIt(t *testing.T) {
	t.Parallel()
	t.Run("a .ytrack that is a file rather than a directory", func(t *testing.T) {
		t.Parallel()
		home, path := emptyHome(t)
		require.NoError(t, os.WriteFile(filepath.Dir(path), []byte("kept here by hand"), 0o600))

		got := runWith(t, []string{"HOME=" + home}, "auth", "status")

		want := faultDocument{code: "denied", details: []detail{directoryDetail(path)}}
		assert.Equal(t, want, requireFault(t, got))
	})
	t.Run("a directory the last record cannot be taken out of", func(t *testing.T) {
		t.Parallel()
		server := fake.ServeNothing(t)
		home, path := homeWith(t, globalRecord(server.URL, everywhereToken))
		withoutTheRightToWrite(t, filepath.Dir(path))

		got := runWith(t, []string{"HOME=" + home}, "auth", "logout", "--global")

		want := faultDocument{code: "denied", details: []detail{directoryDetail(path)}}
		assert.Equal(t, want, requireFault(t, got))
		assertNoRecordedToken(t, got)
		assert.FileExists(t, path)
	})
	t.Run("a directory the records left cannot be written back into", func(t *testing.T) {
		t.Parallel()
		stated, scope := here(t)
		server := fake.ServeNothing(t)
		held := recordFile(scopedRecord(scope, server.URL, hereToken), unscopedRecord(server.URL, everywhereToken))
		home, path := homeWith(t, held)
		withoutTheRightToWrite(t, filepath.Dir(path))

		got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

		found := requireFault(t, got)
		assert.Equal(t, "denied", found.code)
		assert.Equal(t, []detail{directoryDetail(path)}, found.details)
		assert.Equal(t, held, fileBytes(t, path))
		assertNoRecordedToken(t, got)
	})
}

func TestNoCommandSendsATokenARecordCannotCarry(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)
	home, path := homeWith(t, globalRecord(server.URL, recordToken+"\r"))

	got := runWith(t, []string{"HOME=" + home}, "auth", "status")

	assert.Equal(t, faultDocument{code: "bad_usage", details: []detail{fileDetail(path)}}, requireFault(t, got))
	assertNoToken(t, got, recordToken)
	assert.Empty(t, server.Requests())
}

func TestNoCommandFindsAValueWithoutAFileToFindItIn(t *testing.T) {
	t.Parallel()
	t.Run("a home directory with no file in it", func(t *testing.T) {
		t.Parallel()
		home, _ := emptyHome(t)

		got := runWith(t, []string{"HOME=" + home}, "auth", "status")

		want := faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN", "settings")}}
		assert.Equal(t, want, requireFault(t, got))
	})
	t.Run("no home directory", func(t *testing.T) {
		t.Parallel()

		got := runWith(t, nil, "auth", "status")

		want := faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}}
		assert.Equal(t, want, requireFault(t, got))
	})
	t.Run("a home directory that is not an absolute path", func(t *testing.T) {
		t.Parallel()

		got := runWith(t, []string{"HOME=home"}, "auth", "status")

		want := faultDocument{code: "denied", details: []detail{lookedIn("YTRACK_URL", "YTRACK_TOKEN")}}
		assert.Equal(t, want, requireFault(t, got))
	})
}

func TestAuthStatusTakesTheRecordWhenTheVariablesAreEmpty(t *testing.T) {
	t.Parallel()
	server := serveUserOfTheToken(t, map[string]string{recordToken: recordUser})
	home, _ := homeWith(t, globalRecord(server.URL, recordToken))

	got := runWith(t, []string{"HOME=" + home, "YTRACK_URL=", "YTRACK_TOKEN="}, "auth", "status")

	want := status(server.URL, "settings", recordUser, recordUser)
	assert.Equal(t, outcome{stdout: want}, got)
	assertNoToken(t, got, recordToken)
}

func TestProjectShowGoesToTheAddressOfTheGlobalRecordWithItsToken(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectDEV))
	home, _ := homeWith(t, globalRecord(server.URL, recordToken))

	got := runWith(t, []string{"HOME=" + home}, "project", "show", "DEV")

	assert.Equal(t, outcome{stdout: printedDEV}, got)
	assertNoToken(t, got, recordToken)
	requests := server.Requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "/api/admin/projects/DEV", requests[0].URL.Path)
	assert.Equal(t, "Bearer "+recordToken, requests[0].Header.Get("Authorization"))
}

func TestNoCommandPrintsThePasswordOfAnAddressItCannotUse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		inRecord bool
		address  string
	}{
		{name: "another scheme in the environment", address: "ftp://svc:secret@h"},
		{name: "a query in the environment", address: "http://svc:secret@h/?q=1"},
		{name: "an escape that does not parse in the environment", address: "http://svc:secret@h/%zz"},
		{name: "another scheme in a record", inRecord: true, address: "ftp://svc:secret@h"},
		{name: "an escape that does not parse in a record", inRecord: true, address: "http://svc:secret@h/%zz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var env []string
			var want faultDocument
			if tc.inRecord {
				home, path := homeWith(t, globalRecord(tc.address, recordToken))
				env = []string{"HOME=" + home}
				want = faultDocument{code: "bad_usage", details: []detail{fileDetail(path)}}
			} else {
				env = []string{"YTRACK_URL=" + tc.address, "YTRACK_TOKEN=" + fake.Token}
				want = faultDocument{code: "bad_usage"}
			}

			got := runWith(t, env, "auth", "status")

			assert.Equal(t, want, requireFault(t, got))
			assert.NotContains(t, got.stdout, "secret")
			assert.NotContains(t, got.stderr, "secret")
		})
	}
}
