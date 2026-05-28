package helper

import (
	"net/http"
	"net/url"
	"strings"
)

// ProxyFromEnvironmentOrSystem mirrors http.ProxyFromEnvironment, then falls
// back to the current user's OS proxy settings where supported.
func ProxyFromEnvironmentOrSystem(req *http.Request) (*url.URL, error) {
	if proxy, err := http.ProxyFromEnvironment(req); proxy != nil || err != nil {
		return proxy, err
	}
	return systemProxyForRequest(req)
}

func proxyURLFromProxyServer(proxyServer, requestScheme string) (*url.URL, error) {
	proxyServer = strings.TrimSpace(proxyServer)
	if proxyServer == "" {
		return nil, nil
	}

	if !strings.Contains(proxyServer, "=") {
		return normalizeProxyURL(proxyServer, "http")
	}

	entries := strings.Split(proxyServer, ";")
	values := map[string]string{}
	first := ""
	for _, entry := range entries {
		key, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if first == "" {
			first = value
		}
		values[key] = value
	}

	if value := values[strings.ToLower(requestScheme)]; value != "" {
		return normalizeProxyURL(value, strings.ToLower(requestScheme))
	}
	if value := values["http"]; value != "" {
		return normalizeProxyURL(value, "http")
	}
	if value := values["https"]; value != "" {
		return normalizeProxyURL(value, "http")
	}
	if value := values["socks"]; value != "" {
		return normalizeProxyURL(value, "socks")
	}
	return normalizeProxyURL(first, "http")
}

func normalizeProxyURL(raw, proxyKind string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !strings.Contains(raw, "://") {
		scheme := "http"
		if strings.EqualFold(proxyKind, "socks") {
			scheme = "socks5"
		}
		raw = scheme + "://" + raw
	}
	return url.Parse(raw)
}
