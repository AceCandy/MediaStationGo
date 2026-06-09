package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// OrganizeScanSummary reports a library scan triggered after directory
// organize. It is intentionally compact for the tools page toast.
type OrganizeScanSummary struct {
	LibraryID string `json:"library_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Visited   int    `json:"visited"`
	Added     int    `json:"added"`
	Updated   int    `json:"updated"`
	Removed   int64  `json:"removed"`
	Error     string `json:"error,omitempty"`
}

// ScanLibrariesForPath recursively scans libraries affected by an organize
// destination. If preferredLibraryID is set, only that library is scanned.
// Otherwise every enabled library whose path intersects destRoot is scanned;
// if no path can be matched, we fall back to all enabled libraries to preserve
// the old "scan all after ingest" UI behavior.
func (s *ScannerService) ScanLibrariesForPath(ctx context.Context, destRoot, preferredLibraryID string) []OrganizeScanSummary {
	if s == nil || s.repo == nil || s.repo.Library == nil {
		return nil
	}
	libraries, err := s.repo.Library.List(ctx)
	if err != nil {
		return []OrganizeScanSummary{{Error: err.Error()}}
	}
	targets := selectOrganizeScanTargets(libraries, destRoot, preferredLibraryID)
	out := make([]OrganizeScanSummary, 0, len(targets))
	for _, lib := range targets {
		summary := OrganizeScanSummary{
			LibraryID: lib.ID,
			Name:      lib.Name,
			Path:      lib.Path,
		}
		res, err := s.ScanLibrary(ctx, lib.ID)
		if err != nil {
			summary.Error = err.Error()
			out = append(out, summary)
			continue
		}
		summary.Visited = res.Visited
		summary.Added = res.Added
		summary.Updated = res.Updated
		summary.Removed = res.Removed
		out = append(out, summary)
	}
	return out
}

func selectOrganizeScanTargets(libraries []model.Library, destRoot, preferredLibraryID string) []model.Library {
	preferredLibraryID = strings.TrimSpace(preferredLibraryID)
	enabled := make([]model.Library, 0, len(libraries))
	for _, lib := range libraries {
		if !lib.Enabled {
			continue
		}
		if preferredLibraryID != "" {
			if lib.ID == preferredLibraryID {
				return []model.Library{lib}
			}
			continue
		}
		enabled = append(enabled, lib)
	}
	if preferredLibraryID != "" {
		return nil
	}
	destRoot = strings.TrimSpace(destRoot)
	if destRoot == "" {
		return enabled
	}
	matched := make([]model.Library, 0, len(enabled))
	for _, lib := range enabled {
		if pathWithin(lib.Path, destRoot) || pathWithin(destRoot, lib.Path) {
			matched = append(matched, lib)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return enabled
}
