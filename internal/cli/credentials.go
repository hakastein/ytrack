package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
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

func (o loginSource) pair() youtrack.Pair {
	return youtrack.Pair{Key: "auth_from", Value: youtrack.NewString(string(o))}
}

const (
	urlVariable   = "YTRACK_URL"
	tokenVariable = "YTRACK_TOKEN"
	homeVariable  = "HOME"

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
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

func fromEnvironmentVariables(env []string, raw, token string) (connection, *diag.Fault) {
	address, reason := parseAddress(raw, urlVariable)
	if reason != "" {
		return connection{}, &diag.Fault{Code: youtrack.CodeBadUsage, Message: reason}
	}
	client, err := youtrack.NewClient(address.String(), token, youtrack.WithMetadataCache(cacheDirectory(lookup(env, homeVariable))))
	if err != nil {
		return connection{}, diag.FromError(err)
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
	chain, fault := recordsForWorkingDir(records)
	if fault != nil {
		return connection{}, fault
	}
	if len(chain) == 0 {
		return connection{}, noLoginFoundFault(noLoginFound, string(fromSettings))
	}
	held := chain[0]
	client, err := youtrack.NewClient(held.address.String(), held.token, youtrack.WithMetadataCache(cacheDirectory(home)))
	if err != nil {
		return connection{}, diag.FromError(err)
	}
	return connection{client: client, address: held.address, from: fromSettings}, nil
}

func (c connection) withLoginSource(fault *diag.Fault) *diag.Fault {
	if fault.Code != youtrack.CodeDenied {
		return fault
	}
	fault.Details = append(fault.Details, c.from.pair())
	return fault
}

func noLoginFoundFault(message string, lookedIn ...string) *diag.Fault {
	places := []*youtrack.Node{youtrack.NewString(urlVariable), youtrack.NewString(tokenVariable)}
	for _, place := range lookedIn {
		places = append(places, youtrack.NewString(place))
	}
	details := []youtrack.Pair{{Key: "looked_in", Value: youtrack.NewList(places...)}}
	return &diag.Fault{Code: youtrack.CodeDenied, Message: message, Details: details}
}

// A domain alone is the address of an instance over https; any other spelling is a link ParseAddress takes or refuses.
func parseAddress(raw, source string) (*url.URL, string) {
	spelled := raw
	if !strings.Contains(raw, "://") {
		spelled = "https://" + raw
	}
	address, err := youtrack.ParseAddress(spelled)
	if err != nil {
		return nil, source + " is neither a domain nor a link like https://example.com"
	}
	return canonical(address), ""
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
