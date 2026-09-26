package cli_test

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	held, err := os.Stat(path)
	require.NoError(t, err)
	return held.Mode().Perm()
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
	assert.Equal(t, savedFile(everywhere, parent), fileBytes(t, path))
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
			assert.Equal(t, savedFile(held), fileBytes(t, path))
		})
	}
}

func TestAuthLogoutTakesTheFileAwayWithTheLastRecord(t *testing.T) {
	t.Parallel()
	home, path := homeWith(t, globalRecord(fake.ServeNothing(t).URL, everywhereToken))

	got := runWith(t, []string{"HOME=" + home}, "auth", "logout", "--global")

	assert.Equal(t, 0, got.code)
	assert.Empty(t, entries(t, filepath.Dir(path)))
	assert.Equal(t, []string{".ytrack"}, entries(t, home))
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := recordFile(tc.records(fake.ServeNothing(t).URL)...)
			home, path := homeWith(t, held)

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoRecordedToken(t, got)
			assert.Equal(t, held, fileBytes(t, path))
		})
	}
}

func TestAuthLogoutGlobalRefusesWithoutARecordForEverywhere(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	held := recordFile(scopedRecord(scope, fake.ServeNothing(t).URL, hereToken))
	home, path := homeWith(t, held)

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout", "--global")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assertNoRecordedToken(t, got)
	assert.Equal(t, held, fileBytes(t, path))
}

func TestAuthLogoutRefusesWithoutAFileAndMakesNone(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	home, _ := emptyHome(t)

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, entries(t, home))
}

func TestAuthLogoutRefusesWithoutAPlaceToTakeTheRecordFrom(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name string
		env  func(home, stale string) []string
	}{
		{name: "no home directory", env: func(_, _ string) []string { return []string{"PWD=" + stated} }},
		{name: "a PWD naming a directory the call is not in", env: func(home, stale string) []string {
			return []string{"HOME=" + home, "PWD=" + stale}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := recordFile(scopedRecord(scope, fake.ServeNothing(t).URL, hereToken))
			home, path := homeWith(t, held)

			got := runWith(t, tc.env(home, t.TempDir()), "auth", "logout")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoRecordedToken(t, got)
			assert.Equal(t, held, fileBytes(t, path))
		})
	}
}

func TestAuthLogoutLeavesEveryOtherRecordByteForByte(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.ServeNothing(t)
	odd := `{"scope":"\/tmp\/а \"b\"\nк","url":"http://h","token":"perm-ytrack-test-odd"}`
	near := `{ "url" : "http://g", "scope" : "/aaa", "token" : "perm-ytrack-test-near" }`
	everywhere := unscopedRecord(server.URL, everywhereToken)
	home, path := homeWith(t, recordFile(odd, scopedRecord(scope, server.URL, hereToken), near, everywhere))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, savedFile(everywhere, near, odd), fileBytes(t, path))
}

func TestAuthLogoutPrintsTheAddressWithoutItsPassword(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	behind := fake.ServeNothing(t).Address(t)
	behind.User = url.UserPassword("svc", "secret")
	home, _ := homeWith(t, recordFile(scopedRecord(scope, behind.String(), hereToken)))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	assert.Equal(t, "http://svc:xxxxx@"+behind.Host+behind.Path, urlPrinted(t, got))
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
	assert.Equal(t, fs.FileMode(0o600), mode(t, path))
}

func TestAuthLogoutKeepsTheModeOfTheDirectoryItFinds(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.ServeNothing(t)
	records := recordFile(scopedRecord(scope, server.URL, hereToken), unscopedRecord(server.URL, everywhereToken))
	home, path := homeWith(t, records)
	require.NoError(t, os.Chmod(filepath.Dir(path), 0o750))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "logout")

	require.Equal(t, 0, got.code, "stderr: %q", got.stderr)
	assert.Equal(t, fs.FileMode(0o750), mode(t, filepath.Dir(path)))
}

func TestAuthLogoutWritesNothingIntoTheDirectoryItWasCalledIn(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	home, _ := homeWith(t, recordFile(scopedRecord(scope, fake.ServeNothing(t).URL, hereToken)))
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
