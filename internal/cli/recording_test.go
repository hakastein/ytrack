//go:build !contract

package cli_test

import (
	"fmt"
	"net/http"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

const cassetteMode = recorder.ModeReplayOnly

func devTokens(*testing.T) devInstanceTokens {
	return devInstanceTokens{admin: "perm-replay-admin", limited: "perm-replay-limited", member: "perm-replay-member"}
}

func realTransport(t *testing.T) http.RoundTripper {
	return offline{t}
}

type offline struct {
	t *testing.T
}

func (o offline) RoundTrip(r *http.Request) (*http.Response, error) {
	o.t.Errorf("a replay reached for the network: %s %s", r.Method, r.URL)
	return nil, fmt.Errorf("a replay sends nothing to the network: %s %s", r.Method, r.URL)
}
