// Command ytrack is the only code that touches the process; everything else receives
// it through cli.Run.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/hakastein/ytrack/internal/cli"
)

// version is the calendar version a release build stamps with -ldflags "-X main.version=…". go build cannot
// take it off the tag: a tag of major 26 on a module path without /v26 is not a version of this module to Go.
// Left empty, as by a plain go build, the version go build put into the stamp stands (ADR-0009).
var version string

func main() {
	// Unhandled, a signal kills ytrack with nothing said about a request already on the wire; cancelling
	// ends that request as any other given-up call, which for a write is write_uncertain and exit code 2.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Cancelling takes the handler back off, so a second signal kills the process by Go's own default.
	context.AfterFunc(ctx, stop)
	// The stamp go build left in this binary, read here because the image of the process is this package's to
	// read. ok is false only in a binary the Go toolchain did not build: there is no stamp to hand over then,
	// and --version says so.
	var build *debug.BuildInfo
	if stamped, ok := debug.ReadBuildInfo(); ok {
		build = stamped
		if version != "" {
			build.Main.Version = version
		}
	}
	os.Exit(cli.Run(ctx, os.Args[1:], os.Environ(), build, os.Stdin, os.Stdout, os.Stderr))
}
