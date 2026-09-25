//go:build linux

package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/hakastein/ytrack/internal/cli"
)

const (
	typedToken = "perm-ytrack-test-typed"
	typedUser  = "from.typed"
)

const (
	addressPrompt = "YouTrack URL: "
	tokenPrompt   = "Token: "
)

type transcript struct {
	shown  string
	echoes bool
}

const ctrlD = 0x04

type keyboard struct {
	master        *os.File
	slave         *os.File
	nonBlockingFD int
	shown         []byte
}

func (k *keyboard) typeLine(t *testing.T, line string) {
	t.Helper()
	_, err := io.WriteString(k.master, line+"\n")
	require.NoError(t, err)
}

func (k *keyboard) typeTheEndOfInput(t *testing.T) {
	t.Helper()
	_, err := k.master.Write([]byte{ctrlD})
	require.NoError(t, err)
}

func (k *keyboard) waitForTheEchoToGoOff(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return !k.echoes() }, 5*time.Second, time.Millisecond)
}

func (k *keyboard) echoes() bool {
	termios, err := unix.IoctlGetTermios(int(k.slave.Fd()), unix.TCGETS)
	return err == nil && termios.Lflag&unix.ECHO != 0
}

func (k *keyboard) drainWithoutBlocking() error {
	held := make([]byte, 4096)
	for {
		n, err := unix.Read(k.nonBlockingFD, held)
		if n <= 0 {
			return err
		}
		k.shown = append(k.shown, held[:n]...)
	}
}

func (k *keyboard) waitForThePrompt(t *testing.T, words string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_ = k.drainWithoutBlocking()
		return strings.Contains(string(k.shown), words)
	}, 5*time.Second, time.Millisecond)
}

func (k *keyboard) everythingShown(t *testing.T) string {
	t.Helper()
	require.ErrorIs(t, k.drainWithoutBlocking(), unix.EAGAIN)
	return string(k.shown)
}

func runOnATerminal(t *testing.T, env []string, answering func(t *testing.T, k *keyboard), argv ...string) (outcome, transcript) {
	t.Helper()
	return runOnATerminalUntil(t, t.Context(), env, answering, argv...)
}

func runOnATerminalUntil(t *testing.T, ctx context.Context, env []string, answering func(t *testing.T, k *keyboard), argv ...string) (outcome, transcript) {
	t.Helper()
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
	})
	typist := &keyboard{master: master, slave: slave, nonBlockingFD: int(master.Fd())}
	require.NoError(t, unix.SetNonblock(typist.nonBlockingFD, true))

	var stdout, stderr bytes.Buffer
	finished := make(chan int, 1)
	go func() { finished <- cli.Run(ctx, argv, env, nil, slave, &stdout, &stderr) }()

	answering(t, typist)
	code := <-finished

	said := transcript{echoes: typist.echoes(), shown: typist.everythingShown(t)}
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}, said
}

func typeAnswers(address, secret string) func(t *testing.T, k *keyboard) {
	return func(t *testing.T, k *keyboard) {
		t.Helper()
		k.waitForThePrompt(t, addressPrompt)
		k.typeLine(t, address)
		k.waitForTheEchoToGoOff(t)
		k.typeLine(t, secret)
	}
}

func typingTheEndOfInput(t *testing.T, k *keyboard) {
	t.Helper()
	k.typeTheEndOfInput(t)
}

func assertTheTokenWasNotShown(t *testing.T, got outcome, said transcript, secret string) {
	t.Helper()
	assertNoToken(t, got, secret)
	assert.NotContains(t, said.shown, secret)
}

func loginDocument(address, scope, login, fullName string) string {
	return fmt.Sprintf("url: %q\nscope: %q\nuser:\n  login: %q\n  fullName: %q\n", address, scope, login, fullName)
}

