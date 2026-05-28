//go:build !windows

package helper

import (
	"net/http"
	"net/url"
)

func systemProxyForRequest(_ *http.Request) (*url.URL, error) {
	return nil, nil
}
