package hongguo

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Fake-IP 只触发重新解析，绝不作为可连接地址；IP 字面量不能借此绕过检查。
func downloadLookupIP(ctx context.Context, host string, system, public func(context.Context, string) ([]net.IPAddr, error)) ([]net.IPAddr, error) {
	ips, err := system(ctx, host)
	if err != nil {
		return nil, errDownloadDNS
	}
	if net.ParseIP(host) == nil {
		_, fake, _ := net.ParseCIDR("198.18.0.0/15")
		for _, ip := range ips {
			if fake.Contains(ip.IP) {
				ips, err = public(ctx, host)
				if err != nil {
					return nil, errDownloadPublicDNS
				}
				break
			}
		}
	}
	if len(ips) == 0 {
		return nil, errDownloadDNS
	}
	return ips, nil
}

// 固定 DoH 入口绕过本机 Fake-IP DNS；TLS 仍验证服务域名，禁止代理和重定向。
func lookupDownloadPublicIP(ctx context.Context, host string) ([]net.IPAddr, error) {
	transport := &http.Transport{
		TLSHandshakeTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, "223.5.5.5:443")
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://dns.alidns.com/resolve?type=A&name="+url.QueryEscape(host), nil)
	if err != nil {
		return nil, errDownloadPublicDNS
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errDownloadPublicDNS
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errDownloadPublicDNS
	}
	return parseDownloadDNS(resp.Body)
}

func parseDownloadDNS(reader io.Reader) ([]net.IPAddr, error) {
	body, err := io.ReadAll(io.LimitReader(reader, 65537))
	if err != nil || len(body) > 65536 {
		return nil, errDownloadPublicDNS
	}
	var response struct {
		Status int
		Answer []struct {
			Type int
			Data string
		}
	}
	if json.Unmarshal(body, &response) != nil || response.Status != 0 {
		return nil, errDownloadPublicDNS
	}
	var ips []net.IPAddr
	for _, answer := range response.Answer {
		if answer.Type != 1 {
			continue
		}
		ip := net.ParseIP(answer.Data)
		if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || reservedDownloadIP(ip) {
			return nil, errDownloadPublicDNS
		}
		ips = append(ips, net.IPAddr{IP: ip})
	}
	if len(ips) == 0 {
		return nil, errDownloadPublicDNS
	}
	return ips, nil
}
