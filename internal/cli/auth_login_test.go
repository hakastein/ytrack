//go:build linux

package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/hakastein/ytrack/internal/cli"
)

// The token a human types in these scenarios, and the user the server says it belongs to.
const (
	typedToken = "perm-ytrack-test-typed"
	typedUser  = "from.typed"
)

// What the dialogue puts on the terminal.
const (
	addressPrompt = "YouTrack URL: "
	tokenPrompt   = "Token: "
)

// transcript is what the terminal showed — the prompts and whatever the line discipline echoed back — and whether
// the command left the echo on.
type transcript struct {
	shown  string
	echoes bool
}

// keyboard is the master side of the pseudoterminal: the test types on it while the command reads and prompts on
// the other side, and shown is everything drawn off it so far.
type keyboard struct {
	master *os.File
	slave  *os.File
	// (*os.File).Fd puts the file back into blocking mode, so the descriptor is taken once and made
	// non-blocking after that, never through Fd again.
	descriptor int
	shown      []byte
}

func (k *keyboard) typeLine(t *testing.T, line string) {
	t.Helper()
	_, err := io.WriteString(k.master, line+"\n")
	require.NoError(t, err)
}

// An EOT is how a terminal in canonical mode ends the input of the read waiting on it.
func (k *keyboard) typeTheEndOfInput(t *testing.T) {
	t.Helper()
	_, err := k.master.Write([]byte{0x04})
	require.NoError(t, err)
}

// The prompt for the token is written while the echo is still on, so a test that typed on seeing it would have the
// token echoed back; the termios of the terminal is what says the echo has gone.
func (k *keyboard) waitForTheEchoToGoOff(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return !k.echoes() }, 5*time.Second, time.Millisecond)
}

func (k *keyboard) echoes() bool {
	termios, err := unix.IoctlGetTermios(int(k.slave.Fd()), unix.TCGETS)
	return err == nil && termios.Lflag&unix.ECHO != 0
}

// draw takes what the terminal has to show at this moment. A blocking read of a terminal nobody will write to
// again is woken by neither a deadline nor a close, so the reading is done out of blocking mode, where having
// nothing more to show is EAGAIN.
func (k *keyboard) draw() error {
	held := make([]byte, 4096)
	for {
		n, err := unix.Read(k.descriptor, held)
		if n <= 0 {
			return err
		}
		k.shown = append(k.shown, held[:n]...)
	}
}

// waitForThePrompt holds the typist back until the command has asked, so that what the terminal showed reads in
// the order a human saw it rather than in the order two goroutines happened to reach it.
func (k *keyboard) waitForThePrompt(t *testing.T, words string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_ = k.draw()
		return strings.Contains(string(k.shown), words)
	}, 5*time.Second, time.Millisecond)
}

// everythingShown is the whole of what the terminal showed: the command wrote its prompts and the line discipline
// echoed what was typed, all of it before the command returned, and nothing else reads this side.
func (k *keyboard) everythingShown(t *testing.T) string {
	t.Helper()
	require.ErrorIs(t, k.draw(), unix.EAGAIN)
	return string(k.shown)
}

// runOnATerminal runs argv with a pseudoterminal for stdin and lets answering type on it while the command is in the
// middle of the dialogue.
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
	typist := &keyboard{master: master, slave: slave, descriptor: int(master.Fd())}
	require.NoError(t, unix.SetNonblock(typist.descriptor, true))

	var stdout, stderr bytes.Buffer
	finished := make(chan int, 1)
	go func() { finished <- cli.Run(ctx, argv, env, nil, slave, &stdout, &stderr) }()

	answering(t, typist)
	code := <-finished

	said := transcript{echoes: typist.echoes(), shown: typist.everythingShown(t)}
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}, said
}

// typeAnswers waits for each prompt and types after it, the token once the echo has gone off.
func typeAnswers(address, secret string) func(t *testing.T, k *keyboard) {
	return func(t *testing.T, k *keyboard) {
		t.Helper()
		k.waitForThePrompt(t, addressPrompt)
		k.typeLine(t, address)
		k.waitForTheEchoToGoOff(t)
		k.typeLine(t, secret)
	}
}

