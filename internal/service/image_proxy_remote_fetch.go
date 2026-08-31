package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

type remoteImageFetchClient struct {
	name   string
	client *http.Client
}

type remoteImageHTTPStatusError struct {
	StatusCode int
}

func (e *remoteImageHTTPStatusError) Error() string {
	return "upstream image returned status " + strconv.Itoa(e.StatusCode)
}

func isRemoteImageHTTPStatus(err error, statusCode int) bool {
	var statusErr *remoteImageHTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == statusCode
}

func (p *ImageProxy) remoteImageFetchClients(directOnly bool) []remoteImageFetchClient {
	client := p.client
	if client == nil {
		client = NewExternalHTTPClient(30 * time.Second)
	}
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	direct := remoteImageFetchClient{
		name:   "direct",
		client: &http.Client{Timeout: timeout, Transport: NewInternalTransport()},
	}
	if directOnly {
		return []remoteImageFetchClient{direct}
	}
	clients := []remoteImageFetchClient{{name: "default", client: client}}
	if _, ok := client.Transport.(*http.Transport); ok {
		clients = append(clients, direct)
	}
	return clients
}

func (p *ImageProxy) canUseExternalImageFallback(directOnly bool, host string) bool {
	if p == nil || p.client == nil {
		return false
	}
	_, ok := p.client.Transport.(*http.Transport)
	return ok && (directOnly || isDoubanImageHost(host))
}

func (p *ImageProxy) fetchRemoteImageOnce(ctx context.Context, raw, host string, candidate remoteImageFetchClient) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		p.log.Warn("imageproxy: build request failed", zap.String("url", raw), zap.Error(err))
		return nil, "", "", errImageProxyRequestSetup
	}
	applyRemoteImageHeaders(req, host)

	resp, err := candidate.client.Do(req)
	if err != nil {
		p.log.Warn("imageproxy: upstream fetch failed", zap.String("host", host), zap.String("client", candidate.name), zap.Error(err))
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		p.log.Warn("imageproxy: upstream returned non-OK", zap.String("host", host), zap.String("client", candidate.name), zap.String("status", resp.Status))
		return nil, "", "", &remoteImageHTTPStatusError{StatusCode: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil || len(data) == 0 {
		p.log.Warn("imageproxy: read upstream body failed", zap.String("host", host), zap.String("client", candidate.name), zap.Error(err))
		if err == nil {
			err = errors.New("upstream image body is empty")
		}
		return nil, "", "", err
	}
	ctype, ok := validImageContentType(data)
	if !ok {
		p.log.Warn("imageproxy: upstream returned non-image content", zap.String("host", host), zap.String("client", candidate.name), zap.String("content_type", resp.Header.Get("Content-Type")))
		return nil, "", "", errImageProxyNonImageContent
	}
	return data, ctype, resp.Header.Get("Content-Length"), nil
}

func applyRemoteImageHeaders(req *http.Request, host string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	switch {
	case isDoubanImageHost(host):
		req.Header.Set("Referer", "https://movie.douban.com/")
	case strings.Contains(host, "bgm.tv"):
		req.Header.Set("Referer", "https://bgm.tv/")
	}
}

func isDoubanImageHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return host == "doubanio.com" || strings.HasSuffix(host, ".doubanio.com")
}

// useDoubanImageDirect 只把豆瓣官方源或当前配置的图片源切到直连模式。
func (p *ImageProxy) useDoubanImageDirect(ctx context.Context, host string) bool {
	if p == nil || p.apiConfig == nil {
		return false
	}
	resolved, err := p.apiConfig.Resolve(ctx, "douban")
	if err != nil || !resolved.ImageDirect {
		return false
	}
	if isDoubanImageHost(host) {
		return true
	}
	origin, err := url.Parse(resolved.BaseURL)
	return err == nil && origin.Host != "" && strings.EqualFold(origin.Host, host)
}

var fetchRemoteImageWithCurl = fetchRemoteImageWithCurlCommand

func fetchRemoteImageWithCurlCommand(ctx context.Context, raw, host string) ([]byte, string, string, error) {
	bin, err := exec.LookPath("curl")
	if err != nil {
		return nil, "", "", err
	}
	curlCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	args := []string{
		"--fail",
		"--location",
		"--silent",
		"--show-error",
		"--http1.1",
		"--max-time", "20",
		"--proto", "=http,https",
		"--proto-redir", "=http,https",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36",
		"--header", "Accept: image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
		"--header", "Accept-Language: zh-CN,zh;q=0.9,en;q=0.8",
		"--header", "Cache-Control: no-cache",
		"--header", "Pragma: no-cache",
	}
	if isDoubanImageHost(host) {
		args = append(args, "--referer", "https://movie.douban.com/")
	}
	args = append(args, "--", raw)

	cmd := exec.CommandContext(curlCtx, bin, args...) // #nosec G204 -- bin is resolved by LookPath and args are not shell-expanded.
	stderr := bytes.Buffer{}
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, 32<<20))
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, "", "", readErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, "", "", errors.New(message)
		}
		return nil, "", "", waitErr
	}
	if len(data) == 0 {
		return nil, "", "", errors.New("curl image body is empty")
	}
	ctype := detectContentType(data)
	if !isImageContentType(ctype) || isTransparentPlaceholderData(data) {
		return nil, "", "", errors.New("curl returned non-image content")
	}
	return data, ctype, "", nil
}
