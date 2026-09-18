package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

const autoMarkPreviousEpisodesSetting = "playback.auto_mark_previous_episodes"

func (p *PlaybackService) autoMarkPreviousEpisodes(ctx context.Context, completed bool) (bool, error) {
	if !completed {
		return false, nil
	}
	value, err := p.repo.Setting.Get(ctx, autoMarkPreviousEpisodesSetting)
	return parseBoolSetting(value, false), err
}

// saveProgress 统一网页与 Emby 的历史、前集补标和真实事件事务；无需加载媒体展示投影。
func (p *PlaybackService) saveProgress(ctx context.Context, history *model.PlaybackHistory, sessionID, libraryID string, visibility MediaVisibility) error {
	autoMark, err := p.autoMarkPreviousEpisodes(ctx, history.Completed)
	if err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" && !autoMark {
		return p.repo.History.Upsert(ctx, history)
	}
	return p.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repos := repository.New(tx)
		if err := repos.History.Upsert(ctx, history); err != nil {
			return err
		}
		if autoMark && !(visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0) {
			filter := repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs}
			if err := repos.History.MarkPreviousEpisodes(ctx, history.UserID, history.MetadataID, filter, history.WatchedAt); err != nil {
				return err
			}
		}
		if sessionID == "" {
			return nil
		}
		return repos.PlaybackEvent.Insert(ctx, &model.PlaybackEvent{
			UserID: history.UserID, SessionID: sessionID, MetadataID: history.MetadataID,
			MediaID: history.MediaID, LibraryID: libraryID, PlayedAt: history.WatchedAt,
		})
	})
}
