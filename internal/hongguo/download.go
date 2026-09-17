package hongguo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DownloadMedia 仅供后台下载使用，不返回给浏览器或持久化。
type DownloadMedia struct {
	URL      string
	Referer  string
	Key      []byte
	Duration float64
	Quality  int
	Width    int
	Height   int
	Codec    string
}

const DownloadApp = "app"
const DownloadOfficial = "official"
const DownloadFallback = "fallback"

// DownloadSources 保留已配置的首选来源，其余按 App、备用、网页顺序兜底。
func DownloadSources(priority string) []string {
	sources := []string{priority}
	for _, source := range []string{DownloadApp, DownloadFallback, DownloadOfficial} {
		if source != priority {
			sources = append(sources, source)
		}
	}
	return sources
}

func DownloadSourceName(source string) string {
	return map[string]string{DownloadApp: "App 接口", DownloadFallback: "备用接口", DownloadOfficial: "官方网页"}[source]
}

// DownloadHTTPClient 在实际连接时校验每个 IP，避免媒体 URL 和重定向访问本机或内网。
func DownloadHTTPClient() *http.Client {
	transport := &http.Transport{TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("媒体地址无效")
		}
		ips, err := downloadLookupIP(ctx, host, net.DefaultResolver.LookupIPAddr, lookupDownloadPublicIP)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || reservedDownloadIP(ip.IP) {
				return nil, errDownloadPrivateIP
			}
		}
		for _, ip := range ips {
			conn, dialErr := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
		}
		return nil, errDownloadConnect
	}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !ValidDownloadURL(req.URL.String()) {
			return errDownloadRedirect
		}
		return nil
	}}
}

func reservedDownloadIP(ip net.IP) bool {
	for _, block := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "2001:db8::/32"} {
		_, subnet, _ := net.ParseCIDR(block)
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

func ValidDownloadURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 8192 && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "80" || u.Port() == "443")
}

// ResolveDownload 核对源身份后返回媒体；不能把试看集作为请求分集。
func (c *Client) ResolveDownload(ctx context.Context, sourceID, videoID string) (DownloadMedia, error) {
	media, err := c.ResolveDownloadSource(ctx, sourceID, videoID, DownloadOfficial)
	if err == nil || ctx.Err() != nil {
		return media, err
	}
	return c.ResolveDownloadSource(ctx, sourceID, videoID, DownloadFallback)
}

// ResolveDownloadSource 只解析指定来源，由下载执行器在传输或校验失败时切换来源。
func (c *Client) ResolveDownloadSource(ctx context.Context, sourceID, videoID, source string) (DownloadMedia, error) {
	if !ValidID(sourceID) || !ValidID(videoID) {
		return DownloadMedia{}, errors.New("红果作品或分集 ID 无效")
	}
	if err := ctx.Err(); err != nil {
		return DownloadMedia{}, err
	}
	if source == DownloadFallback {
		return c.resolveDownloadFallback(ctx, sourceID, videoID)
	}
	if source == DownloadApp {
		return c.resolveDownloadApp(ctx, videoID)
	}
	if source != DownloadOfficial {
		return DownloadMedia{}, errors.New("下载来源无效")
	}
	body, err := c.page(ctx, "/player/"+sourceID+"/"+videoID)
	if err != nil {
		return DownloadMedia{}, err
	}
	return parseDownloadPage(body, sourceID, videoID)
}

func parseDownloadPage(body []byte, sourceID, videoID string) (DownloadMedia, error) {
	page, err := loader(body, "player")
	if err != nil {
		return DownloadMedia{}, err
	}
	if scalar(page["series_id"]) != sourceID || scalar(page["vid"]) != videoID {
		return DownloadMedia{}, errors.New("来源返回了其他分集")
	}
	info := object(page["video_player_info"])
	raw := scalar(info["main_url"])
	if !ValidDownloadURL(raw) {
		return DownloadMedia{}, errors.New("该集未提供可下载媒体")
	}
	duration, _ := strconv.ParseFloat(scalar(info["duration"]), 64)
	return DownloadMedia{URL: raw, Referer: BaseURL + "/", Duration: duration}, nil
}

// DownloadRequest 不携带业务账户凭据；外部错误文本不包含签名地址。
func DownloadRequest(ctx context.Context, client *http.Client, media DownloadMedia) (*http.Response, error) {
	if !ValidDownloadURL(media.URL) || strings.ContainsAny(media.Referer, "\r\n") {
		return nil, errors.New("媒体地址无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
	if err != nil {
		return nil, errors.New("媒体请求无效")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", media.Referer)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, publicDownloadError(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("媒体请求 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

var (
	errDownloadPrivateIP = errors.New("媒体主机必须使用公网地址")
	errDownloadDNS       = errors.New("媒体主机 DNS 解析失败")
	errDownloadPublicDNS = errors.New("Fake-IP 网络的公网 DNS 解析失败，请检查网络")
	errDownloadConnect   = errors.New("媒体连接失败，请检查网络")
	errDownloadRedirect  = errors.New("媒体重定向无效")
)

// 仅公开本地定义的错误类别，不暴露 url.Error 中的签名地址。
func publicDownloadError(err error) error {
	for _, known := range []error{errDownloadPrivateIP, errDownloadDNS, errDownloadPublicDNS, errDownloadConnect, errDownloadRedirect} {
		if errors.Is(err, known) {
			return known
		}
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return errors.New("媒体请求超时")
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) {
		return errors.New("媒体 TLS 证书验证失败")
	}
	return errors.New("媒体网络请求失败")
}
