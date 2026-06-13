package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// quarkProvider implements the 夸克网盘 cloud disk using cookie auth.
//
// Quark's web API is plain JSON over HTTPS keyed by a session cookie; no
// request-body encryption is required (unlike 115). The resolved download_url
// is tied to the session, so playback runs in proxy mode by default.
type quarkProvider struct {
	cookie string
	ua     string
	base   string // override for tests; defaults to quarkBase
	client *http.Client
	proxy  bool
}

const quarkBase = "https://drive-pc.quark.cn/1/clouddrive"

func newQuark(cfg map[string]any, client *http.Client) *quarkProvider {
	base := str(cfg["base"])
	if base == "" {
		base = quarkBase
	}
	ua := str(cfg["ua"])
	if ua == "" {
		ua = defaultUA
	}
	// Quark download links require the session cookie + UA, so the host must
	// reverse-proxy. The global cloud playback setting decides whether clients
	// receive a STRMURL entry or a /Videos stream entry; this provider only
	// reports whether the resolved upstream URL itself is safe for raw 302.
	proxy := true
	return &quarkProvider{
		cookie: str(cfg["cookie"]),
		ua:     ua,
		base:   strings.TrimRight(base, "/"),
		client: client,
		proxy:  proxy,
	}
}

func (q *quarkProvider) Type() string { return TypeQuark }

func (q *quarkProvider) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, q.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", q.cookie)
	req.Header.Set("User-Agent", q.ua)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://pan.quark.cn/")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return q.client.Do(req)
}

type quarkResp struct {
	Status  int             `json:"status"`
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (q *quarkProvider) Ping(ctx context.Context) error {
	if q.cookie == "" {
		return fmt.Errorf("quark: missing cookie")
	}
	_, err := q.List(ctx, "0")
	return err
}

func (q *quarkProvider) List(ctx context.Context, dirID string) ([]FileEntry, error) {
	if dirID == "" {
		dirID = "0"
	}
	const pageSize = 100
	out := make([]FileEntry, 0, pageSize)
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("pr", "ucpro")
		query.Set("fr", "pc")
		query.Set("uc_param_str", "")
		query.Set("pdir_fid", dirID)
		query.Set("_page", fmt.Sprint(page))
		query.Set("_size", fmt.Sprint(pageSize))
		query.Set("_fetch_total", "1")
		query.Set("_sort", "file_type:asc,updated_at:desc")
		path := "/file/sort?" + query.Encode()
		resp, err := q.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var r quarkResp
		err = json.NewDecoder(resp.Body).Decode(&r)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("quark: decode list: %w", err)
		}
		if r.Code != 0 && r.Status != 200 {
			return nil, fmt.Errorf("quark: list failed: %s", r.Message)
		}
		var data struct {
			List []struct {
				Fid      string `json:"fid"`
				FileName string `json:"file_name"`
				Dir      bool   `json:"dir"`
				Size     int64  `json:"size"`
			} `json:"list"`
		}
		if err := json.Unmarshal(r.Data, &data); err != nil {
			return nil, fmt.Errorf("quark: decode list data: %w", err)
		}
		for _, it := range data.List {
			out = append(out, FileEntry{
				ID:    it.Fid,
				Name:  it.FileName,
				IsDir: it.Dir,
				Size:  it.Size,
			})
		}
		if len(data.List) < pageSize {
			break
		}
	}
	return out, nil
}

func (q *quarkProvider) Resolve(ctx context.Context, fileRef string) (*DirectLink, error) {
	if fileRef == "" {
		return nil, fmt.Errorf("quark: empty file id")
	}
	payload, _ := json.Marshal(map[string]any{"fids": []string{fileRef}})
	resp, err := q.do(ctx, http.MethodPost, "/file/download?pr=ucpro&fr=pc&uc_param_str=", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r quarkResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("quark: decode download: %w", err)
	}
	if r.Code != 0 && r.Status != 200 {
		return nil, fmt.Errorf("quark: download failed: %s", r.Message)
	}
	var data []struct {
		DownloadURL string `json:"download_url"`
		Fid         string `json:"fid"`
	}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return nil, fmt.Errorf("quark: decode download data: %w", err)
	}
	if len(data) == 0 || data[0].DownloadURL == "" {
		return nil, fmt.Errorf("quark: no download url returned")
	}
	return &DirectLink{
		URL: data[0].DownloadURL,
		Headers: map[string]string{
			"Cookie":     q.cookie,
			"User-Agent": q.ua,
			"Referer":    "https://pan.quark.cn/",
		},
		Proxy: q.proxy,
	}, nil
}
