package cli_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		_, _ = io.WriteString(w, currentUser(user, user))
	})
}

func currentUser(login, fullName string) string {
	return fmt.Sprintf(`{"$type":"Me","id":"1-1","login":%q,"fullName":%q,"email":null,"banned":false}`, login, fullName)
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

func TestNoCommandTakesHalfALoginFromTheEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(asked *fake.Server) string
	}{
		{name: "an address and no token", set: func(asked *fake.Server) string { return "YTRACK_URL=" + asked.URL }},
		{name: "a token and no address", set: func(*fake.Server) string { return "YTRACK_TOKEN=" + fake.Token }},
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
		})
	}
}

func TestAuthStatusDoesNotReadTheFileWhenTheEnvironmentHasBothValues(t *testing.T) {
	t.Parallel()
	server := serveUserOfTheToken(t, map[string]string{fake.Token: envUser})
	home, _ := homeWith(t, "not a file of login records")

	got := runWith(t, append(envOf(server), "HOME="+home), "auth", "status")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, bearing(fake.Token), server.Request(t, 0).Header.Get("Authorization"))
}

func TestAuthStatusTakesTheRecordWhenTheVariablesAreEmpty(t *testing.T) {
	t.Parallel()
	server := serveUserOfTheToken(t, map[string]string{recordToken: recordUser})
	home, _ := homeWith(t, globalRecord(server.URL, recordToken))

	got := runWith(t, []string{"HOME=" + home, "YTRACK_URL=", "YTRACK_TOKEN="}, "auth", "status")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, bearing(recordToken), server.Request(t, 0).Header.Get("Authorization"))
}

func TestNoCommandUsesAFileOfLoginRecordsItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		records string
	}{
		{name: "not JSON", records: `[{"url":`},
		{name: "null rather than an array of records", records: "null"},
		{name: "a second value after the array", records: `[{"url":"http://h","token":"perm-x"}] []`},
		{name: "a record that is not an object", records: `[5]`},
		{name: "a key no record has", records: `[{"url":"http://h","token":"perm-x","expires":"never"}]`},
		{name: "no url", records: `[{"token":"perm-x"}]`},
		{name: "an empty token", records: `[{"url":"http://h","token":""}]`},
		{name: "a url of another scheme", records: `[{"url":"ftp://h","token":"perm-x"}]`},
		{name: "a token with a line ending", records: globalRecord("http://h", "perm-x\r")},
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
			assertNoToken(t, got, "perm-x")
			assertNoToken(t, got, "perm-y")
		})
	}
}

const tokenWithATab = "perm-ytrack-test\twith-a-tab"

func TestAuthStatusSendsATokenWithATabInside(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login func(t *testing.T, address string) []string
	}{
		{name: "from the environment", login: func(_ *testing.T, address string) []string {
			return []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + tokenWithATab}
		}},
		{name: "from a saved login", login: func(t *testing.T, address string) []string {
			home, _ := homeWith(t, globalRecord(address, tokenWithATab))
			return []string{"HOME=" + home}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveUserOfTheToken(t, map[string]string{tokenWithATab: recordUser})

			got := runWith(t, tc.login(t, server.URL), "auth", "status")

			assert.Equal(t, 0, got.code)
			assert.Equal(t, bearing(tokenWithATab), server.Request(t, 0).Header.Get("Authorization"))
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

func TestAuthStatusIsDeniedAFileTheSystemKeepsFromIt(t *testing.T) {
	t.Parallel()
	home, path := emptyHome(t)
	require.NoError(t, os.WriteFile(filepath.Dir(path), []byte("kept here by hand"), 0o600))

	got := runWith(t, []string{"HOME=" + home}, "auth", "status")

	want := faultDocument{code: "denied", details: []detail{directoryDetail(path)}}
	assert.Equal(t, want, requireFault(t, got))
}

func TestAuthLogoutIsDeniedTheRemovalOfTheLastRecord(t *testing.T) {
	t.Parallel()
	home, path := homeWith(t, globalRecord(fake.ServeNothing(t).URL, everywhereToken))
	withoutTheRightToWrite(t, filepath.Dir(path))

	got := runWith(t, []string{"HOME=" + home}, "auth", "logout", "--global")

	want := faultDocument{code: "denied", details: []detail{directoryDetail(path)}}
	assert.Equal(t, want, requireFault(t, got))
	assertNoRecordedToken(t, got)
	assert.FileExists(t, path)
}

func TestAuthLogoutIsDeniedWritingBackTheRecordsLeft(t *testing.T) {
	t.Parallel()
	scope := here(t)
	server := fake.ServeNothing(t)
	held := recordFile(scopedRecord(scope, server.URL, hereToken), unscopedRecord(server.URL, everywhereToken))
	home, path := homeWith(t, held)
	withoutTheRightToWrite(t, filepath.Dir(path))

	got := runWith(t, []string{"HOME=" + home}, "auth", "logout")

	want := faultDocument{code: "denied", details: []detail{directoryDetail(path)}}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, held, fileBytes(t, path))
	assertNoRecordedToken(t, got)
}

func TestNoCommandFindsALoginWithoutAFileToFindItIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		env      func(t *testing.T) []string
		lookedIn detail
	}{
		{
			name: "a home directory with no file in it",
			env: func(t *testing.T) []string {
				home, _ := emptyHome(t)
				return []string{"HOME=" + home}
			},
			lookedIn: lookedIn("YTRACK_URL", "YTRACK_TOKEN", "settings"),
		},
		{
			name:     "no home directory",
			env:      func(*testing.T) []string { return nil },
			lookedIn: lookedIn("YTRACK_URL", "YTRACK_TOKEN"),
		},
		{
			name:     "a home directory that is not an absolute path",
			env:      func(*testing.T) []string { return []string{"HOME=home"} },
			lookedIn: lookedIn("YTRACK_URL", "YTRACK_TOKEN"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, tc.env(t), "auth", "status")

			assert.Equal(t, faultDocument{code: "denied", details: []detail{tc.lookedIn}}, requireFault(t, got))
		})
	}
}

func TestProjectShowGoesToTheAddressOfTheGlobalRecordWithItsToken(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectDEV))
	home, _ := homeWith(t, globalRecord(server.URL, recordToken))

	got := runWith(t, []string{"HOME=" + home}, showDEV...)

	assert.Equal(t, 0, got.code)
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, server.Paths())
	assert.Equal(t, bearing(recordToken), server.Request(t, 0).Header.Get("Authorization"))
}

func TestNoCommandPrintsThePasswordOfAnAddressItCannotUse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
	}{
		{name: "another scheme", address: "ftp://svc:secret@h"},
		{name: "a query", address: "http://svc:secret@h/?q=1"},
		{name: "an escape that does not parse", address: "http://svc:secret@h/%zz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runWith(t, []string{"YTRACK_URL=" + tc.address, "YTRACK_TOKEN=" + fake.Token}, "auth", "status")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.NotContains(t, got.stderr, "secret")
		})
	}
}
