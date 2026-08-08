package service

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// resolveBaseRoot picks the organize destination root (目的地目录): a
// per-request override wins, then the organize.target_dir setting, then the
// library's own path.
func (o *OrganizerService) resolveBaseRoot(ctx context.Context, lib *model.Library, override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return r
	}
	if o.repo != nil && o.repo.Setting != nil {
		if v, err := o.repo.Setting.Get(ctx, "organize.target_dir"); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return lib.Path
}

// resolveSourceRoot picks the organize source root (源目录，待整理文件所在目录):
// a per-request override wins, then the organize.source_dir setting, then the
// library's own path. Library organize only touches media located under this
// root, so operators can point at a specific staging folder.
func (o *OrganizerService) resolveSourceRoot(ctx context.Context, lib *model.Library, override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return r
	}
	if o.repo != nil && o.repo.Setting != nil {
		if v, err := o.repo.Setting.Get(ctx, "organize.source_dir"); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return lib.Path
}

// resolveTransferMode picks the transfer mode: a per-request override wins,
// otherwise the organize.transfer_mode setting (default move).
func (o *OrganizerService) resolveTransferMode(ctx context.Context, override TransferMode) TransferMode {
	mode := override
	if mode == "" {
		mode = TransferMove
		if o.repo != nil && o.repo.Setting != nil {
			if v, err := o.repo.Setting.Get(ctx, "organize.transfer_mode"); err == nil && strings.TrimSpace(v) != "" {
				mode = parseTransferMode(v)
			}
		}
	}
	return mode
}

func (o *OrganizerService) autoAddLibraryEnabled(ctx context.Context) bool {
	if o == nil || o.repo == nil || o.repo.Setting == nil {
		return true
	}
	v, err := o.repo.Setting.Get(ctx, "organize.auto_add_library")
	if err != nil || strings.TrimSpace(v) == "" {
		return true
	}
	return parseBoolSetting(v, true)
}

func (o *OrganizerService) effectiveOrganizeOverrides(opts OrganizeOptions, explicitDest string) (string, string) {
	mediaType := normalizeOrganizeMediaType(opts.MediaType)
	category := sanitizeFilename(strings.TrimSpace(opts.MediaCategory))
	hasExplicitLayout := mediaType != "" || category != ""
	if category != "" {
		if impliedType, normalizedCategory := o.mediaTypeForDirectoryCategory(category); impliedType != "" {
			category = normalizedCategory
			if mediaType == "" {
				mediaType = impliedType
			}
		}
		return mediaType, category
	}
	if hasExplicitLayout {
		if layout := o.organizeLayoutFromDestPath(explicitDest); layout.Category != "" {
			category = sanitizeFilename(layout.Category)
			if mediaType == "" {
				mediaType = layout.MediaType
			}
		}
	}
	return mediaType, category
}

func (o *OrganizerService) organizeLayoutFromDestPath(dest string) organizeDirectoryLayout {
	dest = filepath.Clean(strings.TrimSpace(dest))
	if dest == "" || dest == "." {
		return organizeDirectoryLayout{}
	}
	if mediaType, category := o.mediaTypeForDirectoryCategory(filepath.Base(dest)); mediaType != "" && category != "" {
		return organizeDirectoryLayout{MediaType: mediaType, Category: category}
	}
	return organizeDirectoryLayout{}
}