func sayingNothing(*testing.T, *keyboard) {}

// A token typed on a terminal has one more place it could show up than a token found in a variable or a file: the
// echo of the terminal it was typed on.
func assertTheTokenWasNotShown(t *testing.T, got outcome, said transcript, secret string) {
	t.Helper()
	assertNoToken(t, got, secret)
	assert.NotContains(t, said.shown, secret)
}

// loginDocument is what auth login prints: where the login applies and whose token the server said it is.
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

	got, said := runOnATerminal(t, env, typeAnswers(server.url, typedToken), "auth", "login")

	assert.Equal(t, outcome{stdout: loginDocument(server.url, scope, typedUser, typedUser)}, got)
	assertTheTokenWasNotShown(t, got, said, typedToken)
	// The whole of what the terminal showed: the two prompts, the echo of the address, which is typed in the open,
	// and the line the dialogue ends itself, the return key after the token having gone unechoed with it.
	assert.Equal(t, addressPrompt+server.url+"\r\n"+tokenPrompt+"\r\n", said.shown)
	assert.True(t, said.echoes, "the terminal was left without its echo")
	assert.Equal(t, savedFile(scopedRecord(scope, server.url, typedToken)), fileBytes(t, path))
	assert.Equal(t, fs.FileMode(0o600), mode(t, path))
	assert.Equal(t, fs.FileMode(0o700), mode(t, filepath.Dir(path)))
	assert.Equal(t, []string{".ytrack"}, entries(t, home))
	assert.Equal(t, []string{"auth.json"}, entries(t, filepath.Dir(path)))
	// Nothing of ytrack's is kept in the project a caller works in.
	assert.Equal(t, before, entries(t, stated))

	// The login just kept is the one every other command finds here, with no environment to name it.
	afterwards := runWith(t, env, "auth", "status")

	want := status(server.url, "settings", typedUser, typedUser)
	assert.Equal(t, outcome{stdout: want}, afterwards)
	assertNoToken(t, afterwards, typedToken)
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	held, err := os.Stat(path)
	require.NoError(t, err)
	return held.Mode().Perm()
}

// A login made for everywhere is the one a caller wants for the instance they work with outside their projects, and
// it leaves the logins of the directories alone.
func TestAuthLoginGlobalKeepsTheLoginForEverywhere(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	held := scopedRecord(scope, "http://elsewhere.example", hereToken)
	home, path := homeWith(t, recordFile(held))

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.url, typedToken), "auth", "login", "--global")

	assert.Equal(t, outcome{stdout: loginDocument(server.url, "global", typedUser, typedUser)}, got)
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assertNoRecordedToken(t, got)
	assert.Equal(t, savedFile(unscopedRecord(server.url, typedToken), held), fileBytes(t, path))
}

// Logging in for everywhere again is what a rotated token calls for, and a second record without a scope would
// make the whole file unreadable for every command until a human took one of them out.
func TestAuthLoginGlobalReplacesTheSavedGlobalLogin(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	held := scopedRecord(scope, "http://elsewhere.example", hereToken)
	home, path := homeWith(t, recordFile(unscopedRecord("http://was.example", everywhereToken), held))

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.url, typedToken), "auth", "login", "--global")

	assert.Equal(t, outcome{stdout: loginDocument(server.url, "global", typedUser, typedUser)}, got)
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assertNoRecordedToken(t, got)
	assert.Equal(t, savedFile(unscopedRecord(server.url, typedToken), held), fileBytes(t, path))
}

// An instance behind a proxy that asks for a password has that password in its address, and a login run from a
// pipeline puts what it prints into the job's log.
func TestAuthLoginPrintsTheAddressWithoutItsPassword(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	behind, err := url.Parse(server.url)
	require.NoError(t, err)
	behind.User = url.UserPassword("svc", "secret")
	home, path := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(behind.String(), typedToken), "auth", "login")

	printed := "http://svc:xxxxx@" + behind.Host
	assert.Equal(t, outcome{stdout: loginDocument(printed, scope, typedUser, typedUser)}, got)
	assert.NotContains(t, got.stdout, "secret")
	assertTheTokenWasNotShown(t, got, said, typedToken)
	// The record keeps the address as it was typed: without the password nothing of this would get past the proxy.
	assert.Equal(t, savedFile(scopedRecord(scope, behind.String(), typedToken)), fileBytes(t, path))
}

