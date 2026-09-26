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

type connection struct {
	client  *youtrack.Client
	address *url.URL
	from    loginSource
}

type loginSource string

const (
	fromEnvironment loginSource = "environment"
	fromSettings    loginSource = "settings"
)

func (o loginSource) pair() render.Pair {
	return render.Pair{Key: "auth_from", Value: render.NewString(string(o))}
}

const (
	urlVariable   = "YTRACK_URL"
	tokenVariable = "YTRACK_TOKEN"
	homeVariable  = "HOME"
	pwdVariable   = "PWD"

	noLoginFound = "no login was found in the places under looked_in"
)

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
	client, fault := youtrack.New(address, token, cacheDirectory(lookup(env, homeVariable)))
	if fault != nil {
		return connection{}, fault
	}
	return connection{client: client, address: address, from: fromEnvironment}, nil
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
	client, fault := youtrack.New(held.address, held.token, cacheDirectory(home))
	if fault != nil {
		return connection{}, fault
	}
	return connection{client: client, address: held.address, from: fromSettings}, nil
}

func (c connection) withLoginSource(fault *diag.Fault) *diag.Fault {
	if fault.Code != diag.Denied {
		return fault
	}
	fault.Details = append(fault.Details, c.from.pair())
	return fault
}

func noLoginFoundFault(message string, lookedIn ...string) *diag.Fault {
	places := []*render.Node{render.NewString(urlVariable), render.NewString(tokenVariable)}
	for _, place := range lookedIn {
		places = append(places, render.NewString(place))
	}
	details := []render.Pair{{Key: "looked_in", Value: render.NewList(places...)}}
	return &diag.Fault{Code: diag.Denied, Message: message, Details: details}
}

func requotedURLReason(err error) string {
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

func parseAddress(raw, source string) (*url.URL, string) {
	address, err := url.Parse(raw)
	if err != nil {
		var parse *url.Error
		if errors.As(err, &parse) {
			return nil, fmt.Sprintf("%s: %s %s: %s", source, parse.Op, render.Quote(redactedRaw(parse.URL)), requotedURLReason(parse.Err))
		}
		return nil, fmt.Sprintf("%s: %v", source, err)
	}
	if (address.Scheme != "http" && address.Scheme != "https") || address.Host == "" {
		return nil, fmt.Sprintf("%s %s is not an absolute http or https URL", source, render.Quote(redactedRaw(raw)))
	}
	if strings.ContainsAny(raw, "?#") {
		return nil, fmt.Sprintf("%s %s has a query or a fragment", source, render.Quote(redactedRaw(raw)))
	}
	return canonical(address), ""
}

const asciiDelete = 0x7f

func headerForbids(r rune) bool {
	return (r < ' ' && r != '\t') || r == asciiDelete
}

func validateToken(token, source string) string {
	if strings.ContainsFunc(token, headerForbids) {
		return source + " holds a control character, such as a line ending, and a request header cannot carry one"
	}
	return ""
}

const redactedPassword = "xxxxx"

func redactedRaw(raw string) string {
	slashes := strings.Index(raw, "//")
	if slashes < 0 {
		return raw
	}
	authorityStart := slashes + len("//")
	authority := raw[authorityStart:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	userinfoEnd, passwordColon := strings.LastIndex(authority, "@"), strings.Index(authority, ":")
	if userinfoEnd < 0 || passwordColon < 0 || passwordColon > userinfoEnd {
		return raw
	}
	return raw[:authorityStart+passwordColon+1] + redactedPassword + raw[authorityStart+userinfoEnd:]
}

func canonical(address *url.URL) *url.URL {
	spelled := *address
	spelled.Host = strings.ToLower(address.Host)
	escapedPath := address.EscapedPath()
	spelled.RawPath = strings.TrimRight(escapedPath, "/")
	trimmedSlashes := len(escapedPath) - len(spelled.RawPath)
	spelled.Path = address.Path[:len(address.Path)-trimmedSlashes]
	return &spelled
}

func lookup(env []string, name string) string {
	for _, variable := range env {
		if key, value, ok := strings.Cut(variable, "="); ok && key == name {
			return value
		}
	}
	return ""
}
