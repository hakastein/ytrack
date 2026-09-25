package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/hakastein/ytrack/internal/cli"
)

// Set with -ldflags -X: go build does not stamp a v26 tag on a module path without /v26.
var version string

func main() {
	ctx, letNextSignalKill := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, letNextSignalKill)
	var build *debug.BuildInfo
	if stamped, ok := debug.ReadBuildInfo(); ok {
		build = stamped
		if version != "" {
			build.Main.Version = version
		}
	}
	os.Exit(cli.Run(ctx, os.Args[1:], os.Environ(), build, os.Stdin, os.Stdout, os.Stderr))
}
