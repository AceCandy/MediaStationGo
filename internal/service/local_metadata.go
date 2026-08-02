// Package service local metadata helpers read Kodi/Jellyfin NFO sidecars and
// nearby artwork, then normalize them into LocalMetadata for scanner/scraper
// ingestion.
package service

import (
	"path/filepath"
	"strings"
)

func nfoPath(media string) string {
	dir := filepath.Dir(media)
	base := strings.TrimSuffix(filepath.Base(media), filepath.Ext(media))
	return filepath.Join(dir, base+".nfo")
}

func splitNFOList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
