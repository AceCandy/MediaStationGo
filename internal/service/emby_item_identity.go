package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type embyItemTarget struct {
	ItemID     string
	MetadataID string
	MediaID    string
}

func (e *EmbyService) userDataForTarget(ctx context.Context, userID string, target embyItemTarget) (bool, int64) {
	if strings.TrimSpace(userID) == "" || target.ItemID == "" || target.MetadataID == "" {
		return false, 0
	}
	favorite, _ := e.repo.Favorite.IsFavoriteByIdentity(ctx, userID, target.MetadataID, target.MediaID)
	var history model.PlaybackHistory
	q := e.repo.DB.WithContext(ctx).Where("user_id = ? AND metadata_id = ?", userID, target.MetadataID)
	if err := q.Order("watched_at desc").First(&history).Error; err != nil {
		return favorite, 0
	}
	return favorite, history.PositionMs
}

func embyItemID(m *model.MediaView) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.MetadataID)
}

// PlayableMediaID resolves either a metadata item ID or a concrete media ID
// to the file version that should be opened by a stream endpoint.
func (e *EmbyService) PlayableMediaID(ctx context.Context, id, userID string) (string, error) {
	view, err := e.playableMedia(ctx, id, userID)
	if err != nil || view == nil {
		return "", err
	}
	return strings.TrimSpace(view.ID), nil
}

// mediaViewForItemID 同时接受作品 ID 和具体 MediaSource ID。
func (e *EmbyService) mediaViewForItemID(ctx context.Context, id, userID string) (*model.MediaView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	if rows, err := e.repo.MediaView.FindByIDs(ctx, []string{id}, e.mediaQueryFilter(ctx, userID)); err != nil {
		return nil, err
	} else if len(rows) > 0 {
		return &rows[0], nil
	}

	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("media.metadata_id = ?", id)
	q = e.applyUserMediaVisibility(ctx, q, userID)
	var rows []model.Media
	if err := q.Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	views, err := e.mediaViewsForRows(ctx, rows, userID)
	if err != nil || len(views) == 0 {
		return nil, err
	}
	preferred := views[0]
	for i := 1; i < len(views); i++ {
		if preferMediaVersion(views[i].Media, preferred.Media) {
			preferred = views[i]
		}
	}
	return &preferred, nil
}

func (e *EmbyService) itemTarget(ctx context.Context, id, userID string) (embyItemTarget, error) {
	if m, err := e.mediaViewForItemID(ctx, id, userID); err != nil {
		return embyItemTarget{}, err
	} else if m != nil {
		return embyItemTarget{ItemID: embyItemID(m), MetadataID: m.MetadataID, MediaID: m.ID}, nil
	}
	if series, ok, err := e.findSeriesGroup(ctx, id, userID); err != nil {
		return embyItemTarget{}, err
	} else if ok {
		mediaID := ""
		if len(series.Episodes) > 0 {
			mediaID = series.Episodes[0].ID
		}
		return embyItemTarget{ItemID: series.ID, MetadataID: series.ID, MediaID: mediaID}, nil
	}
	if season, ok, err := e.findSeasonGroup(ctx, id, userID); err != nil {
		return embyItemTarget{}, err
	} else if ok {
		mediaID := ""
		if len(season.Episodes) > 0 {
			mediaID = season.Episodes[0].ID
		}
		return embyItemTarget{ItemID: season.ID, MetadataID: season.ID, MediaID: mediaID}, nil
	}
	return embyItemTarget{}, nil
}
