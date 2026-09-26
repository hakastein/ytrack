package youtrack

import (
	"net/url"

	yt "github.com/hakastein/youtrack"

	"github.com/hakastein/ytrack/internal/diag"
)

type Client struct {
	address *url.URL
	module  *yt.Client
}

func New(address *url.URL, token, cacheDir string) (*Client, *diag.Fault) {
	module, err := yt.New(address.String(), token, yt.WithMetadataCache(cacheDir))
	if err != nil {
		return nil, moduleFailure(err)
	}
	return &Client{address: address, module: module}, nil
}
