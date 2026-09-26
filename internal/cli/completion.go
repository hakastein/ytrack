package cli

import (
	"github.com/hakastein/go-youtrack"

	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

type shell struct {
	name     string
	generate func(root *cobra.Command, stdout io.Writer) error
	install  string
}

func shells() []shell {
	return []shell{{
		name:     "bash",
		generate: func(root *cobra.Command, stdout io.Writer) error { return root.GenBashCompletionV2(stdout, true) },
		install:  "source <(ytrack completion bash)",
	}, {
		name:     "zsh",
		generate: func(root *cobra.Command, stdout io.Writer) error { return root.GenZshCompletion(stdout) },
		install:  "source <(ytrack completion zsh)",
	}, {
		name:     "fish",
		generate: func(root *cobra.Command, stdout io.Writer) error { return root.GenFishCompletion(stdout, true) },
		install:  "ytrack completion fish | source",
	}, {
		name:     "powershell",
		generate: func(root *cobra.Command, stdout io.Writer) error { return root.GenPowerShellCompletionWithDesc(stdout) },
		install:  "ytrack completion powershell | Out-String | Invoke-Expression",
	}}
}

func shellNames() []string {
	names := make([]string, 0, len(shells()))
	for _, shell := range shells() {
		names = append(names, shell.name)
	}
	return names
}

func installHelp() string {
	var lines strings.Builder
	for _, shell := range shells() {
		fmt.Fprintf(&lines, "  %s: %s\n", shell.name, shell.install)
	}
	return lines.String()
}

func newCompletion(stdout io.Writer) *cobra.Command {
	completion := newCommand("completion <shell>", func(cmd *cobra.Command, args []string) *diag.Fault {
		for _, shell := range shells() {
			if shell.name != args[0] {
				continue
			}
			if err := shell.generate(cmd.Root(), stdout); err != nil {
				return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: err.Error()}
			}
			return nil
		}
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: fmt.Sprintf(
			"unknown shell %s: ytrack has a script for %s", render.Quote(args[0]), strings.Join(shellNames(), ", "))}
	})
	completion.Args = cobra.ExactArgs(1)
	// cobra checks ValidArgs only under Args = OnlyValidArgs, so here it feeds completion alone.
	completion.ValidArgs = shellNames()
	completion.Short = "Print a shell completion script"
	completion.Long = "Print a shell completion script. Load it:\n\n" +
		installHelp() + "\nFor every new shell, put the same line in the file the shell reads at start. zsh needs " +
		"autoload -U compinit; compinit first, bash needs the bash-completion package."
	return completion
}

const pathIsArgumentNumber = "ytrack.completesPathAt"

func pathArgumentNumber(cmd *cobra.Command) int {
	number, err := strconv.Atoi(cmd.Annotations[pathIsArgumentNumber])
	if err != nil {
		return 0
	}
	return number
}

const (
	shellOffersNoFileNames = cobra.ShellCompDirectiveNoFileComp
	shellOffersFileNames   = cobra.ShellCompDirectiveDefault
)

const noCommandLine = "the completion protocol needs the command line to complete"

func complete(root *cobra.Command, stdout io.Writer, calledAs string, words []string) *diag.Fault {
	if len(words) == 0 {
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: noCommandLine}
	}
	typed, completing := words[:len(words)-1], words[len(words)-1]
	directive := shellOffersNoFileNames
	var suggestions []string
	if cmd, argsAndFlags, err := root.Find(typed); err == nil {
		cmd.InitDefaultHelpFlag()
		written := parseLine(cmd, argsAndFlags)
		switch {
		case strings.HasPrefix(completing, "-"):
			suggestions = append(flagsOf(cmd), joinedValues(cmd, completing)...)
		case written.flagValue:
			suggestions = closedSetOf(written.valueOf)
		default:
			suggestions = argumentsOf(cmd, written)
			if completingArgument := written.arguments + 1; completingArgument == pathArgumentNumber(cmd) {
				directive = shellOffersFileNames
			}
		}
	}
	return printCompletions(stdout, suggestions, completing, calledAs == cobra.ShellCompRequestCmd, directive)
}

type lineState struct {
	arguments        int
	flagValue        bool
	valueOf          *pflag.Flag
	localFlagWritten bool
}

