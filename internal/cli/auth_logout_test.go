package cli_test

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logoutDocument is what auth logout prints: the login it took out, named by where it was held.
func logoutDocument(address, scope string) string {
	return fmt.Sprintf("url: %q\nscope: %q\n", address, scope)
}

// savedFile is the file as auth logout leaves it: the records it kept, each in the bytes the file held, the one
// for everywhere first and the scoped ones by their scope.
func savedFile(records ...string) string {
	return "[\n  " + strings.Join(records, ",\n  ") + "\n]\n"
}

func fileBytes(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func entries(t *testing.T, path string) []string {
	t.Helper()
	held, err := os.ReadDir(path)
	require.NoError(t, err)
	names := make([]string, 0, len(held))
	for _, entry := range held {
		names = append(names, entry.Name())
	}
	return names
}

func TestAuthLogoutTakesOutTheRecordOfTheDirectoryItWasCalledIn(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	above := filepath.Dir(scope)
	server := serveNothing(t)
	everywhere := unscopedRecord(server.url, everywhereToken)
	parent := scopedRecord(above, server.url, aboveToken)
	home, path := homeWith(t, recordFile(parent, scopedRecord(scope, server.url, hereToken), everywhere))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, outcome{stdout: logoutDocument(server.url, scope)}, got)
	assertNoRecordedToken(t, got)
	assert.Equal(t, savedFile(everywhere, parent), fileBytes(t, path))
	assert.Empty(t, server.requests())
}

func TestAuthLogoutGlobalTakesOutTheRecordForEverywhereAlone(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name string
		env  func(home string) []string
	}{
		{
			name: "called where a record of a directory is held",
			env:  func(home string) []string { return []string{"HOME=" + home, "PWD=" + stated} },
		},
		// --global names the record itself, so which directory the call is in is nothing it has to work out.
		{
			name: "called with no PWD at all",
			env:  func(home string) []string { return []string{"HOME=" + home} },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			held := scopedRecord(scope, server.url, hereToken)
			home, path := homeWith(t, recordFile(held, unscopedRecord(server.url, everywhereToken)))

			got := runWith(t, tc.env(home), "auth", "logout", "--global")

			assert.Equal(t, outcome{stdout: logoutDocument(server.url, "global")}, got)
			assertNoRecordedToken(t, got)
			assert.Equal(t, savedFile(held), fileBytes(t, path))
			assert.Empty(t, server.requests())
		})
	}
}

func TestAuthLogoutTakesTheFileAwayWithTheLastRecord(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)
	home, path := homeWith(t, globalRecord(server.url, everywhereToken))

	got := runWith(t, []string{"HOME=" + home}, "auth", "logout", "--global")

	assert.Equal(t, outcome{stdout: logoutDocument(server.url, "global")}, got)
	assertNoRecordedToken(t, got)
	assert.NoFileExists(t, path)
	// The directory outlives the records it held: the metadata of projects is cached in it. Nothing else is left there,
	// the file a rewrite renames from included.
	assert.Empty(t, entries(t, filepath.Dir(path)))
	assert.Equal(t, []string{".ytrack"}, entries(t, home))
	assert.Empty(t, server.requests())
}

// A logout that changed nothing would leave the caller sure they had logged out while the token of another record
// goes on being sent from here.
func TestAuthLogoutRefusesWhereTheDirectoryHasNoRecordOfItsOwn(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	above := filepath.Dir(scope)
	tests := []struct {
		name    string
		records func(address string) []string
	}{
		{
			name:    "a record for the parent of the directory",
			records: func(address string) []string { return []string{scopedRecord(above, address, aboveToken)} },
		},
		{
			name:    "a record for everywhere",
			records: func(address string) []string { return []string{unscopedRecord(address, everywhereToken)} },
		},
		{
			name:    "a record for a directory of its own",
			records: func(address string) []string { return []string{scopedRecord("/nowhere/near/here", address, hereToken)} },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			held := recordFile(tc.records(server.url)...)
			home, path := homeWith(t, held)

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

			want := refusal{code: "bad_usage"}
			assert.Equal(t, want, requireRefusal(t, got))
			assertNoRecordedToken(t, got)
			assert.Equal(t, held, fileBytes(t, path))
			assert.Empty(t, server.requests())
		})
	}
}

func TestAuthLogoutGlobalRefusesWithoutARecordForEverywhere(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveNothing(t)
	held := recordFile(scopedRecord(scope, server.url, hereToken))
	home, path := homeWith(t, held)

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout", "--global")

	want := refusal{code: "bad_usage"}
	assert.Equal(t, want, requireRefusal(t, got))
	assertNoRecordedToken(t, got)
	assert.Equal(t, held, fileBytes(t, path))
	assert.Empty(t, server.requests())
}

