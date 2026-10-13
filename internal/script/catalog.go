package script

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hakastein/cliche"
	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/render"
)

//go:embed builtin
var builtinFiles embed.FS

// An empty place is unknown, and no root is looked up from it.
type Places struct {
	WorkingDir string
	Home       string
}

type Catalog struct {
	// Scripts are the commands and the broken paths that enter the tree: a call of a broken path ends in its error,
	// and --help above it warns.
	Scripts *cliche.Catalog
	// Hidden are first words a builtin command holds in a root of scripts: every call under that word warns.
	Hidden []Hidden
	// Roots names the directory of the root whose commands a first word holds; builtin words are absent.
	Roots map[string]string
}

type Hidden struct {
	Word    string
	Warning *youtrack.Warning
}

const appName = "ytrack"

// Load reads the roots by the rule of ytrack: a first word belongs to one root, the builtin one before the project's
// and the project's before the user's. reserved are the first words of the commands written in Go.
func Load(reserved []string, places Places) *Catalog {
	builtin, err := fs.Sub(builtinFiles, "builtin")
	if err != nil {
		panic(err)
	}
	home := places.Home
	if !filepath.IsAbs(home) {
		home = ""
	}
	// Sources nil would make cliche look up the directory and the home of the process itself.
	sources := append([]cliche.Source{}, cliche.DefaultSources(appName, places.WorkingDir, home)...)
	config := cliche.Config{Name: appName, Builtin: builtin, Sources: sources, Types: types()}
	catalog := &Catalog{Scripts: &cliche.Catalog{}, Roots: map[string]string{}}
	reservedRoot := &cliche.Root{Builtin: true}
	owner := map[string]*cliche.Root{}
	for _, word := range reserved {
		owner[word] = reservedRoot
	}
	roots := config.Scan()
	for i := range roots {
		root := &roots[i]
		if root.Err != nil {
			catalog.Scripts.Broken = append(catalog.Scripts.Broken, cliche.Broken{Err: root.Err})
			continue
		}
		for _, word := range root.Words {
			commands, broken := restrict(word)
			if root.Builtin && len(broken) > 0 {
				defect(FaultOf(broken[0].Err))
			}
			switch held := owner[word.Word]; {
			case held == nil:
				owner[word.Word] = root
				catalog.Scripts.Commands = append(catalog.Scripts.Commands, commands...)
				catalog.Scripts.Broken = append(catalog.Scripts.Broken, broken...)
				if !root.Builtin && len(commands) > 0 {
					catalog.Roots[word.Word] = root.Name
				}
			case held.Builtin && !root.Builtin:
				message := fmt.Sprintf("%s is not a command: the command %s is built into ytrack, and no script "+
					"adds to or replaces a builtin command", render.Quote(word.File), render.Quote(word.Word))
				hidden := FaultOf(&cliche.ScriptError{Message: message, File: word.File})
				catalog.Hidden = append(catalog.Hidden, Hidden{Word: word.Word, Warning: hidden.Warning()})
			}
		}
	}
	return catalog
}

var (
	argTypes  = []string{"string", "path", "duration"}
	flagTypes = []string{"string", "int", "bool", fieldsType, "pair", "duration"}
)

// restrict turns a command that declares more than ytrack takes into a broken path: cliche also reads optional,
// multiple and grouped arguments, int and pair arguments and path flags, which no command of ytrack has.
func restrict(word cliche.Word) ([]*cliche.Command, []cliche.Broken) {
	var commands []*cliche.Command
	broken := slices.Clone(word.Broken)
	for _, command := range word.Commands {
		if reason, line, column := refusal(command); reason != "" {
			broken = append(broken, cliche.Broken{Path: command.Path, Err: &cliche.ScriptError{
				Message: "exports.definition " + reason, File: command.File(), Line: line, Column: column}})
			continue
		}
		commands = append(commands, command)
	}
	return commands, broken
}

// refusal names what ytrack does not take and where the declaration writes it.
func refusal(command *cliche.Command) (reason string, line, column int) {
	for _, arg := range command.Args {
		switch {
		case arg.Group != nil:
			return fmt.Sprintf("gives argument %s a group, and an argument is one value", render.Quote(arg.Name)), arg.Line, arg.Column
		case arg.Optional, arg.Multiple:
			return fmt.Sprintf("makes argument %s optional or multiple, and every argument is given once",
				render.Quote(arg.Name)), arg.Line, arg.Column
		case !slices.Contains(argTypes, arg.Type):
			return fmt.Sprintf("gives argument %s the type %s, which is none of %s", render.Quote(arg.Name),
				render.Quote(arg.Type), strings.Join(argTypes, ", ")), arg.Line, arg.Column
		}
	}
	for _, flag := range command.Flags {
		switch {
		case !slices.Contains(flagTypes, flag.Type):
			return fmt.Sprintf("gives --%s the type %s, which is none of %s", flag.Name, render.Quote(flag.Type),
				strings.Join(flagTypes, ", ")), flag.Line, flag.Column
		case flag.Type == fieldsType:
			if text, isText := flag.Default.(string); !isText || strings.TrimSpace(text) == "" {
				return fmt.Sprintf("gives --%s a default that is no fields expression", flag.Name), flag.Line, flag.Column
			}
		}
	}
	return "", 0, 0
}

func (c *Catalog) Warnings(under []string) []*youtrack.Warning {
	var warnings []*youtrack.Warning
	if len(under) == 0 {
		for _, hidden := range c.Hidden {
			warnings = append(warnings, hidden.Warning)
		}
	}
	for _, broken := range c.Scripts.Broken {
		if len(broken.Path) >= len(under) && slices.Equal(broken.Path[:len(under)], under) {
			warnings = append(warnings, FaultOf(broken.Err).Warning())
		}
	}
	return warnings
}
