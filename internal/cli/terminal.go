package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/hakastein/ytrack/internal/diag"
)

// The one thing ytrack asks stdin is whether it is a terminal; a pipe, a file or /dev/null is refused here, before a
// byte of it is read, so a token cannot be given by piping it in.
const notATerminal = "auth login asks for the token on a terminal and stdin is not one, so nothing was read from it: " +
	"where there is no terminal the address and the token are given in YTRACK_URL and YTRACK_TOKEN, or by a login saved on a terminal"

const (
	addressPrompt = "YouTrack URL: "
	tokenPrompt   = "Token: "

	dialogueStopped = "auth login stopped before it had a token to check, and nothing was kept"
)

// console is the terminal the dialogue is held on: the file it reads the answers from and the file it prompts on. A
// prompt belongs on the terminal that was answered, not on stdout, which carries one document, or on stderr, which
// carries the stream of refusals.
type console struct {
	in, out *os.File
}

// Close lets go of the file the prompts went to when it was opened for them; stdin belongs to the caller.
func (c console) Close() {
	if c.out != c.in {
		_ = c.out.Close()
	}
}

func terminal(stdin *os.File) (console, *diag.Fault) {
	// A nil file answers with a descriptor no call owns, so a caller who handed none is refused rather than crashing.
	if !term.IsTerminal(int(stdin.Fd())) {
		return console{}, &diag.Fault{Code: diag.BadUsage, Message: notATerminal}
	}
	out, err := promptsOn(stdin)
	if err != nil {
		return console{}, unwritable(err)
	}
	return console{in: stdin, out: out}, nil
}

// askForTheAddress is the line typed after the first prompt, which the terminal echoes as any other.
func askForTheAddress(ctx context.Context, tty console) (string, *diag.Fault) {
	if fault := say(tty.out, addressPrompt); fault != nil {
		return "", fault
	}
	return typedLine(ctx, func() ([]byte, error) { return readLine(tty.in) })
}

// askForTheToken is the line after the second prompt, which the terminal does not echo. ReadPassword takes the echo
// off after the prompt is written and puts it back on its way out; the state taken before it is what puts the echo
// back when the context is done instead and ReadPassword is left waiting.
func askForTheToken(ctx context.Context, tty console) (string, *diag.Fault) {
	descriptor := int(tty.in.Fd())
	state, err := term.GetState(descriptor)
	if err != nil {
		return "", unreadable(err)
	}
	if fault := say(tty.out, tokenPrompt); fault != nil {
		return "", fault
	}
	secret, fault := typedLine(ctx, func() ([]byte, error) { return term.ReadPassword(descriptor) })
	if fault != nil {
		_ = term.Restore(descriptor, state)
	}
	// The return key was not echoed either, so the line the prompt stands on is ended here.
	if said := say(tty.out, "\n"); fault == nil {
		fault = said
	}
	if fault != nil {
		return "", fault
	}
	return secret, nil
}

type answer struct {
	line []byte
	err  error
}

// A read of a terminal returns for a line and for nothing else — not for a context that was cancelled — so it waits
// on a goroutine of its own, which is what lets one interrupt end auth login rather than leave it asking.
func typedLine(ctx context.Context, read func() ([]byte, error)) (string, *diag.Fault) {
	answered := make(chan answer, 1)
	go func() {
		line, err := read()
		answered <- answer{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", &diag.Fault{Code: diag.UpstreamFailed, Message: dialogueStopped}
	case got := <-answered:
		// A terminal at its end has said all it is going to, and what was typed before that end stands as the line.
		if got.err != nil && !errors.Is(got.err, io.EOF) {
			return "", unreadable(got.err)
		}
		return strings.TrimSpace(string(got.line)), nil
	}
}

// The line is read a byte at a time so that nothing typed past it is taken out of the terminal: what follows the
// return key belongs to whatever runs next, not to ytrack.
func readLine(tty *os.File) ([]byte, error) {
	var line []byte
	var read [1]byte
	for {
		n, err := tty.Read(read[:])
		if n > 0 {
			if read[0] == '\n' {
				return line, nil
			}
			line = append(line, read[0])
			continue
		}
		if err != nil {
			return line, err
		}
	}
}

func say(screen *os.File, words string) *diag.Fault {
	if _, err := io.WriteString(screen, words); err != nil {
		return unwritable(err)
	}
	return nil
}

// ADR-0005 has no code for a terminal that broke, and upstream_failed is where what it does not name goes.
func unwritable(err error) *diag.Fault {
	return &diag.Fault{Code: diag.UpstreamFailed, Message: "the terminal cannot be written to: " + err.Error()}
}

func unreadable(err error) *diag.Fault {
	return &diag.Fault{Code: diag.UpstreamFailed, Message: "the terminal cannot be read: " + err.Error()}
}
