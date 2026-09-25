package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

// A connection names where the login came from; the token itself is only inside the client, which cli cannot read,
// so no document can carry it.
type connection struct {
	client  *youtrack.Client
	address *url.URL
	from    origin
}

// An origin is where a login came from, whole. There is no third state: an address from one of these and a token
// from the other is what connect refuses rather than resolves.
type origin string

const (
	fromEnvironment origin = "environment"
	fromSettings    origin = "settings"
)

// A document names the origin and never the place inside it — which variable, which file, which record — because
// that place is ytrack's own arrangement, and what a caller does about a wrong login is the same either way.
func (o origin) pair() render.Pair {
	return render.Pair{Key: "auth_from", Value: render.NewString(string(o))}
}

const (
	urlVariable   = "YTRACK_URL"
	tokenVariable = "YTRACK_TOKEN"
	homeVariable  = "HOME"
	pwdVariable   = "PWD"

	noLoginFound = "no login was found in the places under looked_in"
)

// connect is the one place a command gets YouTrack from, and env the only place the address, the token and the
// home directory holding the saved logins are read from. The two sources are taken whole and never mixed: a token
// always travels with the address it was written down beside, so no resolution of ours can send a development
// token to production.
func connect(env []string) (connection, *diag.Fault) {
	raw, token := lookup(env, urlVariable), lookup(env, tokenVariable)
	switch {
	case raw != "" && token != "":
		return fromEnvironmentVariables(env, raw, token)
	case raw != "":
		return connection{}, partialEnvFault(urlVariable, tokenVariable)
	case token != "":
		return connection{}, partialEnvFault(tokenVariable, urlVariable)
	}
	return fromSavedLogin(env)
}