func TestAuthLoginKeepsTheLoginTypedForTheDirectoryItWasCalledIn(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	home, path := emptyHome(t)
	env := []string{"HOME=" + home, "PWD=" + stated}
	before := entries(t, stated)

	got, said := runOnATerminal(t, env, typeAnswers(server.URL, typedToken), "auth", "login")

	assert.Equal(t, outcome{stdout: loginDocument(server.URL, scope, typedUser, typedUser)}, got)
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assert.Equal(t, addressPrompt+server.URL+"\r\n"+tokenPrompt+"\r\n", said.shown)
	assert.True(t, said.echoes, "the terminal was left without its echo")
	assert.Equal(t, savedFile(scopedRecord(scope, server.URL, typedToken)), fileBytes(t, path))
	assert.Equal(t, fs.FileMode(0o600), mode(t, path))
	assert.Equal(t, fs.FileMode(0o700), mode(t, filepath.Dir(path)))
	assert.Equal(t, []string{".ytrack"}, entries(t, home))
	assert.Equal(t, []string{"auth.json"}, entries(t, filepath.Dir(path)))
	assert.Equal(t, before, entries(t, stated))

	afterwards := runWith(t, env, "auth", "status")

	assert.Equal(t, 0, afterwards.code)
	assert.Equal(t, bearing(typedToken), server.Request(t, 1).Header.Get("Authorization"))
}

func TestAuthLoginGlobalKeepsTheLoginForEverywhere(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	held := scopedRecord(scope, "http://elsewhere.example", hereToken)
	home, path := homeWith(t, recordFile(held))

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.URL, typedToken), "auth", "login", "--global")

	assert.Equal(t, outcome{stdout: loginDocument(server.URL, "global", typedUser, typedUser)}, got)
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assert.Equal(t, savedFile(unscopedRecord(server.URL, typedToken), held), fileBytes(t, path))
}

func TestAuthLoginGlobalReplacesTheSavedGlobalLogin(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	held := scopedRecord(scope, "http://elsewhere.example", hereToken)
	home, path := homeWith(t, recordFile(unscopedRecord("http://was.example", everywhereToken), held))

	got, _ := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.URL, typedToken), "auth", "login", "--global")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, savedFile(unscopedRecord(server.URL, typedToken), held), fileBytes(t, path))
}

func TestAuthLoginPrintsTheAddressWithoutItsPassword(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	behind := server.Address(t)
	behind.User = url.UserPassword("svc", "secret")
	home, path := emptyHome(t)

	got, _ := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(behind.String(), typedToken), "auth", "login")

	assert.Equal(t, "http://svc:xxxxx@"+behind.Host+behind.Path, urlPrinted(t, got))
	assert.Equal(t, savedFile(scopedRecord(scope, behind.String(), typedToken)), fileBytes(t, path))
}

func TestAuthLoginReplacesTheLoginSavedForTheSameDirectory(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	everywhere := unscopedRecord("http://elsewhere.example", everywhereToken)
	held := recordFile(scopedRecord(scope, "http://was.example", hereToken), everywhere)
	home, path := homeWith(t, held)

	got, _ := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.URL, typedToken), "auth", "login")

	assert.Equal(t, 0, got.code)
	assert.Equal(t, savedFile(everywhere, scopedRecord(scope, server.URL, typedToken)), fileBytes(t, path))
}

func TestAuthLoginKeepsNoLoginTheServerRefuses(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := fake.Serve(t, fake.JSON(http.StatusUnauthorized, `{"error":"Unauthorized","error_description":"Invalid token"}`))
	held := recordFile(scopedRecord(scope, "http://was.example", hereToken))
	home, path := homeWith(t, held)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.URL, typedToken), "auth", "login")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", meRequest(server.URL)},
			{"upstream_status", 401},
			{"upstream_error", "Unauthorized"},
			{"upstream_message", "Invalid token"},
		},
	}
	assert.Equal(t, want, requireFault(t, got))
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assert.Equal(t, held, fileBytes(t, path))
}

func TestAuthLoginKeepsNoLoginWhenNothingAnswersAtTheAddress(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	closed := fake.NobodyListens
	home, path := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(closed, typedToken), "auth", "login")

	assert.Equal(t, faultDocument{code: "upstream_failed", details: []detail{{"request", meRequest(closed)}}}, requireFault(t, got))
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assert.NoFileExists(t, path)
	assert.Empty(t, entries(t, home))
}

func TestAuthLoginRefusesAnAddressItCannotUseBeforeAskingForTheToken(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	tests := []struct {
		name    string
		address string
	}{
		{name: "another scheme", address: "ftp://h"},
		{name: "a query", address: "http://h/?q=1"},
		{name: "a fragment", address: "http://h/#f"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home, path := emptyHome(t)

			got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, func(t *testing.T, k *keyboard) {
				k.waitForThePrompt(t, addressPrompt)
				k.typeLine(t, tc.address)
				k.typeLine(t, "")
			}, "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.NotContains(t, said.shown, tokenPrompt, "the token was asked for after the address was refused")
			assert.NoFileExists(t, path)
		})
	}
}