func TestAuthLoginReplacesTheLoginSavedForTheSameDirectory(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	everywhere := unscopedRecord("http://elsewhere.example", everywhereToken)
	held := recordFile(scopedRecord(scope, "http://was.example", hereToken), everywhere)
	home, path := homeWith(t, held)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.url, typedToken), "auth", "login")

	assert.Equal(t, outcome{stdout: loginDocument(server.url, scope, typedUser, typedUser)}, got)
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assertNoRecordedToken(t, got)
	assert.Equal(t, savedFile(everywhere, scopedRecord(scope, server.url, typedToken)), fileBytes(t, path))
}

// The server is asked whose token it is before anything is written, so a token it refuses is refused here rather
// than by every command that would have used it afterwards.
func TestAuthLoginKeepsNoLoginTheServerRefuses(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serve(t, respondWith(http.StatusUnauthorized, `{"error":"Unauthorized","error_description":"Invalid token"}`))
	held := recordFile(scopedRecord(scope, "http://was.example", hereToken))
	home, path := homeWith(t, held)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.url, typedToken), "auth", "login")

	want := faultDocument{
		code: "denied",
		details: []detail{
			{"request", meRequest(server.url)},
			{"upstream_status", 401},
			{"upstream_error", "Unauthorized"},
			{"upstream_message", "Invalid token"},
		},
	}
	// The token was typed on this run rather than found somewhere, so the refusal has no origin to name.
	assert.Equal(t, want, requireRefusal(t, got))
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assert.Equal(t, held, fileBytes(t, path))
}

func TestAuthLoginKeepsNoLoginWhenNothingAnswersAtTheAddress(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	listening, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closed := "http://" + listening.Addr().String()
	require.NoError(t, listening.Close())
	home, path := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(closed, typedToken), "auth", "login")

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", meRequest(closed)}}, found.details)
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
		{name: "no scheme", address: "localhost:8091"},
		{name: "another scheme", address: "ftp://h"},
		{name: "a query", address: "http://h/?q=1"},
		{name: "a fragment", address: "http://h/#f"},
		{name: "nothing at all", address: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			home, path := emptyHome(t)

			got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, func(t *testing.T, k *keyboard) {
				k.typeLine(t, tc.address)
			}, "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Contains(t, said.shown, addressPrompt)
			assert.NotContains(t, said.shown, tokenPrompt, "the token was asked for after the address was refused")
			assert.NoFileExists(t, path)
			assert.Empty(t, server.requests())
		})
	}
}

// A terminal at its end says as much as a refused address does: there is no address to log in with.
func TestAuthLoginRefusesAnEndOfInputAtTheAddressPrompt(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	server := serveNothing(t)
	home, path := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, func(t *testing.T, k *keyboard) {
		k.typeTheEndOfInput(t)
	}, "auth", "login")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
	assert.NotContains(t, said.shown, tokenPrompt)
	assert.True(t, said.echoes)
	assert.NoFileExists(t, path)
	assert.Empty(t, server.requests())
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
		{name: "a tab", typed: "\t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			held := recordFile(scopedRecord(scope, "http://was.example", hereToken))
			home, path := homeWith(t, held)

			got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.url, tc.typed), "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.True(t, said.echoes)
			assert.Equal(t, held, fileBytes(t, path))
			assert.Empty(t, server.requests())
		})
	}
}

// A token pasted out of a page or a log brings whatever was copied with it, and the check that keeps a control
// byte out of a header is the same one a token from a variable or a record goes through.
func TestAuthLoginRefusesATokenItCannotSend(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name  string
		typed string
	}{
		{name: "a start of heading", typed: "perm-\x01bad"},
		{name: "an escape", typed: "perm-\x1bbad"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)
			held := recordFile(scopedRecord(scope, "http://was.example", hereToken))
			home, path := homeWith(t, held)

			got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.url, tc.typed), "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assertTheTokenWasNotShown(t, got, said, tc.typed)
			assert.True(t, said.echoes, "the terminal was left without its echo")
			assert.Equal(t, held, fileBytes(t, path))
			assert.Empty(t, server.requests())
		})
	}
}