func (s lineState) subcommandCanFollow() bool {
	return s.arguments == 0 && !s.localFlagWritten
}

func parseLine(cmd *cobra.Command, words []string) lineState {
	localFlags := cmd.LocalNonPersistentFlags()
	var written lineState
	for index := 0; index < len(words); index++ {
		word := words[index]
		if word == "--" {
			written.arguments += len(words) - index - 1
			return written
		}
		isFlag, flag, takesTheNextWord := parseFlagWord(cmd, word)
		switch {
		case !isFlag:
			written.arguments++
		case takesTheNextWord && index == len(words)-1:
			written.flagValue = true
			written.valueOf = flag
		case takesTheNextWord:
			index++
		}
		if flag != nil && localFlags.Lookup(flag.Name) != nil {
			written.localFlagWritten = true
		}
	}
	return written
}

func parseFlagWord(cmd *cobra.Command, word string) (isFlag bool, flag *pflag.Flag, takesTheNextWord bool) {
	switch {
	case strings.HasPrefix(word, "--"):
		name, _, carriesItsValue := strings.Cut(word[2:], "=")
		flag = cmd.Flags().Lookup(name)
		return true, flag, !carriesItsValue && takesAValue(flag)
	case strings.HasPrefix(word, "-") && len(word) > 1:
		name, _, carriesItsValue := strings.Cut(word[1:], "=")
		if name != "" {
			flag = cmd.Flags().ShorthandLookup(name[:1])
		}
		standsAlone := len(word) == len("-x")
		return true, flag, !carriesItsValue && standsAlone && takesAValue(flag)
	}
	return false, nil, false
}

const completesClosedSet = "ytrack.completesClosedSet"

func closedSet(flag *pflag.Flag, values []string) {
	if flag.Annotations == nil {
		flag.Annotations = map[string][]string{}
	}
	flag.Annotations[completesClosedSet] = values
}

func closedSetOf(flag *pflag.Flag) []string {
	if flag == nil {
		return nil
	}
	return flag.Annotations[completesClosedSet]
}

func joinedValues(cmd *cobra.Command, completing string) []string {
	name, _, carriesItsValue := strings.Cut(strings.TrimPrefix(completing, "--"), "=")
	if !carriesItsValue || !strings.HasPrefix(completing, "--") {
		return nil
	}
	var suggestions []string
	for _, value := range closedSetOf(cmd.Flags().Lookup(name)) {
		suggestions = append(suggestions, "--"+name+"="+value)
	}
	return suggestions
}

// cobra's Find also takes an unknown flag to consume the next word.
func takesAValue(flag *pflag.Flag) bool {
	return flag == nil || flag.NoOptDefVal == ""
}

func flagsOf(cmd *cobra.Command) []string {
	var suggestions []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		_, usage := pflag.UnquoteUsage(flag)
		suggestions = append(suggestions, "--"+flag.Name+"\t"+usage)
	})
	return suggestions
}

func argumentsOf(cmd *cobra.Command, written lineState) []string {
	var suggestions []string
	if takesAnArgumentAt(cmd, written.arguments) {
		suggestions = append(suggestions, cmd.ValidArgs...)
	}
	if !written.subcommandCanFollow() {
		return suggestions
	}
	for _, child := range cmd.Commands() {
		if !child.IsAvailableCommand() {
			continue
		}
		suggestions = append(suggestions, child.Name()+"\t"+child.Short)
	}
	return suggestions
}

func takesAnArgumentAt(cmd *cobra.Command, place int) bool {
	return cmd.ValidateArgs(make([]string, place+1)) == nil
}

func printCompletions(stdout io.Writer, suggestions []string, prefix string, describe bool,
	directive cobra.ShellCompDirective,
) *diag.Fault {
	var answer strings.Builder
	for _, suggestion := range suggestions {
		name, text, _ := strings.Cut(suggestion, "\t")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if describe && text != "" {
			name += "\t" + text
		}
		answer.WriteString(name + "\n")
	}
	fmt.Fprintf(&answer, ":%d\n", directive)
	if _, err := io.WriteString(stdout, answer.String()); err != nil {
		return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: err.Error()}
	}
	return nil
}
