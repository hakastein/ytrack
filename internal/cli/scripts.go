package cli

import (
	"context"
	"io"
	"slices"
	"strings"

	"github.com/hakastein/cliche"
	"github.com/spf13/cobra"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/script"
)

const builtinGroup = "builtin"

func loadScripts(root *cobra.Command, env []string) *script.Catalog {
	reserved := make([]string, 0, len(root.Commands()))
	for _, command := range root.Commands() {
		reserved = append(reserved, command.Name())
	}
	dir, _ := workingDirectory()
	return script.Load(reserved, script.Places{WorkingDir: dir, Home: lookup(env, homeVariable)})
}

func addScripts(root *cobra.Command, catalog *script.Catalog, env []string, stdout io.Writer, renderer render.Renderer,
	stream *diag.Stream,
) {
	catalog.Scripts.AddTo(root, func(cmd *cobra.Command, command *cliche.Command, in cliche.Input) error {
		answer, wrote, fault := script.Run(cmd.Context(), command, in, scriptHost(env, stream))
		if fault == nil {
			fault = printNode(stdout, renderer, answer)
		}
		if fault == nil {
			return nil
		}
		if wrote {
			fault.AfterWrite = true
		}
		return fault
	})
	groupRoots(root, catalog.Roots)
	shown := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		for _, warning := range catalog.Warnings(strings.Fields(cmd.CommandPath())[1:]) {
			stream.Warn(warning)
		}
		shown(cmd, args)
	})
}

// A hidden script warns on every call under its first word, and on --help too, which no hook of cobra runs before.
func warnHidden(root *cobra.Command, catalog *script.Catalog, argv []string, stream *diag.Stream) {
	called, _, err := root.Find(argv)
	if err != nil || called == root {
		return
	}
	word := strings.Fields(called.CommandPath())[1]
	for _, hidden := range catalog.Hidden {
		if hidden.Word == word {
			stream.Warn(hidden.Warning)
		}
	}
}

// roots names the directory of the root of each first word a script of a project or of the user holds.
func groupRoots(root *cobra.Command, roots map[string]string) {
	if !slices.ContainsFunc(root.Commands(), func(child *cobra.Command) bool {
		_, isScript := roots[child.Name()]
		return isScript
	}) {
		return
	}
	root.AddGroup(&cobra.Group{ID: builtinGroup, Title: "Commands:"})
	for _, child := range root.Commands() {
		dir, isScript := roots[child.Name()]
		if !isScript {
			child.GroupID = builtinGroup
			continue
		}
		if !root.ContainsGroup(dir) {
			root.AddGroup(&cobra.Group{ID: dir, Title: "Commands of " + dir + ":"})
		}
		child.GroupID = dir
	}
}

func scriptHost(env []string, stream *diag.Stream) script.Host {
	var held *connection
	connected := func() (connection, *diag.Fault) {
		if held != nil {
			return *held, nil
		}
		c, fault := connect(env)
		if fault != nil {
			return connection{}, fault
		}
		held = &c
		return c, nil
	}
	return script.Host{
		Call: func(ctx context.Context, f func(context.Context, *youtrack.Client) (*youtrack.Node, error)) (*youtrack.Node, *diag.Fault) {
			c, fault := connected()
			if fault != nil {
				return nil, fault
			}
			return c.call(ctx, f)
		},
		Address: func() (string, *diag.Fault) {
			c, fault := connected()
			if fault != nil {
				return "", fault
			}
			return c.address.String(), nil
		},
		Warn: stream.Warn,
	}
}