func TestAuthLoginUsesNoFileOfLoginRecordsItCannotRead(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	server := serveNothing(t)
	held := `[{"url":"http://h","token":"perm-x","expires":"never"}]`
	home, path := homeWith(t, held)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, func(t *testing.T, k *keyboard) {
		k.typeTheEndOfInput(t)
	}, "auth", "login")

	want := faultDocument{code: "bad_usage", details: []detail{fileDetail(path)}}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.NotContains(t, said.shown, addressPrompt, "an address was asked for before the file that would hold it was read")
	assert.Equal(t, held, fileBytes(t, path))
	assert.Empty(t, server.requests())
}

// A token pasted into a terminal brings whatever whitespace was around it in the file it was copied from, and the
// server answers 401 to a token with a space on it.
func TestAuthLoginSendsTheTokenWithoutTheSpacesAroundIt(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	server := serveUserOfTheToken(t, map[string]string{typedToken: typedUser})
	home, path := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(server.url, "  "+typedToken+"\t"), "auth", "login")

	assert.Equal(t, outcome{stdout: loginDocument(server.url, scope, typedUser, typedUser)}, got)
	assertTheTokenWasNotShown(t, got, said, typedToken)
	assert.Equal(t, savedFile(scopedRecord(scope, server.url, typedToken)), fileBytes(t, path))
	requests := server.requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "Bearer "+typedToken, requests[0].Header.Get("Authorization"))
}

// The read of the token returns for a line and for nothing else, so a context cancelled while it waits has to put
// the echo back itself: a caller who interrupted auth login gets a terminal that still shows what they type.
func TestAuthLoginGivesTheEchoBackWhenItIsStoppedAtTheTokenPrompt(t *testing.T) {
	t.Parallel()
	stated, _ := here(t)
	ctx, stop := context.WithCancel(t.Context())
	server := serveNothing(t)
	home, path := emptyHome(t)

	got, said := runOnATerminalUntil(t, ctx, []string{"HOME=" + home, "PWD=" + stated}, func(t *testing.T, k *keyboard) {
		k.typeLine(t, server.url)
		k.waitForTheEchoToGoOff(t)
		stop()
	}, "auth", "login")

	want := faultDocument{code: "upstream_failed"}
	assert.Equal(t, want, requireRefusal(t, got))
	assert.True(t, said.echoes, "the terminal was left without its echo")
	assert.NoFileExists(t, path)
	assert.Empty(t, entries(t, home))
	assert.Empty(t, server.requests())
}

// Which login a call is about is settled before a word is asked for: a refusal afterwards would throw away a token
// already typed.
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
			server := serveNothing(t)
			held := recordFile(scopedRecord(scope, server.url, hereToken))
			home, path := homeWith(t, held)

			got, said := runOnATerminal(t, tc.env(home, stale), sayingNothing, "auth", "login")

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, said.shown, "the terminal was asked something before there was a place to keep the answer")
			assertNoRecordedToken(t, got)
			assert.Equal(t, held, fileBytes(t, path))
			assert.Empty(t, server.requests())
		})
	}
}

func TestAuthLoginKeepsTheAdminOfTheDevInstance(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	dev := devInstance(t)
	home, path := emptyHome(t)

	got, said := runOnATerminal(t, []string{"HOME=" + home, "PWD=" + stated}, typeAnswers(dev.url, dev.token), "auth", "login")

	assert.Equal(t, outcome{stdout: loginDocument(dev.url, scope, "admin", "admin")}, got)
	assertTheTokenWasNotShown(t, got, said, dev.token)
	assert.Equal(t, savedFile(scopedRecord(scope, dev.url, dev.token)), fileBytes(t, path))
	assert.Len(t, dev.requests(), 1)
}
