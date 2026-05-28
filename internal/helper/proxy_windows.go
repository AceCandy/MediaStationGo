//go:build windows

package helper

import (
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const windowsInternetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

func systemProxyForRequest(req *http.Request) (*url.URL, error) {
	if req == nil || req.URL == nil {
		return nil, nil
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, nil
	}
	proxyServer, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(proxyServer) == "" {
		return nil, nil
	}

	if proxyOverride, _, err := key.GetStringValue("ProxyOverride"); err == nil {
		if windowsProxyBypass(req.URL.Hostname(), proxyOverride) {
			return nil, nil
		}
	}
	return proxyURLFromProxyServer(proxyServer, req.URL.Scheme)
}

func windowsProxyBypass(host, override string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if parsed := net.ParseIP(host); parsed != nil && parsed.IsLoopback() {
		return true
	}

	for _, rule := range strings.Split(override, ";") {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}
		if rule == "<local>" && !strings.Contains(host, ".") {
			return true
		}
		if ok, _ := filepath.Match(rule, host); ok {
			return true
		}
		if strings.HasPrefix(rule, "*.") && strings.HasSuffix(host, strings.TrimPrefix(rule, "*")) {
			return true
		}
		if host == rule {
			return true
		}
	}
	return false
}
