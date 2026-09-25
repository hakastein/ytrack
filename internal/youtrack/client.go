package youtrack

import (
	"net/http"
	"net/url"
)

type Client struct {
	address    *url.URL
	token      string
	httpClient *http.Client
	cache      metaCache
}

func New(address *url.URL, token, cacheDir string) *Client {
	return &Client{
		address:    address,
		token:      token,
		httpClient: newSendOnceHTTPClient(),
		cache:      newMetaCache(cacheDir, address.String(), token),
	}
}