func TestAuthLoginRefusesAnEndOfInputAtTheAddressPrompt(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	home, path := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typingTheEndOfInput, "auth", "login")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Equal(t, addressPrompt, said.shown)
	assert.NoFileExists(t, path)
}

func TestAuthLoginRefusesATokenOfNothingAtAll(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name  string
		typed string
	}{
		{name: "nothing at all", typed: ""},
		{name: "spaces", typed: "   "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)
			held := recordFile(scopedRecord(scope, "http://was.example", hereToken))
			home, path := homeWith(t, held)

			got, _ := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.URL, tc.typed), "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Equal(t, held, fileBytes(t, path))
		})
	}
}

func TestAuthLoginRefusesATokenItCannotSend(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	const typed = "perm-\x1bbad"
	held := recordFile(scopedRecord(scope, "http://was.example", hereToken))
	home, path := homeWith(t, held)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(fake.ServeNothing(t).URL, typed), "auth", "login")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assertTheTokenWasNotShown(t, got, said, typed)
	assert.True(t, said.echoes, "the terminal was left without its echo")
	assert.Equal(t, held, fileBytes(t, path))
}

func TestAuthLoginUsesNoFileOfLoginRecordsItCannotRead(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	held := `[{"url":"http://h","token":"perm-x","expires":"never"}]`
	home, path := homeWith(t, held)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typingTheEndOfInput, "auth", "login")

	want := faultDocument{code: "bad_usage", details: []detail{fileDetail(path)}}
	assert.Equal(t, want, requireFault(t, got))
	assert.Empty(t, said.shown, "an address was asked for before the file that would hold it was read")
	assert.Equal(t, held, fileBytes(t, path))
}

func TestAuthLoginSendsAndKeepsTheTokenWithoutTheSpacesAroundIt(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name  string
		typed string
		token string
	}{
		{name: "spaces and a tab around the token", typed: "  " + typedToken + "\t", token: typedToken},
		{name: "a tab inside the token", typed: tokenWithATab, token: tokenWithATab},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveUserOfTheToken(t, map[string]string{tc.token: typedUser})
			home, path := emptyHome(t)

			got, _ := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.URL, tc.typed), "auth", "login")

			assert.Equal(t, 0, got.code)
			assert.Equal(t, bearing(tc.token), server.Request(t, 0).Header.Get("Authorization"))
			assert.Equal(t, savedFile(scopedRecord(scope, server.URL, tc.token)), fileBytes(t, path))
		})
	}
}

func TestAuthLoginGivesTheEchoBackWhenItIsStoppedAtTheTokenPrompt(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	ctx, stop := context.WithCancel(t.Context())
	server := fake.ServeNothing(t)
	home, _ := emptyHome(t)

	got, said := runOnATerminalUntil(t, ctx, []string{"HOME=" + home, "PWD=" + stated}, func(t *testing.T, k *keyboard) {
		k.typeLine(t, server.URL)
		k.waitForTheEchoToGoOff(t)
		stop()
	}, "auth", "login")

	assert.Equal(t, faultDocument{code: "upstream_failed"}, requireFault(t, got))
	assert.True(t, said.echoes, "the terminal was left without its echo")
	assert.Empty(t, entries(t, home))
}

func TestAuthLoginAsksForNothingWithoutADirectoryToSaveTheLogin(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name string
		env  func(home, stale string) []string
	}{
		{
			name: "a PWD naming a directory the call is not in",
			env:  func(home, stale string) []string { return []string{"HOME=" + home, "PWD=" + stale} },
		},
		{
			name: "no home directory",
			env:  func(_, stale string) []string { return []string{"PWD=" + stated} },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stale := t.TempDir()
			server := fake.ServeNothing(t)
			held := recordFile(scopedRecord(scope, server.URL, hereToken))
			home, path := homeWith(t, held)

			got, said := runOnATerminal(t, tc.env(home, stale), typingTheEndOfInput, "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, said.shown, "the terminal was asked something before there was a place to keep the answer")
			assertNoRecordedToken(t, got)
			assert.Equal(t, held, fileBytes(t, path))
		})
	}
}

func TestAuthLoginOnATerminalRefusesAnArgument(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	home, _ := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typingTheEndOfInput, "auth", "login", fake.NobodyListens)

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, said.shown, "the terminal was asked for an address past the argument")
	assert.Empty(t, entries(t, home))
}
