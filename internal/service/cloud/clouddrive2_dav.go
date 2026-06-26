package cloud

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

func (p *cloudDrive2Provider) List(ctx context.Context, dir string) ([]FileEntry, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	if p.typ == TypeOpenList && p.apiBase != nil && p.hasOpenListAPICredentials() {
		return p.listOpenListAPI(ctx, dir)
	}
	target := normalizeCloudDAVPath(dir)
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", p.urlFor(target), strings.NewReader(cloudDAVPropfindBody))
	if err != nil {
		return nil, err
	}
	p.auth(req)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Accept", "application/xml,text/xml,*/*")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, decorateDAVTransportError(p.name, p.urlFor(target), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, p.decorateDAVStatusError(resp, target)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var multi cloudDAVMultiStatus
	if err := xml.Unmarshal(body, &multi); err != nil {
		return nil, fmt.Errorf("%s: decode webdav: %w", p.name, err)
	}
	basePath := strings.TrimRight(p.base.EscapedPath(), "/")
	currentID := normalizeCloudDAVPath(target)
	out := make([]FileEntry, 0, len(multi.Responses))
	for _, item := range multi.Responses {
		entryPath, err := p.entryIDFromHref(item.Href, basePath)
		if err != nil || entryPath == "" || sameCloudDAVPath(entryPath, currentID) {
			continue
		}
		name := firstNonEmpty(item.PropStat.Prop.DisplayName, path.Base(strings.TrimRight(entryPath, "/")))
		if decoded, err := url.PathUnescape(name); err == nil {
			name = decoded
		}
		if name == "" || name == "." || name == "/" {
			continue
		}
		out = append(out, FileEntry{
			ID:    entryPath,
			Name:  name,
			IsDir: item.PropStat.Prop.ResourceType.Collection != nil || strings.HasSuffix(item.Href, "/"),
			Size:  parseDAVSize(item.PropStat.Prop.ContentLength),
		})
	}
	return out, nil
}

func (p *cloudDrive2Provider) resolveCloudDAVRedirectDirect(ctx context.Context, fileRef string) (*DirectLink, error) {
	target := p.urlFor(fileRef)
	headers := map[string]string{
		"User-Agent": p.ua,
	}
	if p.token != "" {
		headers["Authorization"] = p.token
	} else if p.username != "" {
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(p.username+":"+p.password))
	}
	location, status, err := p.firstHTTPRedirectLocation(ctx, target, headers)
	if err != nil {
		return nil, decorateDAVTransportError(p.name, target, err)
	}
	if location == "" {
		return nil, fmt.Errorf("%s: WebDAV %s returned http %d without CDN Location; refusing WebDAV/proxy fallback for pure 302 playback", p.name, fileRef, status)
	}
	return &DirectLink{URL: location, Headers: nil, Proxy: false}, nil
}

func (p *cloudDrive2Provider) firstHTTPRedirectLocation(ctx context.Context, target string, headers map[string]string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", "bytes=0-0")
	if strings.TrimSpace(p.ua) != "" {
		req.Header.Set("User-Agent", p.ua)
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	if status >= 300 && status < 400 {
		rawLocation := strings.TrimSpace(resp.Header.Get("Location"))
		if rawLocation == "" {
			return "", status, fmt.Errorf("%s: upstream returned redirect http %d without Location", p.name, status)
		}
		location, err := resolveHTTPRedirectLocation(target, rawLocation)
		if err != nil {
			return "", status, err
		}
		return location, status, nil
	}
	return "", status, nil
}

func resolveHTTPRedirectLocation(baseURL, rawLocation string) (string, error) {
	rawLocation = strings.TrimSpace(rawLocation)
	if rawLocation == "" {
		return "", fmt.Errorf("empty redirect Location")
	}
	if strings.HasPrefix(rawLocation, "//") {
		base, err := url.Parse(baseURL)
		if err != nil || base.Scheme == "" {
			return "", fmt.Errorf("protocol-relative redirect Location without base scheme")
		}
		rawLocation = base.Scheme + ":" + rawLocation
	}
	location, err := url.Parse(rawLocation)
	if err != nil {
		return "", fmt.Errorf("invalid redirect Location: %w", err)
	}
	if location.IsAbs() {
		if location.Scheme != "http" && location.Scheme != "https" {
			return "", fmt.Errorf("unsupported redirect Location scheme %q", location.Scheme)
		}
		return location.String(), nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid redirect base URL: %w", err)
	}
	return base.ResolveReference(location).String(), nil
}

func (p *cloudDrive2Provider) auth(req *http.Request) {
	req.Header.Set("User-Agent", p.ua)
	if p.token != "" {
		req.Header.Set("Authorization", p.token)
		return
	}
	if p.username != "" {
		req.SetBasicAuth(p.username, p.password)
	}
}

func (p *cloudDrive2Provider) decorateDAVStatusError(resp *http.Response, target string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := compactDAVErrorBody(string(body))
	if detail == "" {
		if resp.StatusCode == http.StatusMethodNotAllowed {
			return fmt.Errorf("%s: list %s returned http %d；请确认填写的是 WebDAV 地址（通常以 /dav 结尾），并且桥接网盘已在 OpenList/CloudDrive2 内完成登录或 Cookie 保存", p.name, target, resp.StatusCode)
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%s: list %s returned http %d；请填写 OpenList/CloudDrive2 的 Token 或用户名密码，或确认 WebDAV 凭据可用", p.name, target, resp.StatusCode)
		}
		return fmt.Errorf("%s: list %s returned http %d", p.name, target, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusMethodNotAllowed {
		return fmt.Errorf("%s: list %s returned http %d：%s；请确认填写的是 WebDAV 地址（通常以 /dav 结尾），并且桥接网盘已在 OpenList/CloudDrive2 内完成登录或 Cookie 保存", p.name, target, resp.StatusCode, detail)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s: list %s returned http %d：%s；请检查 WebDAV 用户名/密码、Authorization Token，或先在 OpenList/CloudDrive2 中保存对应网盘 Cookie", p.name, target, resp.StatusCode, detail)
	}
	return fmt.Errorf("%s: list %s returned http %d：%s", p.name, target, resp.StatusCode, detail)
}

func compactDAVErrorBody(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\x00", ""))
	if raw == "" {
		return ""
	}
	raw = strings.Join(strings.Fields(raw), " ")
	if len([]rune(raw)) > 180 {
		return string([]rune(raw)[:180]) + "…"
	}
	return raw
}

func decorateDAVTransportError(name, target string, err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "server gave HTTP response to HTTPS client") {
		return fmt.Errorf("%s: %w；当前地址使用 https://，但服务端返回 HTTP。请改用 http:// 地址，例如 OpenList 默认 WebDAV 通常是 http://host:5244/dav/；如果必须使用 https，请在 OpenList 前配置反向代理和证书", name, err)
	}
	if strings.Contains(message, "first record does not look like a TLS handshake") {
		return fmt.Errorf("%s: %w；疑似把 HTTP 服务配置成了 https://，请检查 %s 的协议头", name, err, target)
	}
	return err
}

func (p *cloudDrive2Provider) urlFor(remotePath string) string {
	u := *p.base
	u.RawPath = ""
	basePath := strings.TrimRight(u.Path, "/")
	remote := strings.Trim(normalizeCloudDAVPath(remotePath), "/")
	switch {
	case basePath == "" || basePath == "/":
		if remote == "" {
			u.Path = "/"
		} else {
			u.Path = "/" + remote
		}
	case remote == "":
		u.Path = basePath
	default:
		u.Path = basePath + "/" + remote
	}
	return u.String()
}

func (p *cloudDrive2Provider) entryIDFromHref(href, basePath string) (string, error) {
	if href == "" {
		return "", nil
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	hrefPath := parsed.EscapedPath()
	if hrefPath == "" {
		hrefPath = href
	}
	if basePath != "" && basePath != "/" {
		hrefPath = strings.TrimPrefix(hrefPath, basePath)
	}
	if decoded, err := url.PathUnescape(hrefPath); err == nil {
		hrefPath = decoded
	}
	return normalizeCloudDAVPath(hrefPath), nil
}

const cloudDAVPropfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:displayname/>
    <d:getcontentlength/>
    <d:resourcetype/>
  </d:prop>
</d:propfind>`

type cloudDAVMultiStatus struct {
	Responses []cloudDAVResponse `xml:"response"`
}

type cloudDAVResponse struct {
	Href     string           `xml:"href"`
	PropStat cloudDAVPropStat `xml:"propstat"`
}

type cloudDAVPropStat struct {
	Prop cloudDAVProp `xml:"prop"`
}

type cloudDAVProp struct {
	DisplayName   string               `xml:"displayname"`
	ContentLength string               `xml:"getcontentlength"`
	ResourceType  cloudDAVResourceType `xml:"resourcetype"`
}

type cloudDAVResourceType struct {
	Collection *struct{} `xml:"collection"`
}

func parseDAVSize(raw string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return n
}
