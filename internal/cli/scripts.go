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
	dir, _ := workingDirectory(env)
	return script.Load(reserved, script.Places{WorkingDir: dir, Home: lookup(env, homeVariable)})
}

func addScripts(root *cobra.Command, catalog *script.Catalog, env []string, stdout io.Writer, renderer render.Renderer,
	stream *diag.Stream,
) {
	groups := map[string]*script.Root{}
	for _, command := range catalog.Commands {
		parent := groupOf(root, command.Path[:len(command.Path)-1])
		parent.AddCommand(newScriptCommand(command, env, stdout, renderer, stream))
		groups[command.Path[0]] = command.Root()
	}
	for _, broken := range catalog.Broken {
		if len(broken.Path) == 0 {
			continue
		}
		fault := broken.Fault
		placeholder := newCommand(broken.Path[len(broken.Path)-1], func(*cobra.Command, []string) *diag.Fault {
			copied := *fault
			return &copied
		})
		placeholder.Hidden = true
		placeholder.DisableFlagParsing = true
		groupOf(root, broken.Path[:len(broken.Path)-1]).AddCommand(placeholder)
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
			stream.Warn(hidden.Warning.Warning())
		}
	}
}

func groupOf(root *cobra.Command, path []string) *cobra.Command {
	parent := root
	for _, word := range path {
		i := slices.IndexFunc(parent.Commands(), func(child *cobra.Command) bool { return child.Name() == word })
		if i >= 0 {
			parent = parent.Commands()[i]
			continue
		}
		group := newCommand(word, requireSubcommand)
		group.Short = "Run the " + word + " commands"
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
		if fault != nil {
			return fault
		}
		if fault := printNode(stdout, renderer, answer); fault != nil {
			fault.AfterWrite = wrote
			return fault
		}
		return nil
	})
	cmd.Args = cobra.ExactArgs(len(command.Args))
	cmd.Annotations = map[string]string{pathIsArgumentNumber: strings.Join(paths, ",")}
	cmd.Short = command.Short
	cmd.Long = command.Long
	if cmd.Long == "" {
		cmd.Long = command.Short
	}
	if command.Example != nil {
		cmd.Long += "\n\n" + example(command.Example)
	}
	for _, flag := range command.Flags {
		values[flag.Name] = declareFlag(cmd, flag)
	}
	return cmd
}

func declareFlag(cmd *cobra.Command, flag script.Flag) func() any {
	flags := cmd.Flags()
	switch flag.Type {
	case script.IntFlag:
		var number int
		flags.IntVar(&number, flag.Name, 0, flag.Usage)
		rejectRepeat(flags.Lookup(flag.Name))
		return func() any { return number }
	case script.BoolFlag:
		var set bool
		flags.BoolVar(&set, flag.Name, false, flag.Usage)
		rejectRepeat(flags.Lookup(flag.Name))
		return func() any { return set }
	case script.StringsFlag:
		value := &choicesValue{choices: flag.Choices}
		flags.Var(value, flag.Name, flag.Usage)
		closedSet(flags.Lookup(flag.Name), flag.Choices)
		return func() any { return value.values }
	}
	value := &choiceValue{choices: flag.Choices}
	flags.Var(value, flag.Name, flag.Usage)
	rejectRepeat(flags.Lookup(flag.Name))
	closedSet(flags.Lookup(flag.Name), flag.Choices)
	return func() any { return value.value }
}

type choiceValue struct {
	choices []string
	value   string
}

func (v *choiceValue) String() string {
	return v.value
}

func (v *choiceValue) Set(text string) error {
	if err := checkChoice(v.choices, text); err != nil {
		return err
	}
	v.value = text
	return nil
}

func (v *choiceValue) Type() string {
	return "string"
}

type choicesValue struct {
	choices []string
	values  []string
}

func (v *choicesValue) String() string {
	return "[" + strings.Join(v.values, ",") + "]"
}

func (v *choicesValue) Set(text string) error {
	if err := checkChoice(v.choices, text); err != nil {
		return err
	}
	v.values = append(v.values, text)
	return nil
}

func (v *choicesValue) Type() string {
	return "strings"
}

func checkChoice(choices []string, text string) error {
	if len(choices) == 0 || slices.Contains(choices, text) {
		return nil
	}
	return errors.New("it is none of " + strings.Join(choices, ", "))
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
		Call: func(ctx context.Context, call func(context.Context, *youtrack.Client) (*youtrack.Node, error)) (*youtrack.Node, *diag.Fault) {
			c, fault := connected()
			if fault != nil {
				return nil, fault
			}
			node, err := call(ctx, c.client)
			if err != nil {
				return nil, c.withLoginSource(diag.FromError(err))
			}
			return node, nil
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
