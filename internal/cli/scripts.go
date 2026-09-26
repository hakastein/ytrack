package cli

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"

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
	groups := map[string]*script.Root{}
	for _, command := range catalog.Commands {
		parent := groupOf(root, command.Path[:len(command.Path)-1], false)
		parent.AddCommand(newScriptCommand(command, env, stdout, renderer, stream))
		groups[command.Path[0]] = command.Root()
	}
	for _, broken := range catalog.Broken {
		if len(broken.Path) == 0 {
			continue
		}
		placeholder := newCommand(broken.Path[len(broken.Path)-1], func(*cobra.Command, []string) *diag.Fault {
			return broken.Fault
		})
		placeholder.Hidden = true
		placeholder.DisableFlagParsing = true
		groupOf(root, broken.Path[:len(broken.Path)-1], true).AddCommand(placeholder)
	}
	groupRoots(root, groups)
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

// A directory with no command under it is no command either, so a group made for a broken path alone stays hidden.
func groupOf(root *cobra.Command, path []string, hidden bool) *cobra.Command {
	parent := root
	for _, word := range path {
		i := slices.IndexFunc(parent.Commands(), func(child *cobra.Command) bool { return child.Name() == word })
		if i >= 0 {
			parent = parent.Commands()[i]
			continue
		}
		group := newCommand(word, requireSubcommand)
		group.Short = "Run the " + word + " commands"
		group.Hidden = hidden
		parent.AddCommand(group)
		parent = group
	}
	return parent
}

func groupRoots(root *cobra.Command, roots map[string]*script.Root) {
	if !slices.ContainsFunc(root.Commands(), func(child *cobra.Command) bool {
		held, isScript := roots[child.Name()]
		return isScript && !held.Builtin()
	}) {
		return
	}
	root.AddGroup(&cobra.Group{ID: builtinGroup, Title: "Commands:"})
	for _, child := range root.Commands() {
		held, isScript := roots[child.Name()]
		if !isScript || held.Builtin() {
			child.GroupID = builtinGroup
			continue
		}
		if !root.ContainsGroup(held.Dir) {
			root.AddGroup(&cobra.Group{ID: held.Dir, Title: "Commands of " + held.Dir + ":"})
		}
		child.GroupID = held.Dir
	}
}

func newScriptCommand(command *script.Command, env []string, stdout io.Writer, renderer render.Renderer,
	stream *diag.Stream,
) *cobra.Command {
	use := command.Path[len(command.Path)-1]
	var paths []string
	for i, arg := range command.Args {
		use += " <" + arg.Name + ">"
		if arg.Path {
			paths = append(paths, strconv.Itoa(i+1))
		}
	}
	values := map[string]func() any{}
	cmd := newCommand(use, func(cmd *cobra.Command, args []string) *diag.Fault {
		input := script.Input{Args: args, Flags: map[string]any{}}
		for name, value := range values {
			if cmd.Flags().Changed(name) {
				input.Flags[name] = value()
			}
		}
		answer, wrote, fault := script.Run(cmd.Context(), command, input, scriptHost(env, stream))
		if fault == nil {
			fault = printNode(stdout, renderer, answer)
		}
		if fault != nil && wrote {
			fault.AfterWrite = true
		}
		return fault
	})
	cmd.Args = cobra.ExactArgs(len(command.Args))
	cmd.Annotations = map[string]string{completesPathAt: strings.Join(paths, ",")}
	cmd.Short = command.Short
	cmd.Long = command.Long
	if command.Example != nil {
		cmd.Long += "\n\n" + example(command.Example)
	}
	for _, flag := range command.Flags {
		values[flag.Name] = declareFlag(cmd, flag)
	}
	return cmd
}

// An int wider than the int parameter of a function would fail the script instead of the call.
func declareFlag(cmd *cobra.Command, flag script.Flag) func() any {
	flags := cmd.Flags()
	var read func() any
	switch flag.Type {
	case script.IntFlag:
		number := flags.Int32(flag.Name, 0, flag.Usage)
		read = func() any { return *number }
	case script.BoolFlag:
		set := flags.Bool(flag.Name, false, flag.Usage)
		read = func() any { return *set }
	default:
		given := &choicesValue{kind: flag.Type, choices: flag.Choices}
		flags.Var(given, flag.Name, flag.Usage)
		read = func() any { return given.values }
		if flag.Type == script.StringFlag {
			read = func() any { return given.values[0] }
		}
	}
	declared := flags.Lookup(flag.Name)
	if flag.Type != script.StringsFlag {
		rejectRepeat(declared)
	}
	closedSet(declared, flag.Choices)
	return read
}

type choicesValue struct {
	kind    script.FlagType
	choices []string
	values  []string
}

func (v *choicesValue) String() string {
	return strings.Join(v.values, ",")
}

func (v *choicesValue) Set(text string) error {
	if len(v.choices) > 0 && !slices.Contains(v.choices, text) {
		return errors.New("it is none of " + strings.Join(v.choices, ", "))
	}
	v.values = append(v.values, text)
	return nil
}

func (v *choicesValue) Type() string {
	return string(v.kind)
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
			return c.address.Redacted(), nil
		},
		Warn: stream.Warn,
	}
}
