package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/script"
)

func Run(ctx context.Context, argv, env []string, build *debug.BuildInfo, stdin *os.File, stdout, stderr io.Writer) int {
	renderer := render.YAML{}
	stream := diag.NewStream(stderr, renderer)
	// Given nil, cobra reads the process's own arguments.
	if argv == nil {
		argv = []string{}
	}
	root := newRoot(env, build, stdin, stdout, renderer)
	catalog := loadScripts(root, env)
	addScripts(root, catalog, env, stdout, renderer, stream)
	err := execute(ctx, root, catalog, argv, stdout, stream)
	if err == nil {
		return 0
	}
	var fault *diag.Fault
	if !errors.As(err, &fault) {
		fault = &diag.Fault{Code: youtrack.CodeBadUsage, Message: cobraMessage(err)}
	}
	stream.Fail(fault)
	return fault.ExitCode()
}

// cobra's own __complete reads the process env and writes to its stderr and a debug file.
func execute(ctx context.Context, root *cobra.Command, catalog *script.Catalog, argv []string, stdout io.Writer,
	stream *diag.Stream,
) error {
	if len(argv) > 0 {
		switch argv[0] {
		case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			if fault := complete(root, stdout, argv[0], argv[1:]); fault != nil {
				return fault
			}
			return nil
		}
	}
	warnHidden(root, catalog, argv, stream)
	root.SetArgs(argv)
	return root.ExecuteContext(ctx)
}

func newRoot(env []string, build *debug.BuildInfo, stdin *os.File, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var version bool
	root := newCommand("ytrack", func(cmd *cobra.Command, args []string) *diag.Fault {
		if version {
			return printNode(stdout, renderer, versionNode(build))
		}
		return requireSubcommand(cmd, args)
	})
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentPreRunE = runE(refuseCobraCompletion)
	// cobra's help exits 0 on an unknown topic and lists a hidden "help"; a hidden long name still widens the list.
	help := newCommand("no-help", func(cmd *cobra.Command, _ []string) *diag.Fault {
		return unknownCommand(cmd.Root(), cmd.CalledAs())
	})
	help.Hidden = true
	root.SetHelpCommand(help)
	// SetHelpCommand takes effect only inside ExecuteC, which the completion protocol never reaches.
	root.AddCommand(help)
	root.SetOut(stdout)
	root.SetErr(io.Discard)
	root.SetFlagErrorFunc(runE(func(_ *cobra.Command, err error) *diag.Fault {
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: flagMessage(err)}
	}))
	// A flag of its own: cobra's Version field prints its own template past the renderer and takes -v.
	root.Flags().BoolVar(&version, "version", false, "print the version and the revision this binary was built from")
	root.AddCommand(newAuth(env, stdin, stdout, renderer), newCompletion(stdout))
	return root
}

func refuseCobraCompletion(cmd *cobra.Command, _ []string) *diag.Fault {
	switch cmd.CalledAs() {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return unknownCommand(cmd.Root(), cmd.CalledAs())
	}
	return nil
}

type call func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error)

func connectAndCall(ctx context.Context, env []string, call call) (connection, *youtrack.Node, *diag.Fault) {
	c, fault := connect(env)
	if fault != nil {
		return connection{}, nil, fault
	}
	node, fault := c.call(ctx, call)
	if fault != nil {
		return connection{}, nil, fault
	}
	return c, node, nil
}

func (c connection) call(ctx context.Context, call call) (*youtrack.Node, *diag.Fault) {
	node, err := call(ctx, c.client)
	if err != nil {
		var fault *diag.Fault
		if !errors.As(err, &fault) {
			fault = diag.FromError(err)
		}
		return nil, c.withLoginSource(fault)
	}
	return node, nil
}

func printNode(stdout io.Writer, renderer render.Renderer, node *youtrack.Node) *diag.Fault {
	if err := renderer.Render(stdout, node); err != nil {
		return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: err.Error()}
	}
	return nil
}

func rejectRepeat(flag *pflag.Flag) {
	flag.Value = &onceValue{Value: flag.Value}
}

type onceValue struct {
	pflag.Value
	given bool
}

func (v *onceValue) Set(value string) error {
	if v.given {
		return errors.New("the flag is given more than once")
	}
	v.given = true
	return v.Value.Set(value)
}

func requireSubcommand(cmd *cobra.Command, args []string) *diag.Fault {
	if len(args) == 0 {
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: "no command given"}
	}
	return unknownCommand(cmd, args[0])
}

func flagMessage(err error) string {
	var invalid *pflag.InvalidValueError
	var required *pflag.ValueRequiredError
	var unknown *pflag.NotExistError
	switch {
	case errors.As(err, &invalid):
		flag := "--" + invalid.GetFlag().Name
		if invalid.GetFlag().Shorthand != "" {
			flag = "-" + invalid.GetFlag().Shorthand + ", " + flag
		}
		cause := errors.Unwrap(invalid)
		var number *strconv.NumError
		if errors.As(cause, &number) {
			cause = number.Err
		}
		return fmt.Sprintf("invalid argument %s for %s flag: %v", render.Quote(invalid.GetValue()), render.Quote(flag), cause)
	case errors.As(err, &required) && required.GetSpecifiedShortnames() != "":
		return "flag needs an argument: " + render.Quote(required.GetSpecifiedName()) + " in -" +
			required.GetSpecifiedShortnames()
	case errors.As(err, &unknown) && unknown.GetSpecifiedShortnames() != "":
		return "unknown shorthand flag: " + render.Quote(unknown.GetSpecifiedName()) + " in -" +
			unknown.GetSpecifiedShortnames()
	}
	return err.Error()
}

// cobra reports an unknown word under the root in an untyped error that quotes it with %q.
var cobraUnknownCommand = regexp.MustCompile(`^unknown command ("(?:[^"\\]|\\.)*") for ("(?:[^"\\]|\\.)*")`)

func cobraMessage(err error) string {
	message := err.Error()
	found := cobraUnknownCommand.FindStringSubmatch(message)
	if found == nil {
		return message
	}
	word, wordErr := strconv.Unquote(found[1])
	path, pathErr := strconv.Unquote(found[2])
	if wordErr != nil || pathErr != nil {
		return message
	}
	return "unknown command " + render.Quote(word) + " for " + render.Quote(path) + message[len(found[0]):]
}

func unknownCommand(parent *cobra.Command, word string) *diag.Fault {
	message := fmt.Sprintf("unknown command %s for %s", render.Quote(word), render.Quote(parent.CommandPath()))
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

func newCommand(use string, run func(cmd *cobra.Command, args []string) *diag.Fault) *cobra.Command {
	return &cobra.Command{Use: use, RunE: runE(run)}
}

func runE[T any](f func(cmd *cobra.Command, arg T) *diag.Fault) func(*cobra.Command, T) error {
	return func(cmd *cobra.Command, arg T) error {
		if fault := f(cmd, arg); fault != nil {
			return fault
		}
		return nil
	}
}
