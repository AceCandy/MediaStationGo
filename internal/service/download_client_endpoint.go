package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var downloadClientEndpointPattern = regexp.MustCompile(`^https?://(?:[A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(?::[0-9]{1,5})?(?:/[A-Za-z0-9._~%!$&'()*+,;=:@/-]*)?$`)

func NormalizeDownloadClientHost(clientType, raw string) (string, error) {
	return normalizeDownloadClientEndpoint(clientType, raw)
}

func normalizeDownloadClientEndpoint(clientType, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("host required")
	}
	if strings.ContainsAny(raw, "\r\n\t") {
		return "", errors.New("host contains invalid control characters")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	if !downloadClientEndpointPattern.MatchString(raw) {
		return "", errors.New("host must be a valid http(s) URL without username, query, or fragment")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("host must be a valid http(s) URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("host only supports http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("host must not include username, query, or fragment")
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return "", errors.New("host must include a hostname")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("host port must be between 1 and 65535")
		}
	}
	if err := validateDownloadClientPath(clientType, parsed.Path); err != nil {
		return "", err
	}
	parsed.Scheme = scheme
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func validateDownloadClientPath(clientType, rawPath string) error {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" || rawPath == "/" {
		return nil
	}
	for _, segment := range strings.Split(rawPath, "/") {
		if segment == "." || segment == ".." {
			return errors.New("host path must not contain traversal segments")
		}
	}
	switch clientType {
	case "qbittorrent", "aria2", "transmission":
		return nil
	default:
		return fmt.Errorf("unsupported client type %q", clientType)
	}
}

func downloadClientRPCURL(clientType, host string) (string, error) {
	base, err := normalizeDownloadClientEndpoint(clientType, host)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch clientType {
	case "aria2":
		if !strings.HasSuffix(strings.ToLower(u.Path), "/jsonrpc") {
			u.Path = strings.TrimRight(u.Path, "/") + "/jsonrpc"
		}
	case "transmission":
		if !strings.Contains(strings.ToLower(u.Path), "/rpc") {
			u.Path = strings.TrimRight(u.Path, "/") + "/transmission/rpc"
		}
	case "qbittorrent":
	default:
		return "", fmt.Errorf("unsupported client type %q", clientType)
	}
	return u.String(), nil
}

func newDownloadClientHTTPRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	endpoint = strings.TrimSpace(endpoint)
	if !downloadClientEndpointPattern.MatchString(endpoint) {
		return nil, errors.New("download client endpoint failed safety validation")
	}
	return http.NewRequestWithContext(ctx, method, endpoint, body)
}
