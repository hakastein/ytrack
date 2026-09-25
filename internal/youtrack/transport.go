package youtrack

import (
	"context"
	"net"
	"net/http"
	"time"
)

func newSendOnceHTTPClient() *http.Client {
	var http1Only http.Protocols
	http1Only.SetHTTP1(true)
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			// net/http silently resends a request over HTTP/2 on GOAWAY and over a reused connection that broke.
			Protocols:         &http1Only,
			DisableKeepAlives: true,
		},
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
