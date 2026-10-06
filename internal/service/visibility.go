package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const AdultLibraryIDsSettingKey = "adult.library_ids"

// AdultContentEnabled reads the global Adult / NSFW switch.
func AdultContentEnabled(ctx context.Context, repo *repository.Container) bool {
	if repo == nil || repo.Setting == nil {
		return true
	}
	value, err := repo.Setting.Get(ctx, "adult.enabled")
	if err != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled", "启用", "开启":
		return true
	case "0", "false", "no", "off", "disabled", "禁用", "关闭":
		return false
	default:
		return true
	}
}

// UserHidesAdult reports whether a user's own lock overrides all profiles.
func UserHidesAdult(ctx context.Context, repo *repository.Container, userID string) bool {
	if strings.TrimSpace(userID) == "" || repo == nil || repo.User == nil {
		return false
	}
	if user, ok := ctx.Value(authenticatedUserKey{}).(authenticatedUserVisibility); ok && user.id == userID {
		return user.hideAdult
	}
	user, err := repo.User.FindByID(ctx, userID)
	return err == nil && user != nil && user.HideAdult
}

// UserDefaultMediaVisibility is the visibility policy used by clients that
// cannot pass a web play-profile token, notably Emby/Jellyfin-compatible apps.
func UserDefaultMediaVisibility(ctx context.Context, repo *repository.Container, userID string) MediaVisibility {
	if repo != nil && userID != "" && repo.PlayProfile != nil {
		rows, err := repo.PlayProfile.ListByUser(ctx, userID)
		if err == nil {
			for i := range rows {
				if rows[i].IsDefault {
					return UserProfileMediaVisibility(ctx, repo, userID, &rows[i])
				}
			}
		}
	}
	return UserProfileMediaVisibility(ctx, repo, userID, nil)
}

// UserProfileMediaVisibility 复用已选定配置，避免 Web 再读取默认配置和用户。
func UserProfileMediaVisibility(ctx context.Context, repo *repository.Container, userID string, profile *model.PlayProfile) MediaVisibility {
	visibility := MediaVisibility{IncludeNSFW: AdultContentEnabled(ctx, repo) && !UserHidesAdult(ctx, repo, userID)}
	if profile != nil {
		visibility.LibraryRestricted = true
		visibility.IncludeNSFW = visibility.IncludeNSFW && profile.AllowAdult
		visibility.AllowedLibraryIDs = DecodeAllowedLibraryIDs(profile.AllowedLibraryIDs)
	}
	visibility.HiddenLibraryIDs = hiddenAdultLibraryIDs(ctx, repo, visibility.IncludeNSFW)
	return visibility
}

// DecodeAllowedLibraryIDs normalises a PlayProfile allowed-library JSON string.
func DecodeAllowedLibraryIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	out := ids[:0]
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			out = append(out, strings.TrimSpace(id))
		}
	}
	return out
}

// LibraryVisibleForUser applies profile library limits and adult-directory
// hiding to a library card/folder.
func LibraryVisibleForUser(ctx context.Context, repo *repository.Container, lib model.Library, visibility MediaVisibility) bool {
	if len(visibility.AllowedLibraryIDs) > 0 {
		found := false
		for _, id := range visibility.AllowedLibraryIDs {
			if id == lib.ID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if lib.Type == model.LibraryTypeHuangGuoAI && !visibility.IncludeNSFW {
		return false
	}
	if visibility.IncludeNSFW {
		return true
	}
	hiddenLibraryIDs := visibility.HiddenLibraryIDs
	configuredAdultLibraryIDs := AdultLibraryIDs(ctx, repo)
	hasConfiguredAdultLibraries := len(hiddenLibraryIDs) > 0 || len(configuredAdultLibraryIDs) > 0
	if len(hiddenLibraryIDs) == 0 {
		hiddenLibraryIDs = configuredAdultLibraryIDs
	}
	for _, id := range hiddenLibraryIDs {
		if id == lib.ID {
			return false
		}
	}
	if hasConfiguredAdultLibraries {
		return true
	}
	if LibraryLooksAdult(lib) {
		return false
	}
	return true
}

// LibraryLooksAdult catches adult-only roots even before all rows are scraped.
func LibraryLooksAdult(lib model.Library) bool {
	text := strings.ToLower(strings.TrimSpace(lib.Name + " " + lib.Path + " " + lib.Type))
	if text == "" {
		return false
	}
	for _, token := range []string{"成人", "限制级", "nsfw", "adult", "jav", "javdb", "javbus", "9kg", "里番", "番号"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func AdultLibraryIDs(ctx context.Context, repo *repository.Container) []string {
	if repo == nil || repo.Setting == nil {
		return nil
	}
	raw, err := repo.Setting.Get(ctx, AdultLibraryIDsSettingKey)
	if err != nil {
		return nil
	}
	return DecodeAllowedLibraryIDs(raw)
}

func hiddenAdultLibraryIDs(ctx context.Context, repo *repository.Container, includeNSFW bool) []string {
	if includeNSFW {
		return nil
	}
	ids := AdultLibraryIDs(ctx, repo)
	hasConfigured := len(ids) > 0
	if repo != nil && repo.DB != nil {
		var sourceIDs []string
		if err := repo.DB.WithContext(ctx).Model(&model.Library{}).Where("type=?", model.LibraryTypeHuangGuoAI).Pluck("id", &sourceIDs).Error; err == nil {
			ids = append(ids, sourceIDs...)
		}
	}
	if hasConfigured || repo == nil || repo.Library == nil {
		return ids
	}
	// 未配置成人库时沿用库名称/路径判断，所有文件入口使用同一库级范围。
	libraries, err := repo.Library.ListBasic(ctx)
	if err != nil {
		return nil
	}
	for _, library := range libraries {
		if library.Type == model.LibraryTypeHuangGuoAI || LibraryLooksAdult(library) {
			ids = append(ids, library.ID)
		}
	}
	return ids
}
