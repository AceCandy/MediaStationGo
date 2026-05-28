//go:build !windows

package service

import (
	"net/http"
	"net/url"
)

func systemProxyForRequest(_ *http.Request) (*url.URL, error) {
	return nil, nil
}