// One variable set and not the other is refused rather than filled in from the settings: an address alone would
// otherwise be dropped without a word and the call would go to the instance the caller had just overridden.
func partialEnvFault(set, unset string) *diag.Fault {
	message := fmt.Sprintf("%s is set and %s is not, and an address and a token are taken together: set both to work from the environment, or neither to work from the settings", set, unset)
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func fromEnvironmentVariables(env []string, raw, token string) (connection, *diag.Fault) {
	address, reason := parseAddress(raw, urlVariable)
	if reason != "" {
		return connection{}, &diag.Fault{Code: diag.BadUsage, Message: reason}
	}
	if reason := validateToken(token, tokenVariable); reason != "" {
		return connection{}, &diag.Fault{Code: diag.BadUsage, Message: reason}
	}
	return connection{
		client:  youtrack.New(address, token, cacheDirectory(lookup(env, homeVariable))),
		address: address,
		from:    fromEnvironment,
	}, nil
}

func fromSavedLogin(env []string) (connection, *diag.Fault) {
	home := lookup(env, homeVariable)
	path := recordsPath(home)
	if path == "" {
		return connection{}, noLoginFoundFault(noLoginFound + ", and the saved logins were not read: " + homeReason(home))
	}
	records, fault := readRecords(path)
	if fault != nil {
		return connection{}, fault
	}
	chain, fault := recordsForWorkingDir(records, env)
	if fault != nil {
		return connection{}, fault
	}
	if len(chain) == 0 {
		return connection{}, noLoginFoundFault(noLoginFound, string(fromSettings))
	}
	held := chain[0]
	return connection{
		client:  youtrack.New(held.address, held.token, cacheDirectory(home)),
		address: held.address,
		from:    fromSettings,
	}, nil
}

// withOrigin is what the server said with the origin of the login added: the server knows nothing about it, and with
// two sources and a record per directory the caller is not to hold in mind which one answered either.
func (c connection) withOrigin(fault *diag.Fault) *diag.Fault {
	// Every other code is about what was asked for, not about who asked.
	if fault.Code != diag.Denied {
		return fault
	}
	fault.Details = append(fault.Details, c.from.pair())
	return fault
}

// noLoginFoundFault names every place a login could have come from, in the order they were looked at. Where the settings were
// not read at all the message says why, so that a caller who has a saved login does not go looking for the mistake
// there.
func noLoginFoundFault(message string, lookedIn ...string) *diag.Fault {
	places := []*render.Node{render.NewString(urlVariable), render.NewString(tokenVariable)}
	for _, place := range lookedIn {
		places = append(places, render.NewString(place))
	}
	details := []render.Pair{{Key: "looked_in", Value: render.NewList(places...)}}
	return &diag.Fault{Code: diag.Denied, Message: message, Details: details}
}

// The two errors of net/url that quote what they found quote it with %q; the rest quote nothing.
func urlReason(err error) string {
	var escape url.EscapeError
	var host url.InvalidHostError
	switch {
	case errors.As(err, &escape):
		return "invalid URL escape " + render.Quote(string(escape))
	case errors.As(err, &host):
		return "invalid character " + render.Quote(string(host)) + " in host name"
	}
	return err.Error()
}

// parseAddress is raw in the one spelling ytrack uses, or the reason the place named by source cannot reach an
// instance with it.
func parseAddress(raw, source string) (*url.URL, string) {
	address, err := url.Parse(raw)
	if err != nil {
		// url.Error prints the address it was given, and a password in it is no more for printing than a token.
		var parse *url.Error
		if errors.As(err, &parse) {
			return nil, fmt.Sprintf("%s: %s %s: %s", source, parse.Op, render.Quote(mask(parse.URL)), urlReason(parse.Err))
		}
		return nil, fmt.Sprintf("%s: %v", source, err)
	}
	if (address.Scheme != "http" && address.Scheme != "https") || address.Host == "" {
		return nil, fmt.Sprintf("%s %s is not an absolute http or https URL", source, render.Quote(mask(raw)))
	}
	// Requests are resolved against the address, which drops its query and fragment unsaid. Unescaped, ? and #
	// only start them, and the parsed address keeps no trace of an empty fragment.
	if strings.ContainsAny(raw, "?#") {
		return nil, fmt.Sprintf("%s %s has a query or a fragment", source, render.Quote(mask(raw)))
	}
	return canonical(address), ""
}

// validateToken is the reason the place named by source holds a token no request can carry, or "" when it holds one
// that can. net/http refuses a header with a control character other than a tab, and its refusal arrives as a
// transport failure, which tells the caller to run the command again — which will not help.
func validateToken(token, source string) string {
	if strings.ContainsFunc(token, func(r rune) bool { return (r < ' ' && r != '\t') || r == 0x7f }) {
		return source + " holds a control character, such as a line ending, and a request header cannot carry one"
	}
	return ""
}

// mask is (*url.URL).Redacted for text that may not parse at all, which is where a refusal quotes what it was
// given.
func mask(raw string) string {
	slashes := strings.Index(raw, "//")
	if slashes < 0 {
		return raw
	}
	authority := raw[slashes+2:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	// The userinfo runs to the last @ of the authority, and its password starts at the first colon; a colon after
	// the @ is the port.
	at, colon := strings.LastIndex(authority, "@"), strings.Index(authority, ":")
	if at < 0 || colon < 0 || colon > at {
		return raw
	}
	return raw[:slashes+2+colon+1] + "xxxxx" + raw[slashes+2+at:]
}

// The host is read regardless of case and requests are joined to the path whatever slashes end it, so neither is
// part of the spelling; url.Parse has already put the scheme in lower case.
func canonical(address *url.URL) *url.URL {
	spelled := *address
	spelled.Host = strings.ToLower(address.Host)
	// An escaped slash at the end belongs to the last segment, so only the slashes written as such go.
	escaped := address.EscapedPath()
	spelled.RawPath = strings.TrimRight(escaped, "/")
	spelled.Path = address.Path[:len(address.Path)-len(escaped)+len(spelled.RawPath)]
	return &spelled
}

// A variable set to nothing counts as unset: VAR= is how a shell clears one for a single call.
func lookup(env []string, name string) string {
	for _, variable := range env {
		if key, value, ok := strings.Cut(variable, "="); ok && key == name {
			return value
		}
	}
	return ""
}
