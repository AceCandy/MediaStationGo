package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

var errProxyPoolUnavailable = errors.New("proxy pool configuration is unavailable")

// ProxyPoolItem is the credential-free administrator projection of a proxy.
type ProxyPoolItem struct {
	ID         string `json:"id"`
	DisplayURL string `json:"display_url"`
	HasAuth    bool   `json:"has_auth"`
}

// ProxyPoolInput replaces, retains, or creates one ordered proxy entry.
type ProxyPoolInput struct {
	ID  string  `json:"id,omitempty"`
	URL *string `json:"url,omitempty"`
}

type proxyPoolSnapshot struct {
	generation uint64
	clients    []*http.Client
}

// ProxyPoolService owns encrypted proxy configuration and reusable transports.
type ProxyPoolService struct {
	repo   *repository.Container
	crypto *CryptoService

	mu         sync.Mutex
	loaded     bool
	generation uint64
	clients    []*http.Client
}

func NewProxyPoolService(repo *repository.Container, crypto *CryptoService) *ProxyPoolService {
	return &ProxyPoolService{repo: repo, crypto: crypto}
}

// List returns the ordered proxy list without authentication information.
func (s *ProxyPoolService) List(ctx context.Context) ([]ProxyPoolItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.loadRows(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ProxyPoolItem, 0, len(rows))
	for i := range rows {
		u, err := s.decryptURL(rows[i].URL)
		if err != nil {
			return nil, err
		}
		items = append(items, publicProxyPoolItem(rows[i].ID, u))
	}
	return items, nil
}

// Replace atomically saves the complete ordered proxy list.
func (s *ProxyPoolService) Replace(ctx context.Context, input []ProxyPoolInput) ([]ProxyPoolItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existingRows, err := s.loadRows(ctx)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]model.ProxyPoolEntry, len(existingRows))
	for _, row := range existingRows {
		existing[row.ID] = row
	}

	rows := make([]model.ProxyPoolEntry, 0, len(input))
	parsedURLs := make([]*url.URL, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for i, item := range input {
		id := strings.TrimSpace(item.ID)
		row, found := existing[id]
		if id != "" {
			if !found {
				return nil, proxyPoolInputError(i, "unknown id")
			}
			if _, duplicate := seen[id]; duplicate {
				return nil, proxyPoolInputError(i, "duplicate id")
			}
			seen[id] = struct{}{}
		} else {
			row = model.ProxyPoolEntry{}
		}

		var parsed *url.URL
		if item.URL == nil {
			if id == "" {
				return nil, proxyPoolInputError(i, "url is required")
			}
			parsed, err = s.decryptURL(row.URL)
		} else {
			parsed, err = normalizeProxyPoolURL(*item.URL)
			if err == nil {
				row.URL, err = s.encryptURL(parsed.String())
			}
		}
		if err != nil {
			return nil, proxyPoolInputError(i, err.Error())
		}
		row.Position = i
		rows = append(rows, row)
		parsedURLs = append(parsedURLs, parsed)
	}

	clients := make([]*http.Client, 0, len(parsedURLs))
	for _, parsed := range parsedURLs {
		transport := NewExternalTransport()
		transport.Proxy = http.ProxyURL(parsed)
		clients = append(clients, &http.Client{Transport: transport})
	}

	if err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&model.ProxyPoolEntry{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(&rows, 500).Error
	}); err != nil {
		closeProxyPoolClients(clients)
		return nil, err
	}

	oldClients := s.clients
	s.generation++
	s.clients = clients
	s.loaded = true
	closeProxyPoolClients(oldClients)

	items := make([]ProxyPoolItem, 0, len(rows))
	for i := range rows {
		items = append(items, publicProxyPoolItem(rows[i].ID, parsedURLs[i]))
	}
	return items, nil
}

func (s *ProxyPoolService) snapshot(ctx context.Context) (proxyPoolSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		rows, err := s.loadRows(ctx)
		if err != nil {
			return proxyPoolSnapshot{}, err
		}
		clients := make([]*http.Client, 0, len(rows))
		for i := range rows {
			parsed, err := s.decryptURL(rows[i].URL)
			if err != nil {
				closeProxyPoolClients(clients)
				return proxyPoolSnapshot{}, err
			}
			transport := NewExternalTransport()
			transport.Proxy = http.ProxyURL(parsed)
			clients = append(clients, &http.Client{Transport: transport})
		}
		s.generation++
		s.clients = clients
		s.loaded = true
	}
	return proxyPoolSnapshot{
		generation: s.generation,
		clients:    append([]*http.Client(nil), s.clients...),
	}, nil
}

func (s *ProxyPoolService) loadRows(ctx context.Context) ([]model.ProxyPoolEntry, error) {
	if s == nil || s.repo == nil || s.repo.DB == nil || s.crypto == nil {
		return nil, errProxyPoolUnavailable
	}
	var rows []model.ProxyPoolEntry
	if err := s.repo.DB.WithContext(ctx).Order("position asc, created_at asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ProxyPoolService) decryptURL(ciphertext string) (*url.URL, error) {
	if s.crypto == nil || !s.crypto.IsEncrypted(ciphertext) {
		return nil, errProxyPoolUnavailable
	}
	plain := s.crypto.Decrypt(ciphertext)
	if plain == ciphertext {
		return nil, errProxyPoolUnavailable
	}
	return normalizeProxyPoolURL(plain)
}

func (s *ProxyPoolService) encryptURL(plain string) (string, error) {
	if s.crypto == nil {
		return "", errProxyPoolUnavailable
	}
	ciphertext := s.crypto.Encrypt(plain)
	if !s.crypto.IsEncrypted(ciphertext) {
		return "", errProxyPoolUnavailable
	}
	return ciphertext, nil
}

func normalizeProxyPoolURL(raw string) (*url.URL, error) {
	u, err := normalizeProxyURL(raw, "http")
	if err != nil || u == nil || u.Hostname() == "" {
		return nil, errors.New("invalid proxy url")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("unsupported proxy protocol")
	}
	if u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("proxy url must contain only scheme, credentials, host, and port")
	}
	u.Path = ""
	u.RawPath = ""
	return u, nil
}

func publicProxyPoolItem(id string, u *url.URL) ProxyPoolItem {
	publicURL := *u
	hasAuth := publicURL.User != nil
	publicURL.User = nil
	return ProxyPoolItem{ID: id, DisplayURL: publicURL.String(), HasAuth: hasAuth}
}

func proxyPoolInputError(index int, reason string) error {
	return fmt.Errorf("proxy %d: %s", index+1, reason)
}

func closeProxyPoolClients(clients []*http.Client) {
	for _, client := range clients {
		client.CloseIdleConnections()
	}
}
