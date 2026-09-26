package script

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dop251/goja/file"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

//go:embed builtin
var builtinFiles embed.FS

type Root struct {
	// Dir is empty for the scripts built into ytrack.
	Dir  string
	fsys fs.FS
}

func (r *Root) Builtin() bool {
	return r.Dir == ""
}

func (r *Root) display(name string) string {
	if r.Builtin() {
		return "builtin:" + name
	}
	return filepath.Join(r.Dir, filepath.FromSlash(name))
}

// An empty place is unknown, and no root is looked up from it.
type Places struct {
	WorkingDir string
	Home       string
}

type Catalog struct {
	Commands []*Command
	// Broken are paths that enter no command tree: a call of one ends in its fault, and --help above it warns.
	Broken []Broken
	// Hidden are first words a builtin command holds in a root of scripts: every call under that word warns.
	Hidden []Hidden
}

type Broken struct {
	Path  []string
	Fault *diag.Fault
}

type Hidden struct {
	Word    string
	Warning *diag.Fault
}

const scriptsDir = ".ytrack/scripts"

// reserved are the first words of the commands written in Go.
func Load(reserved []string, places Places) *Catalog {
	builtinRoot, err := fs.Sub(builtinFiles, "builtin")
	if err != nil {
		panic(err)
	}
	catalog := &Catalog{}
	owner := map[string]*Root{}
	builtin := &Root{fsys: builtinRoot}
	for _, word := range reserved {
		owner[word] = builtin
	}
	for _, root := range roots(builtin, places, catalog) {
		scanned := (&scanner{root: root}).dir(".", nil)
		for _, word := range sortedWords(scanned) {
			entry := scanned[word]
			switch held := owner[word]; {
			case held == nil:
				owner[word] = root
				catalog.Commands = append(catalog.Commands, entry.commands...)
				catalog.Broken = append(catalog.Broken, entry.broken...)
			case held.Builtin() && !root.Builtin():
				message := fmt.Sprintf("%s is not a command: the command %s is built into ytrack, and no script "+
					"adds to or replaces a builtin command", render.Quote(entry.display), render.Quote(word))
				catalog.Hidden = append(catalog.Hidden, Hidden{Word: word, Warning: fileFault(message, entry.display)})
			}
		}
	}
	return catalog
}

func roots(builtin *Root, places Places, catalog *Catalog) []*Root {
	found := []*Root{builtin}
	var user string
	if filepath.IsAbs(places.Home) {
		user = filepath.Join(places.Home, filepath.FromSlash(scriptsDir))
	}
	if places.WorkingDir != "" {
		for dir := places.WorkingDir; ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, filepath.FromSlash(scriptsDir))
			if candidate == user {
				break
			}
			if isDir(candidate, catalog) {
				found = append(found, &Root{Dir: candidate, fsys: os.DirFS(candidate)})
				break
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	if user != "" && isDir(user, catalog) {
		found = append(found, &Root{Dir: user, fsys: os.DirFS(user)})
	}
	return found
}

func isDir(dir string, catalog *Catalog) bool {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false
	case err != nil:
		catalog.Broken = append(catalog.Broken, Broken{Fault: fileFault(fmt.Sprintf("the root of scripts %s "+
			"cannot be read: %v", render.Quote(dir), unwrapPath(err)), dir)})
		return false
	}
	return info.IsDir()
}

// entry is everything a root holds at one path and below it.
type entry struct {
	display  string
	commands []*Command
	broken   []Broken
}

func (e *entry) empty() bool {
	return len(e.commands) == 0 && len(e.broken) == 0
}

type scanner struct {
	root *Root
}

// A symlink that leads back up would otherwise be followed forever.
const deepestPath = 8

