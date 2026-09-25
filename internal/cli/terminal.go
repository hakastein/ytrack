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

const notATerminal = "auth login asks for the token on a terminal and stdin is not one, so nothing was read from it: " +
	"where there is no terminal the address and the token are given in YTRACK_URL and YTRACK_TOKEN, or by a login saved on a terminal"

const (
	addressPrompt  = "YouTrack URL: "
	tokenPrompt    = "Token: "
	unechoedReturn = "\n"

	dialogueStopped = "auth login stopped before it had a token to check, and nothing was kept"
)

type console struct {
	in, out *os.File
}

func (c console) Close() {
	if c.out != c.in {
		_ = c.out.Close()
	}
}

func terminal(stdin *os.File) (console, *diag.Fault) {
	if !term.IsTerminal(int(stdin.Fd())) {
		return console{}, &diag.Fault{Code: diag.BadUsage, Message: notATerminal}
	}
	out, err := promptsOn(stdin)
	if err != nil {
		return console{}, unwritable(err)
	}
	return console{in: stdin, out: out}, nil
}

func promptAddress(ctx context.Context, tty console) (string, *diag.Fault) {
	if fault := prompt(tty.out, addressPrompt); fault != nil {
		return "", fault
	}
	return readLineContext(ctx, func() ([]byte, error) { return readLineWithoutReadAhead(tty.in) })
}

func promptToken(ctx context.Context, tty console) (string, *diag.Fault) {
	descriptor := int(tty.in.Fd())
	echoing, err := term.GetState(descriptor)
	if err != nil {
		return "", unreadable(err)
	}
	if fault := prompt(tty.out, tokenPrompt); fault != nil {
		return "", fault
	}
	secret, fault := readLineContext(ctx, func() ([]byte, error) { return term.ReadPassword(descriptor) })
	if fault != nil {
		_ = term.Restore(descriptor, echoing)
	}
	if returnFault := prompt(tty.out, unechoedReturn); fault == nil {
		fault = returnFault
	}
	if fault != nil {
		return "", fault
	}
	return secret, nil
}

type readResult struct {
	line []byte
	err  error
}

func readLineContext(ctx context.Context, read func() ([]byte, error)) (string, *diag.Fault) {
	answered := make(chan readResult, 1)
	go func() {
		line, err := read()
		answered <- readResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", &diag.Fault{Code: diag.UpstreamFailed, Message: dialogueStopped}
	case got := <-answered:
		if got.err != nil && !errors.Is(got.err, io.EOF) {
			return "", unreadable(got.err)
		}
		return strings.TrimSpace(string(got.line)), nil
	}
}

func readLineWithoutReadAhead(tty *os.File) ([]byte, error) {
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

func prompt(screen *os.File, words string) *diag.Fault {
	if _, err := io.WriteString(screen, words); err != nil {
		return unwritable(err)
	}
	return nil
}

func unwritable(err error) *diag.Fault {
	return &diag.Fault{Code: diag.UpstreamFailed, Message: "the terminal cannot be written to: " + err.Error()}
}

func unreadable(err error) *diag.Fault {
	return &diag.Fault{Code: diag.UpstreamFailed, Message: "the terminal cannot be read: " + err.Error()}
}
