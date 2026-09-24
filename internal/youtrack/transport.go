package youtrack

import (
	"context"
	"net"
	"net/http"
	"time"
)

func newHTTPClient() *http.Client {
	// Only HTTP/1.1: the HTTP/2 transport of net/http sends a request again by itself on a GOAWAY or a refused stream.
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	return &http.Client{
		Transport: &http.Transport{
			// Proxy variables belong to the process's environment, which reaches ytrack only through Run.
			Proxy:                 nil,
			Protocols:             &protocols,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			// On a reused connection that breaks before the answer net/http sends the request again by itself.
			DisableKeepAlives: true,
		},
		// A followed redirect is a request nobody asked for, and net/http sends a POST on as a GET.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *Client) authorize(_ context.Context, request *http.Request) error {
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	return nil
}
