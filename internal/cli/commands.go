package cli

import (
	"slices"
	"strings"

	"github.com/hakastein/cliche"
	"github.com/spf13/cobra"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/script"
)

const (
	authCommand = "auth"
	authStatus  = authCommand + " status"
)

// limitCommands leaves in the tree only what the saved login lists, and auth status. A call of auth is not refused for
// the record: auth status shows the login whose record lists a command that is gone.
func limitCommands(root *cobra.Command, catalog *script.Catalog, env, argv []string) *diag.Fault {
	if loginInEnvironment(env) {
		return nil
	}
	held, found, _, fault := savedLogin(lookup(env, homeVariable))
	auth := callsAuth(root, argv)
	switch {
	case fault != nil && !auth:
		return fault
	case fault != nil || !found || held.commands == nil:
		return nil
	}
	if !auth {
		if fault := checkCommands(root, held.commands); fault != nil {
			return fault
		}
	}
	prune(root, held.commands)
	// --help warns of the broken paths of the catalog, and their stubs are out of the tree now.
	catalog.Scripts.Broken = slices.DeleteFunc(catalog.Scripts.Broken, func(broken cliche.Broken) bool { return len(broken.Path) > 0 })
	return nil
}

func callsAuth(root *cobra.Command, argv []string) bool {
	if len(argv) > 0 && (argv[0] == cobra.ShellCompRequestCmd || argv[0] == cobra.ShellCompNoDescRequestCmd) {
		// The last word is still being typed: "auth" alone may yet become another command.
		argv = argv[1:max(1, len(argv)-1)]
	}
	called, _, err := root.Find(argv)
	word, _, _ := strings.Cut(commandPath(called), " ")
	return err == nil && word == authCommand
}

func checkCommands(root *cobra.Command, paths []string) *diag.Fault {
	if path := repeated(paths); path != "" {
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: "the login lists " + render.Quote(path) + " twice"}
	}
	for _, path := range paths {
		if command := commandAt(root, path); command == nil || command.HasAvailableSubCommands() {
			message := "the login lists " + render.Quote(path) + ", which is no command ytrack runs here"
			return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		}
	}
	return nil
}

func repeated(paths []string) string {
	for i, path := range paths {
		if slices.Contains(paths[:i], path) {
			return path
		}
	}
	return ""
}

func commandAt(root *cobra.Command, path string) *cobra.Command {
	at := root
	for _, word := range strings.Split(path, " ") {
		i := slices.IndexFunc(at.Commands(), func(child *cobra.Command) bool { return !child.Hidden && child.Name() == word })
		if i < 0 {
			return nil
		}
		at = at.Commands()[i]
	}
	return at
}

// Hidden commands go too: cobra adds the help command back in ExecuteC, and completion offers no hidden one.
func prune(parent *cobra.Command, paths []string) {
	for _, child := range parent.Commands() {
		path := commandPath(child)
		switch {
		case path == authCommand:
			// A limited login must not change the record that limits it.
			prune(child, []string{authStatus})
		case slices.Contains(paths, path):
		case slices.ContainsFunc(paths, func(listed string) bool { return strings.HasPrefix(listed, path+" ") }):
			prune(child, paths)
		default:
			parent.RemoveCommand(child)
		}
	}
}

func commandPath(cmd *cobra.Command) string {
	_, path, _ := strings.Cut(cmd.CommandPath(), " ")
	return path
}
