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

	"github.com/hakastein/ytrack/internal/fake"
)

func logoutDocument(address, scope string) string {
	return fmt.Sprintf("url: %q\nscope: %q\n", address, scope)
}

func savedFile(recordsGlobalFirstThenByScope ...string) string {
	return "[\n  " + strings.Join(recordsGlobalFirstThenByScope, ",\n  ") + "\n]\n"
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
	server := fake.ServeNothing(t)
	everywhere := unscopedRecord(server.URL, everywhereToken)
	parent := scopedRecord(above, server.URL, aboveToken)
	home, path := homeWith(t, recordFile(parent, scopedRecord(scope, server.URL, hereToken), everywhere))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, outcome{stdout: logoutDocument(server.URL, scope)}, got)
	assertNoRecordedToken(t, got)
	assert.Equal(t, savedFile(everywhere, parent), fileBytes(t, path))
	assert.Empty(t, server.Requests())
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
		{
			name: "called with no PWD at all",
			env:  func(home string) []string { return []string{"HOME=" + home} },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			held := scopedRecord(scope, server.URL, hereToken)
			home, path := homeWith(t, recordFile(held, unscopedRecord(server.URL, everywhereToken)))

			got := runWith(t, tc.env(home), "auth", "logout", "--global")

			assert.Equal(t, outcome{stdout: logoutDocument(server.URL, "global")}, got)
			assertNoRecordedToken(t, got)
			assert.Equal(t, savedFile(held), fileBytes(t, path))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestAuthLogoutTakesTheFileAwayWithTheLastRecord(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)
	home, path := homeWith(t, globalRecord(server.URL, everywhereToken))

	got := runWith(t, []string{"HOME=" + home}, "auth", "logout", "--global")

	assert.Equal(t, outcome{stdout: logoutDocument(server.URL, "global")}, got)
	assertNoRecordedToken(t, got)
	assert.NoFileExists(t, path)
	assert.Empty(t, entries(t, filepath.Dir(path)))
	assert.Equal(t, []string{".ytrack"}, entries(t, home))
	assert.Empty(t, server.Requests())
}

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
			server := fake.ServeNothing(t)
			held := recordFile(tc.records(server.URL)...)
			home, path := homeWith(t, held)

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

			want := faultDocument{code: "bad_usage"}
			assert.Equal(t, want, requireFault(t, got))
			assertNoRecordedToken(t, got)
			assert.Equal(t, held, fileBytes(t, path))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestAuthLogoutGlobalRefusesWithoutARecordForEverywhere(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.ServeNothing(t)
	held := recordFile(scopedRecord(scope, server.URL, hereToken))
	home, path := homeWith(t, held)

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout", "--global")

	want := faultDocument{code: "bad_usage"}
	assert.Equal(t, want, requireFault(t, got))
	assertNoRecordedToken(t, got)
	assert.Equal(t, held, fileBytes(t, path))
	assert.Empty(t, server.Requests())
}

func TestAuthLogoutRefusesWithoutAFileOrADirectoryToTakeARecordFrom(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	t.Run("a home directory with no file in it", func(t *testing.T) {
		t.Parallel()
		home, path := emptyHome(t)

		got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

		want := faultDocument{code: "bad_usage"}
		assert.Equal(t, want, requireFault(t, got))
		assert.NoFileExists(t, path)
		assert.Empty(t, entries(t, home))
	})
	t.Run("no home directory", func(t *testing.T) {
		t.Parallel()

		got := runWith(t, []string{"PWD=" + stated}, "auth", "logout")

		assert.Equal(t, "bad_usage", requireFault(t, got).code)
	})
	t.Run("a PWD naming a directory the call is not in", func(t *testing.T) {
		t.Parallel()
		stale := t.TempDir()
		server := fake.ServeNothing(t)
		held := recordFile(scopedRecord(scope, server.URL, hereToken))
		home, path := homeWith(t, held)

		got := runWith(t, []string{"HOME=" + home, "PWD=" + stale}, "auth", "logout")

		assert.Equal(t, "bad_usage", requireFault(t, got).code)
		assertNoRecordedToken(t, got)
		assert.Equal(t, held, fileBytes(t, path))
	})
}

func TestAuthLogoutLeavesEveryOtherRecordByteForByte(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.ServeNothing(t)
	const oddToken = "perm-ytrack-test-odd"
	const nearToken = "perm-ytrack-test-near"
	odd := `{"scope":"\/tmp\/а \"b\"\nк","url":"http://h","token":"` + oddToken + `"}`
	near := `{ "url" : "http://g", "scope" : "/aaa", "token" : "` + nearToken + `" }`
	everywhere := unscopedRecord(server.URL, everywhereToken)
	home, path := homeWith(t, recordFile(odd, scopedRecord(scope, server.URL, hereToken), near, everywhere))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, outcome{stdout: logoutDocument(server.URL, scope)}, got)
	assertNoRecordedToken(t, got)
	assertNoToken(t, got, oddToken)
	assertNoToken(t, got, nearToken)
	assert.Equal(t, savedFile(everywhere, near, odd), fileBytes(t, path))
	assert.Empty(t, server.Requests())
}

func TestAuthLogoutPrintsTheAddressWithoutItsPassword(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.ServeNothing(t)
	behind, err := url.Parse(server.URL)
	require.NoError(t, err)
	behind.User = url.UserPassword("svc", "secret")
	home, path := homeWith(t, recordFile(scopedRecord(scope, behind.String(), hereToken)))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, outcome{stdout: logoutDocument("http://svc:xxxxx@"+behind.Host+behind.Path, scope)}, got)
	assert.NotContains(t, got.stdout, "secret")
	assertNoRecordedToken(t, got)
	assert.NoFileExists(t, path)
	assert.Empty(t, server.Requests())
}

func TestAuthLogoutLeavesTheFileReadableByItsOwnerAlone(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.ServeNothing(t)
	records := recordFile(scopedRecord(scope, server.URL, hereToken), unscopedRecord(server.URL, everywhereToken))
	home, path := homeWith(t, records)
	require.NoError(t, os.Chmod(path, 0o644))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	require.Equal(t, 0, got.code, "stderr: %q", got.stderr)
	held, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), held.Mode().Perm())
	assert.Empty(t, server.Requests())
}

func TestAuthLogoutWritesNothingIntoTheDirectoryItWasCalledIn(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.ServeNothing(t)
	home, _ := homeWith(t, recordFile(scopedRecord(scope, server.URL, hereToken)))
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

	want := faultDocument{code: "bad_usage", details: []detail{fileDetail(path)}}
	assert.Equal(t, want, requireFault(t, got))
	assert.Equal(t, held, fileBytes(t, path))
}