func (s *scanner) dir(dir string, words []string) map[string]*entry {
	listed, err := fs.ReadDir(s.root.fsys, dir)
	if err != nil {
		display := s.root.display(dir)
		message := fmt.Sprintf("directory %s cannot be read: %v", render.Quote(display), unwrapPath(err))
		return map[string]*entry{"": {broken: []Broken{{Path: words, Fault: fileFault(message, display)}}}}
	}
	files, dirs := map[string]*entry{}, map[string]*entry{}
	for _, item := range listed {
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		at := path.Join(dir, name)
		info, err := fs.Stat(s.root.fsys, at)
		word, isScript := strings.CutSuffix(name, ".js")
		here := append(slices.Clip(words), word)
		switch {
		case err != nil:
			display := s.root.display(at)
			message := fmt.Sprintf("%s cannot be read: %v", render.Quote(display), unwrapPath(err))
			files[word] = &entry{display: display, broken: []Broken{{Path: here, Fault: fileFault(message, display)}}}
		case info.IsDir() && len(here) >= deepestPath:
			display := s.root.display(at)
			message := fmt.Sprintf("directory %s lies %d directories deep, and a command path is shorter",
				render.Quote(display), len(here))
			dirs[name] = &entry{display: display, broken: []Broken{{Path: here, Fault: fileFault(message, display)}}}
		case info.IsDir():
			dirs[name] = s.flatten(s.dir(at, here), s.root.display(at))
		case info.Mode().IsRegular() && isScript:
			files[word] = s.file(at, here)
		}
	}
	return s.join(files, dirs, words)
}

func (s *scanner) flatten(below map[string]*entry, display string) *entry {
	joined := &entry{display: display}
	for _, word := range sortedWords(below) {
		joined.commands = append(joined.commands, below[word].commands...)
		joined.broken = append(joined.broken, below[word].broken...)
	}
	return joined
}

func (s *scanner) join(files, dirs map[string]*entry, words []string) map[string]*entry {
	joined := map[string]*entry{}
	for word, script := range files {
		joined[word] = script
	}
	for word, below := range dirs {
		script, clash := joined[word]
		if !clash {
			joined[word] = below
			continue
		}
		if script.empty() && below.empty() {
			continue
		}
		here := append(slices.Clip(words), word)
		message := fmt.Sprintf("%s and %s both name the command %s, which is either a script or a directory of "+
			"its subcommands", render.Quote(script.display), render.Quote(below.display), render.Quote(strings.Join(here, " ")))
		joined[word] = &entry{display: below.display, broken: []Broken{{Path: here, Fault: fileFault(message, script.display)}}}
	}
	for word, held := range joined {
		if held.empty() || wordGrammar.MatchString(word) {
			continue
		}
		here := append(slices.Clip(words), word)
		message := fmt.Sprintf("%s names the command word %s, which is not lowercase letters, digits, - and _",
			render.Quote(held.display), render.Quote(word))
		joined[word] = &entry{display: held.display, broken: []Broken{{Path: here, Fault: fileFault(message, held.display)}}}
	}
	return joined
}

func (s *scanner) file(name string, words []string) *entry {
	display := s.root.display(name)
	source, err := fs.ReadFile(s.root.fsys, name)
	if err != nil {
		message := fmt.Sprintf("%s cannot be read: %v", render.Quote(display), unwrapPath(err))
		return &entry{display: display, broken: []Broken{{Path: words, Fault: fileFault(message, display)}}}
	}
	command, err := declaration(display, string(source))
	var failed *unreadable
	switch {
	case errors.As(err, &failed):
		return &entry{display: display, broken: []Broken{{Path: words, Fault: scriptFault(failed.message, failed.at)}}}
	case command == nil:
		return &entry{display: display}
	}
	command.Path, command.root, command.file = words, s.root, name
	return &entry{display: display, commands: []*Command{command}}
}

func sortedWords(entries map[string]*entry) []string {
	words := make([]string, 0, len(entries))
	for word := range entries {
		words = append(words, word)
	}
	slices.Sort(words)
	return words
}

func fileFault(message, display string) *diag.Fault {
	return scriptFault(message, file.Position{Filename: display})
}

func (c *Catalog) Warnings(under []string) []*youtrack.Warning {
	var warnings []*youtrack.Warning
	if len(under) == 0 {
		for _, hidden := range c.Hidden {
			warnings = append(warnings, hidden.Warning.Warning())
		}
	}
	for _, broken := range c.Broken {
		if len(broken.Path) >= len(under) && slices.Equal(broken.Path[:len(under)], under) {
			warnings = append(warnings, broken.Fault.Warning())
		}
	}
	return warnings
}
