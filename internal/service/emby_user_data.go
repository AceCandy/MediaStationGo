package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// SetFavorite 按 Emby 作品身份保存收藏，MediaID 仅保留具体版本。
func (e *EmbyService) SetFavorite(ctx context.Context, userID, itemID string, favorite bool) error {
	target, err := e.itemTarget(ctx, itemID, userID)
	if err != nil {
		return err
	}
	if target.ItemID == "" || target.MetadataID == "" {
		return errors.New("media not found")
	}
	_, err = e.repo.Favorite.SetByIdentity(ctx, userID, target.MetadataID, target.MediaID, favorite)
	return err
}

// MarkPlayed 按作品身份标记已看，并保留当前具体版本。
func (e *EmbyService) MarkPlayed(ctx context.Context, userID, itemID string, played bool) error {
	target, err := e.itemTarget(ctx, itemID, userID)
	if err != nil || target.MetadataID == "" || target.MediaID == "" {
		return errors.New("media not found")
	}
	if !played {
		return e.repo.DB.WithContext(ctx).
			Where("user_id = ? AND metadata_id = ?", userID, target.MetadataID).
			Delete(&model.PlaybackHistory{}).Error
	}
	m, err := e.repo.Media.FindByID(ctx, target.MediaID)
	if err != nil || m == nil {
		return errors.New("media not found")
	}
	dur := int64(m.DurationSec) * 1000
	if dur <= 0 {
		dur = 1
	}
	return e.repo.History.Upsert(ctx, &model.PlaybackHistory{
		UserID:     userID,
		MetadataID: target.MetadataID,
		MediaID:    target.MediaID,
		PositionMs: dur,
		DurationMs: dur,
		WatchedAt:  time.Now(),
		Completed:  true,
	})
}

// RecordProgress 记录播放进度（来自 Emby 客户端的 /Sessions/Playing/Progress）。
func (e *EmbyService) RecordProgress(ctx context.Context, userID, itemID, mediaSourceID string, positionTicks, runtimeTicks int64) error {
	target, err := e.itemTarget(ctx, itemID, userID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(mediaSourceID) != "" {
		sourceTarget, sourceErr := e.itemTarget(ctx, mediaSourceID, userID)
		if sourceErr != nil {
			return sourceErr
		}
		sameItem := target.MetadataID != "" && sourceTarget.MetadataID == target.MetadataID
		if sourceTarget.MediaID != "" && sameItem {
			target.MediaID = sourceTarget.MediaID
		}
	}
	if target.MetadataID == "" || target.MediaID == "" {
		return errors.New("media not found")
	}
	pos := positionTicks / 10_000
	dur := runtimeTicks / 10_000
	if dur <= 0 {
		// runtimeTicks 缺失时回退到 media.DurationSec
		if m, _ := e.repo.Media.FindByID(ctx, target.MediaID); m != nil {
			dur = int64(m.DurationSec) * 1000
		}
	}
	completed := dur > 0 && pos >= dur*9/10
	return e.repo.History.Upsert(ctx, &model.PlaybackHistory{
		UserID:     userID,
		MetadataID: target.MetadataID,
		MediaID:    target.MediaID,
		PositionMs: pos,
		DurationMs: dur,
		WatchedAt:  time.Now(),
		Completed:  completed,
	})
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
