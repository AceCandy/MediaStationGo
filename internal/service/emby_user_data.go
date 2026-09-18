package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// SetFavorite 按 Emby 作品身份保存收藏，MediaID 仅保留具体版本。
func (e *EmbyService) SetFavorite(ctx context.Context, userID, itemID string, favorite bool) error {
	if handled, err := e.hongGuoContainerMutation(ctx, userID, itemID, &favorite, nil); handled {
		return err
	}
	target, err := e.itemTarget(ctx, itemID, userID)
	if err != nil {
		return err
	}
	if target.SourceID != "" {
		if target.SourceEpisode > 0 {
			return repository.ErrFavoriteUnsupportedType
		}
		return e.repo.HongGuo.SetFavorite(ctx, userID, target.SourceID, favorite)
	}
	if target.ItemID == "" || target.MetadataID == "" {
		return errors.New("media not found")
	}
	if _, err = e.repo.Favorite.SetByIdentity(ctx, userID, target.MetadataID, target.MediaID, favorite); err != nil {
		return err
	}
	if e.cache != nil {
		e.cache.DeletePrefix(ctx, embyItemsCachePrefix)
	}
	return nil
}

// MarkPlayed 按作品身份标记已看，并保留当前具体版本。
func (e *EmbyService) MarkPlayed(ctx context.Context, userID, itemID string, played bool) error {
	if handled, err := e.hongGuoContainerMutation(ctx, userID, itemID, nil, &played); handled {
		return err
	}
	metadata, err := e.repo.Metadata.FindByID(ctx, itemID)
	if err != nil {
		return err
	}
	if metadata != nil && (metadata.Kind == model.MetadataKindSeries || metadata.Kind == model.MetadataKindSeason) {
		err = e.markContainerPlayed(ctx, userID, itemID, played)
		if err == nil && e.cache != nil {
			e.cache.DeletePrefix(ctx, embyItemsCachePrefix)
		}
		return err
	}
	target, err := e.itemTarget(ctx, itemID, userID)
	if err == nil && target.SourceID != "" {
		media, err := e.mediaViewForItemID(ctx, target.MediaID, userID)
		if err != nil {
			return err
		}
		if media == nil {
			return errors.New("media not found")
		}
		return e.repo.HongGuo.MarkPlayed(ctx, userID, *media, played)
	}
	if err != nil || target.MetadataID == "" || target.MediaID == "" {
		return errors.New("media not found")
	}
	if !played {
		err = e.repo.DB.WithContext(ctx).
			Where("user_id = ? AND metadata_id = ?", userID, target.MetadataID).
			Delete(&model.PlaybackHistory{}).Error
	} else {
		dur := int64(0)
		if probe, _ := e.repo.MediaProbe.FindByMediaID(ctx, target.MediaID); probe != nil {
			dur = probe.DurationMS
		}
		err = e.repo.History.Upsert(ctx, &model.PlaybackHistory{
			UserID:     userID,
			MetadataID: target.MetadataID,
			MediaID:    target.MediaID,
			PositionMs: dur,
			DurationMs: dur,
			WatchedAt:  time.Now(),
			Completed:  true,
		})
	}
	if err == nil && e.cache != nil {
		e.cache.DeletePrefix(ctx, embyItemsCachePrefix)
	}
	return err
}

// RecordProgress 记录播放进度（来自 Emby 客户端的 /Sessions/Playing/Progress）。
func (e *EmbyService) RecordProgress(ctx context.Context, userID, itemID, mediaSourceID, sessionID string, positionTicks, runtimeTicks int64) error {
	itemID = strings.TrimSpace(itemID)
	mediaSourceID = strings.TrimSpace(mediaSourceID)
	target := embyItemTarget{}
	if mediaSourceID != "" {
		var source model.Media
		query := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("media.id = ?", mediaSourceID)
		sourceErr := e.applyUserMediaVisibility(ctx, query, userID).Take(&source).Error
		if sourceErr == nil && (itemID == source.ID || itemID == source.MetadataID) {
			target = embyItemTarget{ItemID: source.MetadataID, MetadataID: source.MetadataID, MediaID: source.ID}
		} else if sourceErr != nil && !errors.Is(sourceErr, gorm.ErrRecordNotFound) {
			return sourceErr
		}
	}
	if target.MediaID == "" {
		var err error
		target, err = e.itemTarget(ctx, itemID, userID)
		if err != nil {
			return err
		}
		if mediaSourceID != "" {
			sourceTarget, sourceErr := e.itemTarget(ctx, mediaSourceID, userID)
			if sourceErr != nil {
				return sourceErr
			}
			sameItem := target.MetadataID != "" && sourceTarget.MetadataID == target.MetadataID
			if target.SourceID != "" {
				sameItem = sourceTarget.SourceID == target.SourceID && sourceTarget.SourceEpisode == target.SourceEpisode
			}
			if sourceTarget.MediaID != "" && sameItem {
				target.MediaID = sourceTarget.MediaID
			}
		}
	}
	if target.SourceID != "" && target.MediaID != "" {
		dur := runtimeTicks / 10_000
		if dur <= 0 {
			if probe, _ := e.repo.MediaProbe.FindByMediaID(ctx, target.MediaID); probe != nil {
				dur = probe.DurationMS
			}
		}
		if dur <= 0 {
			return nil
		}
		return NewPlaybackService(e.log, e.repo).RecordProgress(ctx, userID, target.MediaID, sessionID, positionTicks/10_000, dur, e.mediaVisibility(ctx, userID))
	}
	if target.MetadataID == "" || target.MediaID == "" {
		return errors.New("media not found")
	}
	pos := positionTicks / 10_000
	dur := runtimeTicks / 10_000
	if dur <= 0 {
		if probe, _ := e.repo.MediaProbe.FindByMediaID(ctx, target.MediaID); probe != nil {
			dur = probe.DurationMS
		}
	}
	if dur <= 0 {
		return nil
	}
	if err := validatePlaybackProgress(pos, dur); err != nil {
		return err
	}
	if !shouldRecordPlaybackProgress(pos, dur) {
		return nil
	}
	history := &model.PlaybackHistory{
		UserID:     userID,
		MetadataID: target.MetadataID,
		MediaID:    target.MediaID,
		PositionMs: pos,
		DurationMs: dur,
		WatchedAt:  time.Now(),
		Completed:  playbackCompleted(pos, dur),
	}
	libraryID := ""
	if strings.TrimSpace(sessionID) != "" {
		media, err := e.repo.Media.FindByID(ctx, target.MediaID)
		if err != nil || media == nil {
			return errors.New("media not found")
		}
		libraryID = media.LibraryID
	}
	err := NewPlaybackService(e.log, e.repo).saveProgress(ctx, history, sessionID, libraryID, e.mediaVisibility(ctx, userID))
	if err == nil && history.Completed && e.cache != nil {
		e.cache.DeletePrefix(ctx, embyItemsCachePrefix)
	}
	return err
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func intToStr(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}
