package cli

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	// How a refusal names an address or a token nobody will find in a variable or a file: they were typed a
	// moment ago.
	addressTyped = "the address typed"
	tokenTyped   = "the token typed"

	noTokenTyped = "no token was typed, and a login is its token, so nothing was kept"
)

func newAuth(env []string, stdin *os.File, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	status := newCommand("status", func(cmd *cobra.Command, _ []string) *diag.Fault {
		c, user, fault := connectAndCall(cmd.Context(), env, youtrack.CurrentUser)
		if fault != nil {
			return fault
		}
		return printNode(stdout, renderer, statusDocument(c, user))
	})
	status.Args = cobra.ExactArgs(0)
	status.Short = "Show the current login"
	status.Long = "Show the address, the user, and whether the login comes from the environment or from the settings.\n\n" +
		example(render.NewMap(render.Pair{Key: "url", Value: render.NewString("https://youtrack.example.com")},
			fromSettings.pair(),
			render.Pair{Key: "user", Value: render.NewMap(render.Pair{Key: "login", Value: render.NewString("user")}, render.Pair{Key: "fullName", Value: render.NewString("User")})}))

	var loginGlobal bool
	login := newCommand("login", func(cmd *cobra.Command, _ []string) *diag.Fault {
		return login(cmd.Context(), env, stdin, stdout, renderer, loginGlobal)
	})
	login.Args = cobra.ExactArgs(0)
	login.Short = "Log in"
	login.Long = "Log in for this directory and below."
	login.Flags().BoolVar(&loginGlobal, "global", false, "log in everywhere")

	var logoutGlobal bool
	logout := newCommand("logout", func(_ *cobra.Command, _ []string) *diag.Fault {
		return logout(env, stdout, renderer, logoutGlobal)
	})
	logout.Args = cobra.ExactArgs(0)
	logout.Short = "Log out"
	logout.Long = "Log out of this directory. The token stays valid."
	logout.Flags().BoolVar(&logoutGlobal, "global", false, "log out of the global login")

	auth := newCommand("auth", rejectGroup)
	auth.Short = "Manage login"
	auth.AddCommand(status, login, logout)
	return auth
}

// login asks for an address and a token on the terminal and has the server say whose token it is before any of
// it is written down: a token the server would refuse is better refused where it was typed than in some later
// command far from the mistake.
func login(ctx context.Context, env []string, stdin *os.File, stdout io.Writer, renderer render.Renderer, global bool) *diag.Fault {
	tty, fault := terminal(stdin)
	if fault != nil {
		return fault
	}
	defer tty.Close()
	path, kept, fault := loginTarget(env, global, "auth login keeps the login of the directory it was called in, and ")
	if fault != nil {
		return fault
	}
	records, fault := readRecords(path)
	if fault != nil {
		return fault
	}
	spelled, fault := promptAddress(ctx, tty)
	if fault != nil {
		return fault
	}
	address, reason := parseAddress(spelled, addressTyped)
	if reason != "" {
		return &diag.Fault{Code: diag.BadUsage, Message: reason}
	}
	secret, fault := promptToken(ctx, tty)
	if fault != nil {
		return fault
	}
	if secret == "" {
		return &diag.Fault{Code: diag.BadUsage, Message: noTokenTyped}
	}
	if reason := validateToken(secret, tokenTyped); reason != "" {
		return &diag.Fault{Code: diag.BadUsage, Message: reason}
	}
	// The login is checked and nothing else, so this client reads no cache and leaves none behind.
	user, fault := youtrack.CurrentUser(ctx, youtrack.New(address, secret, ""))
	if fault != nil {
		// The token was typed on this run rather than found somewhere, so there is no origin for it to name.
		return fault
	}
	// The records were read before the dialogue; a file changed since is written over, which no lock prevents.
	if fault := saveRecords(path, upsertRecord(records, kept, address, secret)); fault != nil {
		return fault
	}
	return printNode(stdout, renderer, render.NewMap(
		// An address may carry a password, masked here as in the request of a refusal.
		render.Pair{Key: "url", Value: render.NewString(address.Redacted())},
		render.Pair{Key: "scope", Value: render.NewString(kept.String())},
		render.Pair{Key: "user", Value: user},
	))
}

// logout changes the file and nothing else, so it asks for no address and no token of its own.
func logout(env []string, stdout io.Writer, renderer render.Renderer, global bool) *diag.Fault {
	path, kept, fault := loginTarget(env, global, "auth logout takes out the login of the directory it was called in, and ")
	if fault != nil {
		return fault
	}
	records, fault := readRecords(path)
	if fault != nil {
		return fault
	}
	taken, rest, found := removeRecord(records, kept)
	if !found {
		return noSavedLoginFault(records, kept)
	}
	if fault := saveRecords(path, rest); fault != nil {
		return fault
	}
	return printNode(stdout, renderer, render.NewMap(
		// An address may carry a password, masked here as in the request of a refusal.
		render.Pair{Key: "url", Value: render.NewString(taken.address.Redacted())},
		render.Pair{Key: "scope", Value: render.NewString(kept.String())},
	))
}

// loginTarget is the file of login records and the one record of it a call is about: the record of the directory the
// call was made in, or the record for everywhere. Both commands settle it before they act, and wanted is the half of
// the refusal that says what the directory was needed for.
func loginTarget(env []string, global bool, wanted string) (path string, kept scope, fault *diag.Fault) {
	home := lookup(env, homeVariable)
	if path = recordsPath(home); path == "" {
		message := "the saved logins cannot be found: " + homeReason(home)
		return "", scope{}, &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	if global {
		return path, globalScope(), nil
	}
	dir, reason := workingDirectory(env)
	if reason != "" {
		return "", scope{}, &diag.Fault{Code: diag.BadUsage, Message: wanted + reason}
	}
	return path, dirScope(dir), nil
}

// A logout that changed nothing while a token still goes out from here would leave the caller sure they had
// logged out, so the refusal names the login that stays in charge.
func noSavedLoginFault(records []record, kept scope) *diag.Fault {
	if kept.isGlobal() {
		return &diag.Fault{Code: diag.BadUsage, Message: "no global login is saved"}
	}
	message := "no login is saved for " + render.Quote(kept.directory)
	if chain := recordsFor(records, kept.directory); len(chain) > 0 {
		message += ", and the login that applies there is " + describeScope(chain[0])
	}
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func describeScope(held record) string {
	if held.scope.isGlobal() {
		return "the global one"
	}
	return "the one saved for " + render.Quote(held.scope.directory)
}

func statusDocument(c connection, user *render.Node) *render.Node {
	return render.NewMap(
		// An address may carry a password, masked here as in the request of a refusal.
		render.Pair{Key: "url", Value: render.NewString(c.address.Redacted())},
		c.from.pair(),
		render.Pair{Key: "user", Value: user},
	)
}