func TestAuthLogoutRefusesWithoutAFileOrADirectoryToTakeARecordFrom(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	t.Run("a home directory with no file in it", func(t *testing.T) {
		t.Parallel()
		home, path := emptyHome(t)

		got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

		want := refusal{code: "bad_usage"}
		assert.Equal(t, want, requireRefusal(t, got))
		assert.NoFileExists(t, path)
		assert.Empty(t, entries(t, home))
	})
	t.Run("no home directory", func(t *testing.T) {
		t.Parallel()

		got := runWith(t, []string{"PWD=" + stated}, "auth", "logout")

		assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
	})
	t.Run("a PWD naming a directory the call is not in", func(t *testing.T) {
		t.Parallel()
		stale := t.TempDir()
		server := serveNothing(t)
		held := recordFile(scopedRecord(scope, server.url, hereToken))
		home, path := homeWith(t, held)

		got := runWith(t, []string{"HOME=" + home, "PWD=" + stale}, "auth", "logout")

		assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
		assertNoRecordedToken(t, got)
		assert.Equal(t, held, fileBytes(t, path))
	})
}

// JSON writes one string many ways, and a record nobody asked about comes back in the bytes its writer chose
// rather than in the ones a JSON writer would have picked for the same string.
func TestAuthLogoutLeavesEveryOtherRecordByteForByte(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveNothing(t)
	const oddToken = "perm-ytrack-test-odd"
	const nearToken = "perm-ytrack-test-near"
	odd := `{"scope":"\/tmp\/а \"b\"\nк","url":"http://h","token":"` + oddToken + `"}`
	near := `{ "url" : "http://g", "scope" : "/aaa", "token" : "` + nearToken + `" }`
	everywhere := unscopedRecord(server.url, everywhereToken)
	home, path := homeWith(t, recordFile(odd, scopedRecord(scope, server.url, hereToken), near, everywhere))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, outcome{stdout: logoutDocument(server.url, scope)}, got)
	assertNoRecordedToken(t, got)
	assertNoToken(t, got, oddToken)
	assertNoToken(t, got, nearToken)
	assert.Equal(t, savedFile(everywhere, near, odd), fileBytes(t, path))
	assert.Empty(t, server.requests())
}

// The address of the record taken out is printed back, and it may carry the password of a proxy in front of the
// instance; a logout run from a pipeline puts what it prints into the job's log.
func TestAuthLogoutPrintsTheAddressWithoutItsPassword(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveNothing(t)
	behind, err := url.Parse(server.url)
	require.NoError(t, err)
	behind.User = url.UserPassword("svc", "secret")
	home, path := homeWith(t, recordFile(scopedRecord(scope, behind.String(), hereToken)))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, outcome{stdout: logoutDocument("http://svc:xxxxx@"+behind.Host, scope)}, got)
	assert.NotContains(t, got.stdout, "secret")
	assertNoRecordedToken(t, got)
	assert.NoFileExists(t, path)
	assert.Empty(t, server.requests())
}

func TestAuthLogoutLeavesTheFileReadableByItsOwnerAlone(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveNothing(t)
	records := recordFile(scopedRecord(scope, server.url, hereToken), unscopedRecord(server.url, everywhereToken))
	home, path := homeWith(t, records)
	require.NoError(t, os.Chmod(path, 0o644))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	require.Equal(t, 0, got.code, "stderr: %q", got.stderr)
	held, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), held.Mode().Perm())
	assert.Empty(t, server.requests())
}

// Nothing of ytrack's is kept in the project a caller works in, and a logout made there is no exception.
func TestAuthLogoutWritesNothingIntoTheDirectoryItWasCalledIn(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveNothing(t)
	home, _ := homeWith(t, recordFile(scopedRecord(scope, server.url, hereToken)))
	before := entries(t, stated)

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	require.Equal(t, 0, got.code, "stderr: %q", got.stderr)
	assert.Equal(t, before, entries(t, stated))
}

func TestAuthLogoutUsesNoFileOfLoginRecordsItCannotRead(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	held := `[{"url":"http://h","token":"perm-x","expires":"never"}]`
	home, path := homeWith(t, held)

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	want := refusal{code: "bad_usage", details: []detail{fileDetail(path)}}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.Equal(t, held, fileBytes(t, path))
}

func TestAuthLogoutRefusesACallThatDoesNotAssemble(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name string
		argv []string
	}{
		{name: "an argument", argv: []string{"auth", "logout", "extra"}},
		{name: "an argument after the flag", argv: []string{"auth", "logout", "--global", "extra"}},
		{name: "a flag for the token", argv: []string{"auth", "logout", "--token", token}},
		{name: "a flag for the address", argv: []string{"auth", "logout", "--base-url", "http://h"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			held := recordFile(scopedRecord(scope, server.url, hereToken))
			home, path := homeWith(t, held)

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, tc.argv...)

			assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
			assertNoToken(t, got, token)
			assertNoRecordedToken(t, got)
			assert.Equal(t, held, fileBytes(t, path))
			assert.Empty(t, server.requests())
		})
	}
}
