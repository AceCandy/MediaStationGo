package service

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func (e *EmbyService) applyUserMediaVisibility(ctx context.Context, q *gorm.DB, userID string) *gorm.DB {
	visibility := e.mediaVisibility(ctx, userID)
	// Persisted media must always point at a live metadata row. Keep this join
	// aligned with MediaView so counts and payloads cannot disagree on orphaned
	// media rows.
	q = q.Joins("JOIN metadata_items AS emby_metadata ON emby_metadata.id = media.metadata_id")
	if !visibility.IncludeNSFW {
		if hidden := visibility.HiddenLibraryIDs; len(hidden) > 0 {
			q = q.Where("media.library_id <> ALL(?)", &hidden)
		}
	}
	if len(visibility.AllowedLibraryIDs) > 0 {
		q = q.Where("media.library_id = ANY(?)", &visibility.AllowedLibraryIDs)
	}
	return q
}

func (e *EmbyService) mediaQueryFilter(ctx context.Context, userID string) repository.MediaQueryFilter {
	visibility := e.mediaVisibility(ctx, userID)
	if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
		visibility.AllowedLibraryIDs = []string{"__locked__"}
	}
	return repository.MediaQueryFilter{
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
}

func (e *EmbyService) mediaViewsForRows(ctx context.Context, rows []model.Media, userID string) ([]model.MediaView, error) {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	return e.repo.MediaView.FindByIDs(ctx, ids, e.mediaQueryFilter(ctx, userID))
}

func (e *EmbyService) mediaVisibility(ctx context.Context, userID string) MediaVisibility {
	if e == nil {
		return MediaVisibility{IncludeNSFW: true}
	}
	key := e.repo.ReadCacheKey() + strings.TrimSpace(userID)
	// 认证快照可能早于权限修改；按本次快照隔离，不能污染修改后的请求。
	if user, ok := ctx.Value(authenticatedUserKey{}).(authenticatedUserVisibility); ok && user.id == userID {
		if user.hideAdult {
			key += ":hidden"
		} else {
			key += ":visible"
		}
	}
	now := time.Now()
	e.visibilityMu.RLock()
	entry, ok := e.visibilityCache[key]
	e.visibilityMu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return cloneMediaVisibility(entry.visibility)
	}

	visibility := UserDefaultMediaVisibility(ctx, e.repo, userID)
	visibility = cloneMediaVisibility(visibility)

	e.visibilityMu.Lock()
	if e.visibilityCache == nil {
		e.visibilityCache = make(map[string]embyVisibilityCacheEntry)
	}
	if len(e.visibilityCache) > 1000 {
		e.visibilityCache = make(map[string]embyVisibilityCacheEntry)
	}
	e.visibilityCache[key] = embyVisibilityCacheEntry{
		visibility: cloneMediaVisibility(visibility),
		expiresAt:  now.Add(embyVisibilityCacheTTL),
	}
	e.visibilityMu.Unlock()

	return visibility
}

func (e *EmbyService) mergedLibraryIDs(ctx context.Context, libraryID string) []string {
	return []string{libraryID}
}

func cloneMediaVisibility(visibility MediaVisibility) MediaVisibility {
	if visibility.AllowedLibraryIDs != nil {
		visibility.AllowedLibraryIDs = append([]string(nil), visibility.AllowedLibraryIDs...)
	}
	if visibility.HiddenLibraryIDs != nil {
		visibility.HiddenLibraryIDs = append([]string(nil), visibility.HiddenLibraryIDs...)
	}
	return visibility
}

func (e *EmbyService) libraryVisibleFromCachedVisibility(lib model.Library, visibility MediaVisibility) bool {
	if len(visibility.AllowedLibraryIDs) > 0 {
		allowed := false
		for _, id := range visibility.AllowedLibraryIDs {
			if id == lib.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	if visibility.IncludeNSFW {
		return true
	}
	for _, id := range visibility.HiddenLibraryIDs {
		if id == lib.ID {
			return false
		}
	}
	return true
}
