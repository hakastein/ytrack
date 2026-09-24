package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// aShell is one of the shells ytrack writes a script for: the name the argument is held to, the way cobra
// writes the script, and the line that loads it. Everything the command says about shells is read off this
// one list, so a shell named here is named in the help and offered by the protocol without a second edit.
type aShell struct {
	name     string
	generate func(root *cobra.Command, stdout io.Writer) error
	install  string
}

// Only the writer forms are called. Their …File counterparts are the one thing in cobra's generators that
// touches the process: they create a file of their own, and nothing here may.
func shells() []aShell {
	return []aShell{{
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

// installs is how the script is loaded, one shell to a line, and it is the only place the help names a shell.
func installs() string {
	var lines strings.Builder
	for _, shell := range shells() {
		fmt.Fprintf(&lines, "  %s: %s\n", shell.name, shell.install)
	}
	return lines.String()
}

// newCompletion prints the script a shell sources to complete ytrack. The script goes to stdout as it stands,
// past the renderer: it is shell source addressed to the shell, not a document addressed to the caller's
// parser. cobra writes it off the very tree that runs the commands, so there is no second copy of the surface
// anywhere — the script holds no list of entities, verbs or flags, it asks the binary for them.
func newCompletion(stdout io.Writer) *cobra.Command {
	completion := newCommand("completion <shell>", func(cmd *cobra.Command, args []string) *diag.Fault {
		for _, shell := range shells() {
			if shell.name != args[0] {
				continue
			}
			if err := shell.generate(cmd.Root(), stdout); err != nil {
				return &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error()}
			}
			return nil
		}
		return &diag.Fault{Code: diag.BadUsage, Message: fmt.Sprintf(
			"unknown shell %s: ytrack has a script for %s", render.Quote(args[0]), strings.Join(shellNames(), ", "))}
	})
	completion.Args = cobra.ExactArgs(1)
	// The set the protocol offers. cobra holds an argument to it only where Args says so, and OnlyValidArgs
	// is not taken: it refuses with words naming no shell to write instead.
	completion.ValidArgs = shellNames()
	completion.Short = "Print a shell completion script"
	completion.Long = "Print a shell completion script. Load it:\n\n" +
		installs() + "\nFor every new shell, put the same line in the file the shell reads at start. zsh needs " +
		"autoload -U compinit; compinit first, bash needs the bash-completion package."
	return completion
}

// The annotation saying where a command takes something no command tree knows: a path. Its value is the place
// of that argument, counted from one, because a command takes a path at one place and readable identifiers at
// the others — attachment create <owner> <path> takes it second. It is read by the completion protocol alone,
// and it is the one place a directive other than NoFileComp comes from.
const completesPathAt = "ytrack.completesPathAt"

// pathIsWrittenAt is the place the annotation names, counted from one, and none where the command takes no
// path at all.
func pathIsWrittenAt(cmd *cobra.Command) int {
	place, err := strconv.Atoi(cmd.Annotations[completesPathAt])
	if err != nil {
		return 0
	}
	return place
}

// What a shell sends when it has nothing to complete at all, which no generated script does: every one of
// them writes the words of the command line after the name of the protocol.
const noCommandLine = "the completion protocol needs the command line to complete"

// complete answers the completion protocol. words is the command line a shell is completing: its last word is
// the one being completed, and the rest stand typed before it. calledAs tells the two names of the protocol
// apart, and the text after a tab is printed for one of them alone.
func complete(root *cobra.Command, stdout io.Writer, calledAs string, words []string) *diag.Fault {
	if len(words) == 0 {
		return &diag.Fault{Code: diag.BadUsage, Message: noCommandLine}
	}
	typed, completing := words[:len(words)-1], words[len(words)-1]
	directive := cobra.ShellCompDirectiveNoFileComp
	var suggestions []string
	// Find takes the flags out of what was typed itself, so neither a flag nor the value after it hides the
	// command, and what it hands back besides the command is the rest of the line. It fails where the line
	// names no command of ytrack, and then there is nothing to offer at all.
	if cmd, rest, err := root.Find(typed); err == nil {
		// --help is cobra's own and joins a command only as it is executed. It is put there first, so that
		// the line is read with the flags the call itself would be read with.
		cmd.InitDefaultHelpFlag()
		written := wordsWritten(cmd, rest)
		switch {
		case strings.HasPrefix(completing, "-"):
			suggestions = append(flagsOf(cmd), joinedValues(cmd, completing)...)
		case written.flagValue:
			// The value of a flag, which is neither an argument nor a flag: only a closed set the binary holds
			// itself is offered there (ADR-0011), and a file name is no value of any flag.
			suggestions = closedSetOf(written.valueOf)
		default:
			suggestions = argumentsOf(cmd, written)
			if written.arguments+1 == pathIsWrittenAt(cmd) {
				// A shell offers file names of its own under every directive but NoFileComp.
				directive = cobra.ShellCompDirectiveDefault
			}
		}
	}
	return printCompletions(stdout, suggestions, completing, calledAs == cobra.ShellCompRequestCmd, directive)
}

// writtenWords is where the word being completed stands, read off the rest of the command line: the words
// under the command found, flags and the values of flags among them.
type writtenWords struct {
	// How many arguments stand written before the word being completed.
	arguments int
	// The word being completed is the value of the flag written before it, and not an argument.
	flagValue bool
	// That flag, and nil where the command has no flag of the name written.
	valueOf *pflag.Flag
	// A flag the command keeps to itself is written. No command under it takes such a flag, so the line is
	// one that command answers itself.
	ownFlag bool
}

// wordsWritten reads the line the way cobra reads it as it runs the call: a flag written as two words takes
// the word after it, one carrying an = takes none, and what is left over are the arguments.
func wordsWritten(cmd *cobra.Command, words []string) writtenWords {
	ownFlags := cmd.LocalNonPersistentFlags()
	var written writtenWords
	for index := 0; index < len(words); index++ {
		word := words[index]
		if word == "--" {
			// A lone -- ends the flags: every word after it is an argument, whatever it begins with.
			written.arguments += len(words) - index - 1
			return written
		}
		isFlag, flag, takesTheNextWord := aFlagWritten(cmd, word)
		switch {
		case !isFlag:
			written.arguments++
		case takesTheNextWord && index == len(words)-1:
			// The flag stands last, so the word being completed is the value it takes.
			written.flagValue = true
			written.valueOf = flag
		case takesTheNextWord:
			// The word after it is that value, and no argument of the command.
			index++
		}
		if flag != nil && ownFlags.Lookup(flag.Name) != nil {
			written.ownFlag = true
		}
	}
	return written
}

// aFlagWritten reads one word of the line as cobra's own Find reads it: whether the word is a flag, which flag
// of the command it names, and whether the value of that flag is the word after it. A word carrying an = carries
// its value with it, a flag the command does not have is read as taking one, and a shorthand takes the word
// after it only where it stands on its own.
func aFlagWritten(cmd *cobra.Command, word string) (isFlag bool, flag *pflag.Flag, takesTheNextWord bool) {
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
		return true, flag, !carriesItsValue && len(word) == 2 && takesAValue(flag)
	}
	return false, nil, false
}

// The annotation of a flag whose value is one of a closed set the binary holds itself — the categories of the
// journal are one — so offering it costs no request. Every other value of a flag is the server's to know.
const completesClosedSet = "ytrack.completesClosedSet"

// closedSet marks the flag as taking one of values.
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

// joinedValues is the closed set of a flag written with an = and its value begun after it, each value offered
// whole with the flag before it, since that is the word the shell replaces.
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

// takesAValue is whether the word after a flag is the value of it. A flag pflag gives a value of its own where
// it is written alone — a boolean — takes no word; a flag the command does not have takes one, since there is
// nothing saying it does not.
func takesAValue(flag *pflag.Flag) bool {
	return flag == nil || flag.NoOptDefVal == ""
}

// flagsOf is the flags a command takes, its own and the ones it inherits, --help among them: it is a flag the
// caller may write like any other.
func flagsOf(cmd *cobra.Command) []string {
	var suggestions []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		// pflag writes the name of a flag's value in backquotes inside the usage, and UnquoteUsage is what
		// takes them out; without it the quotes themselves would reach the shell.
		_, usage := pflag.UnquoteUsage(flag)
		suggestions = append(suggestions, "--"+flag.Name+"\t"+usage)
	})
	return suggestions
}

// argumentsOf is what may stand where the caller is completing: the values of a closed set, where the command
// names one and takes an argument at that place, and the commands under it, which stand right after its own
// name and nowhere else. An identifier is never among them — no TAB of a shell sends a request.
func argumentsOf(cmd *cobra.Command, written writtenWords) []string {
	var suggestions []string
	if takesAnArgumentAt(cmd, written.arguments) {
		suggestions = append(suggestions, cmd.ValidArgs...)
	}
	// Past an argument already written, and past a flag the command keeps to itself, no command under it can
	// answer the line: what stands there is an argument of the command found.
	if written.arguments > 0 || written.ownFlag {
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

// takesAnArgumentAt asks the very judge of the call whether a word may stand at that place. Every command of
// ytrack counts its arguments and reads none of them — cobra.ExactArgs and nothing else — so words standing in
// for the ones written answer the question.
func takesAnArgumentAt(cmd *cobra.Command, place int) bool {
	return cmd.ValidateArgs(make([]string, place+1)) == nil
}

// printCompletions is the form the generated script parses: one suggestion to a line, its text after a tab,
// and a last line of a colon and the directive. Only what the caller has begun to write is offered back.
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
		return &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error()}
	}
	return nil
}
