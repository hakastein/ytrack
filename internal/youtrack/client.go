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

// cache is the directory the metadata of projects is kept in between calls, empty where it is to be kept nowhere.
func New(address *url.URL, token, cache string) *Client {
	return &Client{
		address:    address,
		token:      token,
		httpClient: newHTTPClient(),
		cache:      newMetaCache(cache, address.String(), token),
	}
}
