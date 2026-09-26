package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func here(t *testing.T) (stated, scope string) {
	t.Helper()
	stated, err := os.Getwd()
	require.NoError(t, err)
	scope, err = filepath.EvalSymlinks(stated)
	require.NoError(t, err)
	return stated, scope
}

func serveTheRecordedUsers(t *testing.T) *fake.Server {
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

func bearing(secret string) string {
	return "Bearer " + secret
}

func TestAuthStatusTakesTheRecordOfTheNearestDirectory(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	above := filepath.Dir(scope)
	tests := []struct {
		name    string
		records func(address string) []string
		token   string
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
			token: hereToken,
		},
		{
			name: "a record for the parent of the directory and a global one",
			records: func(address string) []string {
				return []string{
					unscopedRecord(address, everywhereToken),
					scopedRecord(above, address, aboveToken),
				}
			},
			token: aboveToken,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveTheRecordedUsers(t)
			home, _ := homeWith(t, recordFile(tc.records(server.URL)...))

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "status")

			assert.Equal(t, 0, got.code)
			assert.Equal(t, bearing(tc.token), server.Request(t, 0).Header.Get("Authorization"))
		})
	}
}

func TestAuthStatusTakesBothValuesFromOneRecord(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	nearest, global := serveTheRecordedUsers(t), fake.ServeNothing(t)
	records := recordFile(unscopedRecord(global.URL, everywhereToken), scopedRecord(scope, nearest.URL, hereToken))
	home, _ := homeWith(t, records)

	got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "status")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, bearing(hereToken), nearest.Request(t, 0).Header.Get("Authorization"))
}

func TestAuthStatusMatchesAScopeByWholePathComponents(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name  string
		held  string
		token string
	}{
		{name: "a scope the directory begins with without being under it", held: scope[:len(scope)-1], token: everywhereToken},
		{name: "the root directory, which every directory is under", held: string(filepath.Separator), token: hereToken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveTheRecordedUsers(t)
			records := recordFile(unscopedRecord(server.URL, everywhereToken), scopedRecord(tc.held, server.URL, hereToken))
			home, _ := homeWith(t, records)

			got := runWith(t, []string{"HOME=" + home, "PWD=" + stated}, "auth", "status")

			assert.Equal(t, 0, got.code)
			assert.Equal(t, bearing(tc.token), server.Request(t, 0).Header.Get("Authorization"))
		})
	}
}

func TestAuthStatusTakesTheRecordOfTheDirectoryASymlinkedPWDReaches(t *testing.T) {
	t.Parallel()
	_, scope := here(t)
	link := filepath.Join(t.TempDir(), "here")
	require.NoError(t, os.Symlink(scope, link))
	server := serveTheRecordedUsers(t)
	home, _ := homeWith(t, recordFile(scopedRecord(scope, server.URL, hereToken)))

	got := runWith(t, []string{"HOME=" + home, "PWD=" + link}, "auth", "status")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, bearing(hereToken), server.Request(t, 0).Header.Get("Authorization"))
}

func TestNoCommandChoosesARecordWithoutKnowingWhichDirectoryItIsIn(t *testing.T) {
	t.Parallel()
	_, scope := here(t)
	tests := []struct {
		name string
		pwd  func(elsewhere string) []string
	}{
		{name: "no PWD at all", pwd: func(string) []string { return nil }},
		{name: "a relative PWD naming the directory of the call", pwd: func(string) []string { return []string{"PWD=."} }},
		{name: "a PWD naming a directory the call is not in", pwd: func(elsewhere string) []string { return []string{"PWD=" + elsewhere} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			records := recordFile(scopedRecord(scope, server.URL, hereToken), unscopedRecord(server.URL, everywhereToken))
			home, _ := homeWith(t, records)

			got := runWith(t, append([]string{"HOME=" + home}, tc.pwd(t.TempDir())...), "auth", "status")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assertNoRecordedToken(t, got)
		})
	}
}

func TestAuthStatusAsksForNoDirectoryWithoutARecordThatNamesOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		global func(address string) string
	}{
		{name: "a record without a scope", global: func(address string) string { return unscopedRecord(address, everywhereToken) }},
		{name: "a record whose scope is null", global: func(address string) string {
			return fmt.Sprintf(`{"scope":null,"url":%q,"token":%q}`, address, everywhereToken)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveTheRecordedUsers(t)
			home, _ := homeWith(t, recordFile(tc.global(server.URL)))

			got := runWith(t, []string{"HOME=" + home, "PWD=" + t.TempDir()}, "auth", "status")

			assert.Equal(t, 0, got.code)
			assert.Equal(t, bearing(everywhereToken), server.Request(t, 0).Header.Get("Authorization"))
		})
	}
}

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

			want := faultDocument{code: "bad_usage", details: []detail{fileDetail(path)}}
			assert.Equal(t, want, requireFault(t, got))
			assertNoToken(t, got, "perm-x")
			assertNoToken(t, got, "perm-y")
		})
	}
}
