package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const authCommand = "auth"

// limitCommands leaves in the tree only what the saved login lists, and auth. A call of auth sees the whole tree:
// auth login checks --command against it, and auth stays usable while the record names no command.
func limitCommands(root *cobra.Command, env, argv []string) *diag.Fault {
	// Half of the environment is refused by connect, so the record never applies when either variable is set.
	if lookup(env, urlVariable) != "" || lookup(env, tokenVariable) != "" || callsAuth(root, argv) {
		return nil
	}
	held, found, fault := savedLogin(lookup(env, homeVariable))
	if fault != nil || !found || held.commands == nil {
		return fault
	}
	if fault := checkCommands(root, held.commands); fault != nil {
		return fault
	}
	prune(root, held.commands)
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
		if commandAt(root, path) == nil {
			message := "the login lists " + render.Quote(path) + ", which is no command of ytrack here"
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

// prune keeps a listed command with everything under it, the commands on the way to it, auth and hidden ones.
func prune(parent *cobra.Command, paths []string) {
	for _, child := range parent.Commands() {
		path := commandPath(child)
		switch {
		case child.Hidden || path == authCommand || slices.Contains(paths, path):
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
