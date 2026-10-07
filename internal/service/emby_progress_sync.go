package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ErrProgressSyncTarget 拒绝容器、不可见文件或不属于条目的具体版本。
var ErrProgressSyncTarget = errors.New("媒体条目或具体版本不可用")

// EmbyProgressSnapshot 是准确同步扩展，Revision 用字符串避免客户端整数精度丢失。
type EmbyProgressSnapshot struct {
	MediaSourceID string `json:"MediaSourceId"`
	PositionTicks int64  `json:"PositionTicks"`
	RunTimeTicks  int64  `json:"RunTimeTicks"`
	Played        bool   `json:"Played"`
	Revision      string `json:"Revision"`
}

func (e *EmbyService) progressSyncTarget(ctx context.Context, userID, itemID, sourceID string) (repository.ProgressIdentity, *model.MediaView, error) {
	if sourceID == "" {
		sourceID = itemID
	}
	view, err := e.mediaViewForItemID(ctx, sourceID, userID)
	if err != nil {
		return repository.ProgressIdentity{}, nil, err
	}
	if view == nil || (itemID != view.ID && itemID != embyItemID(view)) || (sourceID != itemID && view.ID != sourceID) {
		return repository.ProgressIdentity{}, nil, ErrProgressSyncTarget
	}
	target := embyTargetForView(itemID, view)
	id := repository.ProgressIdentity{UserID: userID, Source: "legacy", ItemID: target.MetadataID, MediaID: view.ID}
	switch {
	case target.NFOItemID != "":
		id.Source, id.ItemID = "nfo", strings.TrimPrefix(target.NFOItemID, "nfo-")
	case target.SourceID != "":
		id.Source, id.ItemID, id.EpisodeNumber = target.Source, target.SourceID, max(1, target.SourceEpisode)
	}
	if id.ItemID == "" {
		return id, nil, ErrProgressSyncTarget
	}
	return id, view, nil
}

func (e *EmbyService) ReadProgressSnapshot(ctx context.Context, userID, itemID, sourceID string) (EmbyProgressSnapshot, error) {
	id, view, err := e.progressSyncTarget(ctx, userID, itemID, sourceID)
	if err != nil {
		return EmbyProgressSnapshot{}, err
	}
	state, err := repository.ReadPlaybackProgress(ctx, e.repo.DB, id, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return EmbyProgressSnapshot{}, err
	}
	duration := state.DurationMs
	if view.PartGroupKey == "" && view.ProbeDurationMS > 0 {
		duration = view.ProbeDurationMS
	}
	return EmbyProgressSnapshot{MediaSourceID: view.ID, PositionTicks: state.PositionMs * 10_000,
		RunTimeTicks: duration * 10_000, Played: state.Completed, Revision: strconv.FormatInt(state.Revision, 10)}, nil
}

// SyncProgressSnapshot 只允许明确的文件及版本条件；同步不产生真实播放统计。
func (e *EmbyService) SyncProgressSnapshot(ctx context.Context, userID, itemID, sourceID string, expected, positionTicks, runtimeTicks int64, played bool) (EmbyProgressSnapshot, error) {
	id, view, err := e.progressSyncTarget(ctx, userID, itemID, sourceID)
	if err != nil {
		return EmbyProgressSnapshot{}, err
	}
	if sourceID == "" || sourceID != view.ID || positionTicks < 0 || runtimeTicks <= 0 || positionTicks > runtimeTicks || positionTicks%10_000 != 0 || runtimeTicks%10_000 != 0 {
		return EmbyProgressSnapshot{}, fmt.Errorf("%w: 播放进度参数无效", ErrInvalidPlaybackProgress)
	}
	position, duration := positionTicks/10_000, runtimeTicks/10_000
	if view.PartGroupKey == "" && view.ProbeDurationMS > 0 && duration != view.ProbeDurationMS {
		return EmbyProgressSnapshot{}, fmt.Errorf("%w: 具体版本时长已变化", ErrInvalidPlaybackProgress)
	}
	revision, err := repository.WritePlaybackProgress(ctx, e.repo.DB, id, expected, position, duration, played)
	if err != nil {
		return EmbyProgressSnapshot{}, err
	}
	if e.cache != nil {
		e.cache.DeletePrefix(ctx, embyItemsCachePrefix)
	}
	return EmbyProgressSnapshot{MediaSourceID: view.ID, PositionTicks: positionTicks,
		RunTimeTicks: runtimeTicks, Played: played, Revision: strconv.FormatInt(revision, 10)}, nil
}
