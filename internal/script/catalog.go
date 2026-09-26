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
	Warning *youtrack.Warning
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
		scanned, err := root.scan(".", nil)
		if err != nil {
			catalog.unreadable(root.Dir, err)
		}
		for _, word := range sortedWords(scanned) {
			entry := scanned[word]
			if root.Builtin() && len(entry.broken) > 0 {
				defect(entry.broken[0].Fault)
			}
			switch held := owner[word]; {
			case entry.empty():
			case held == nil:
				owner[word] = root
				catalog.Commands = append(catalog.Commands, entry.commands...)
				catalog.Broken = append(catalog.Broken, entry.broken...)
			case held.Builtin() && !root.Builtin():
				message := fmt.Sprintf("%s is not a command: the command %s is built into ytrack, and no script "+
					"adds to or replaces a builtin command", render.Quote(entry.display), render.Quote(word))
				catalog.Hidden = append(catalog.Hidden, Hidden{Word: word, Warning: fileFault(message, entry.display).Warning()})
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
	if project := projectRoot(places.WorkingDir, user, catalog); project != nil {
		found = append(found, project)
	}
	if user != "" && isDir(user, catalog) {
		found = append(found, &Root{Dir: user, fsys: os.DirFS(user)})
	}
	return found
}

func projectRoot(from, user string, catalog *Catalog) *Root {
	if from == "" {
		return nil
	}
	for dir := from; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, filepath.FromSlash(scriptsDir))
		switch {
		case candidate == user:
			return nil
		case isDir(candidate, catalog):
			return &Root{Dir: candidate, fsys: os.DirFS(candidate)}
		case filepath.Dir(dir) == dir:
			return nil
		}
	}
}

func isDir(dir string, catalog *Catalog) bool {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false
	case err != nil:
		catalog.unreadable(dir, unwrapPath(err))
		return false
	}
	return info.IsDir()
}

func (c *Catalog) unreadable(root string, err error) {
	message := fmt.Sprintf("the root of scripts %s cannot be read: %v", render.Quote(root), err)
	c.Broken = append(c.Broken, Broken{Fault: fileFault(message, root)})
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

func brokenEntry(path []string, display, message string) *entry {
	return &entry{display: display, broken: []Broken{{Path: path, Fault: fileFault(message, display)}}}
}

// A symlink that leads back up would otherwise be followed forever.
const deepestPath = 8

func (r *Root) scan(dir string, words []string) (map[string]*entry, error) {
	listed, err := fs.ReadDir(r.fsys, dir)
	if err != nil {
		return nil, unwrapPath(err)
	}
	files, dirs := map[string]*entry{}, map[string]*entry{}
	for _, item := range listed {
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		at, display := path.Join(dir, name), r.display(path.Join(dir, name))
		info, err := fs.Stat(r.fsys, at)
		word, isScript := strings.CutSuffix(name, ".js")
		here := append(slices.Clip(words), word)
		switch {
		case err != nil:
			files[word] = brokenEntry(here, display, fmt.Sprintf("%s cannot be read: %v", render.Quote(display), unwrapPath(err)))
		case info.IsDir() && len(here) >= deepestPath:
			dirs[name] = brokenEntry(here, display, fmt.Sprintf("directory %s lies %d directories deep, and a command "+
				"path is shorter", render.Quote(display), len(here)))
		case info.IsDir():
			dirs[name] = r.subdir(at, here)
		case info.Mode().IsRegular() && isScript:
			files[word] = r.file(at, here)
		}
	}
	return join(files, dirs, words), nil
}

func (r *Root) subdir(dir string, words []string) *entry {
	display := r.display(dir)
	below, err := r.scan(dir, words)
	if err != nil {
		return brokenEntry(words, display, fmt.Sprintf("directory %s cannot be read: %v", render.Quote(display), err))
	}
	joined := &entry{display: display}
	for _, word := range sortedWords(below) {
		joined.commands = append(joined.commands, below[word].commands...)
		joined.broken = append(joined.broken, below[word].broken...)
	}
	return joined
}

func join(files, dirs map[string]*entry, words []string) map[string]*entry {
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
		joined[word] = brokenEntry(here, script.display, fmt.Sprintf("%s and %s both name the command %s, which is "+
			"either a script or a directory of its subcommands", render.Quote(script.display), render.Quote(below.display),
			render.Quote(strings.Join(here, " "))))
	}
	for word, held := range joined {
		if held.empty() || wordGrammar.MatchString(word) {
			continue
		}
		here := append(slices.Clip(words), word)
		joined[word] = brokenEntry(here, held.display,
			fmt.Sprintf("%s names the command word %s, which is %s", render.Quote(held.display), render.Quote(word), wordRule))
	}
	return joined
}

func (r *Root) file(name string, words []string) *entry {
	display := r.display(name)
	source, err := fs.ReadFile(r.fsys, name)
	if err != nil {
		return brokenEntry(words, display, fmt.Sprintf("%s cannot be read: %v", render.Quote(display), unwrapPath(err)))
	}
	command, err := declaration(display, string(source))
	var failed *unreadable
	switch {
	case errors.As(err, &failed):
		return &entry{display: display, broken: []Broken{{Path: words, Fault: scriptFault(failed.message, failed.at)}}}
	case command == nil:
		return &entry{display: display}
	}
	command.Path, command.root, command.file = words, r, name
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

func (c *Catalog) Warnings(under []string) []*youtrack.Warning {
	var warnings []*youtrack.Warning
	if len(under) == 0 {
		for _, hidden := range c.Hidden {
			warnings = append(warnings, hidden.Warning)
		}
	}
	for _, broken := range c.Broken {
		if len(broken.Path) >= len(under) && slices.Equal(broken.Path[:len(under)], under) {
			warnings = append(warnings, broken.Fault.Warning())
		}
	}
	return warnings
}
