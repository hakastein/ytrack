package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A token for each record a scenario writes: the server answers as the user of the token it was sent, so the
// document says which record the call chose without printing any of them.
const (
	hereToken       = "perm-ytrack-test-here"
	aboveToken      = "perm-ytrack-test-above"
	everywhereToken = "perm-ytrack-test-everywhere"
)

const (
	hereUser       = "from.here"
	aboveUser      = "from.above"
	everywhereUser = "from.everywhere"
)

// here is the directory the test process runs in, as PWD states it and as a scope has to spell it.
func here(t *testing.T) (stated, scope string) {
	t.Helper()
	stated, err := os.Getwd()
	require.NoError(t, err)
	scope, err = filepath.EvalSymlinks(stated)
	require.NoError(t, err)
	return stated, scope
}

func serveTheRecordedUsers(t *testing.T) *upstream {
	t.Helper()
	return serveUserOfTheToken(t, map[string]string{
		hereToken:       hereUser,
		aboveToken:      aboveUser,
		everywhereToken: everywhereUser,
	})
}

func assertNoRecordedToken(t *testing.T, got outcome) {
	t.Helper()
	for _, secret := range []string{hereToken, aboveToken, everywhereToken} {
		assertNoToken(t, got, secret)
	}
}

// A record of a directory holds in it and under it, and the nearest one wins: cd internal/cli inside a project
// set up for the dev instance is not a step out to the global, production, record.
func TestAuthStatusTakesTheRecordOfTheNearestDirectory(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	above := filepath.Dir(scope)
	tests := []struct {
		name string
		// The records of the file, written in an order of their own; the nearest is neither first nor last.
		records func(address string) []string
		scope   string
		user    string
	}{
		{
			name: "a record for the directory of the call, one for its parent and a global one",
			records: func(address string) []string {
				return []string{
					scopedRecord(above, address, aboveToken),
					scopedRecord(scope, address, hereToken),
					unscopedRecord(address, everywhereToken),
				}
			},
			scope: scope,
			user:  hereUser,
		},
		{
			name: "a record for the parent of the directory and a global one",
			records: func(address string) []string {
				return []string{
					unscopedRecord(address, everywhereToken),
					scopedRecord(above, address, aboveToken),
				}
			},
			scope: above,
			user:  aboveUser,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveTheRecordedUsers(t)
			home, _ := homeWith(t, recordFile(tc.records(server.url)...))

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "status")

			want := status(server.url, "settings", tc.user, tc.user)
			assert.Equal(t, outcome{stdout: want}, got)
			assertNoRecordedToken(t, got)
		})
	}
}

// A scope is an ancestor by path components, not by the letters of the path.
func TestAuthStatusMatchesAScopeByWholePathComponents(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name  string
		held  string
		scope string
		user  string
	}{
		{
			name:  "a scope the directory begins with without being under it",
			held:  scope[:len(scope)-1],
			scope: "global",
			user:  everywhereUser,
		},
		{
			name:  "the root directory, which every directory is under",
			held:  string(filepath.Separator),
			scope: string(filepath.Separator),
			user:  hereUser,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveTheRecordedUsers(t)
			records := recordFile(unscopedRecord(server.url, everywhereToken), scopedRecord(tc.held, server.url, hereToken))
			home, _ := homeWith(t, records)

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "status")

			want := status(server.url, "settings", tc.user, tc.user)
			assert.Equal(t, outcome{stdout: want}, got)
			assertNoRecordedToken(t, got)
		})
	}
}

// A shell keeps PWD as the path it walked in, symlinks and all, while a record spells its directory physically.
func TestAuthStatusTakesTheRecordOfTheDirectoryASymlinkedPWDReaches(t *testing.T) {
	t.Parallel()
	_, scope := here(t)
	link := filepath.Join(t.TempDir(), "here")
	require.NoError(t, os.Symlink(scope, link))
	server := serveTheRecordedUsers(t)
	home, _ := homeWith(t, recordFile(scopedRecord(scope, server.url, hereToken)))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + link}, "auth", "status")

	want := status(server.url, "settings", hereUser, hereUser)
	assert.Equal(t, outcome{stdout: want}, got)
	assertNoRecordedToken(t, got)
}

// Which record applies cannot be guessed, and the global record is no answer: it names whichever instance the
// caller set up for everywhere else.
func TestNoCommandChoosesARecordWithoutKnowingWhichDirectoryItIsIn(t *testing.T) {
	t.Parallel()
	_, scope := here(t)
	tests := []struct {
		name string
		// The env entries beside HOME, so that a scenario can leave PWD out altogether.
		pwd func(elsewhere string) []string
	}{
		{name: "no PWD at all", pwd: func(string) []string { return nil }},
		{name: "a relative PWD", pwd: func(string) []string { return []string{"PWD=internal/cli"} }},
		{name: "a PWD naming a directory the call is not in", pwd: func(elsewhere string) []string { return []string{"PWD=" + elsewhere} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			elsewhere := t.TempDir()
			server := serveNothing(t)
			records := recordFile(scopedRecord(scope, server.url, hereToken), unscopedRecord(server.url, everywhereToken))
			home, _ := homeWith(t, records)

			got := runWith(t, append([]string{"HOME=" + home}, tc.pwd(elsewhere)...), "auth", "status")

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assertNoRecordedToken(t, got)
			assert.Empty(t, server.requests())
		})
	}
}

// Nothing in a file of global records alone is chosen by directory, so a caller whose runner left PWD behind is
// not stopped by it.
func TestAuthStatusAsksForNoDirectoryWithoutARecordThatNamesOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pwd  func(stale string) []string
	}{
		{name: "no PWD at all", pwd: func(string) []string { return nil }},
		{name: "a PWD naming a directory the call is not in", pwd: func(stale string) []string { return []string{"PWD=" + stale} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveTheRecordedUsers(t)
			home, _ := homeWith(t, recordFile(unscopedRecord(server.url, everywhereToken)))

			got := runWith(t, append([]string{"HOME=" + home}, tc.pwd(t.TempDir())...), "auth", "status")

			want := status(server.url, "settings", everywhereUser, everywhereUser)
			assert.Equal(t, outcome{stdout: want}, got)
			assertNoRecordedToken(t, got)
		})
	}
}

// A scope no directory can ever be spelled as, and a directory named twice, are mistakes in the file; the whole
// file is refused before any of it is matched against a directory, so no PWD is needed to see them.
func TestNoCommandUsesAFileWhoseScopeItCannotMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		records string
	}{
		{name: "a scope that is not an absolute path", records: recordFile(scopedRecord("work/ytrack", "http://h", "perm-x"))},
		{name: "a scope with a slash at the end", records: recordFile(scopedRecord("/work/ytrack/", "http://h", "perm-x"))},
		{name: "a scope that walks back up", records: recordFile(scopedRecord("/work/../srv", "http://h", "perm-x"))},
		{
			name: "two records for one directory",
			records: recordFile(
				scopedRecord("/work", "http://h", "perm-x"),
				scopedRecord("/work", "http://g", "perm-y"),
			),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home, path := homeWith(t, tc.records)

			got := runWith(t, []string{"HOME=" + home}, "auth", "status")

			want := refusal{code: "bad_usage", details: []detail{fileDetail(path)}}
			assert.Equal(t, want, requireRefusal(t, got))
			assertNoToken(t, got, "perm-x")
			assertNoToken(t, got, "perm-y")
		})
	}
}
