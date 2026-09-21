package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type embyItemTarget struct {
	ItemID        string
	MetadataID    string
	MediaID       string
	SourceID      string
	SourceEpisode int
	NFOItemID     string
}

func (e *EmbyService) userDataForTarget(ctx context.Context, userID string, target embyItemTarget) (bool, int64, bool) {
	if userID != "" && target.NFOItemID != "" {
		state, _ := e.repo.NFO.UserState(ctx, userID, target.NFOItemID, e.mediaQueryFilter(ctx, userID))
		return state.Favorite, state.PositionMs, state.Completed
	}
	if userID != "" && target.SourceID != "" {
		state, _ := e.repo.HongGuo.UserState(ctx, userID, target.SourceID, max(1, target.SourceEpisode), e.mediaQueryFilter(ctx, userID))
		favorite := false
		if target.SourceEpisode == 0 {
			workState, _ := e.repo.HongGuo.UserState(ctx, userID, target.SourceID, 0)
			favorite = workState.Favorite
		}
		return favorite, state.PositionMs, state.Completed
	}
	if strings.TrimSpace(userID) == "" || target.ItemID == "" || target.MetadataID == "" {
		return false, 0, false
	}
	favorite, _ := e.repo.Favorite.IsFavoriteByIdentity(ctx, userID, target.MetadataID, target.MediaID)
	var history model.PlaybackHistory
	q := e.repo.DB.WithContext(ctx).Table("(?) AS history", repository.PlaybackStates(ctx, e.repo.DB, "legacy", userID, e.mediaQueryFilter(ctx, userID))).Where("metadata_id = ?", target.MetadataID)
	if err := q.Order("watched_at desc").First(&history).Error; err != nil {
		return favorite, 0, false
	}
	return favorite, history.PositionMs, history.Completed
}

func (e *EmbyService) userDataForMetadataIDs(ctx context.Context, userID string, metadataIDs []string) (map[string]bool, map[string]int64, map[string]bool) {
	favorites := map[string]bool{}
	positions := map[string]int64{}
	completed := map[string]bool{}
	if e == nil || e.repo == nil || e.repo.DB == nil || strings.TrimSpace(userID) == "" || len(metadataIDs) == 0 {
		return favorites, positions, completed
	}
	ids := make([]string, 0, len(metadataIDs))
	seen := make(map[string]struct{}, len(metadataIDs))
	for _, id := range metadataIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return favorites, positions, completed
	}
	var favoriteRows []model.Favorite
	if err := e.repo.DB.WithContext(ctx).Where("user_id = ? AND metadata_id IN ?", userID, ids).Find(&favoriteRows).Error; err == nil {
		for _, favorite := range favoriteRows {
			favorites[favorite.MetadataID] = true
		}
	}
	var historyRows []model.PlaybackHistory
	if err := e.repo.DB.WithContext(ctx).Table("(?) AS history", repository.PlaybackStates(ctx, e.repo.DB, "legacy", userID, e.mediaQueryFilter(ctx, userID))).Where("metadata_id IN ?", ids).
		Order("watched_at desc").Find(&historyRows).Error; err == nil {
		for _, history := range historyRows {
			if _, ok := positions[history.MetadataID]; !ok {
				positions[history.MetadataID] = history.PositionMs
				completed[history.MetadataID] = history.Completed
			}
		}
	}
	return favorites, positions, completed
}

func embyItemID(m *model.MediaView) string {
	if m == nil {
		return ""
	}
	if m.CatalogSource == model.TaskSystemHongGuo || m.CatalogSource == model.CatalogSourceNFO {
		return m.CatalogItemID
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
	views, err := e.mediaViewsForItemID(ctx, id, userID)
	if err != nil || len(views) == 0 {
		return nil, err
	}
	if len(views) != 1 || views[0].ID != id {
		views = collapseMediaPartViews(views)
	}
	preferred := views[0]
	for i := 1; i < len(views); i++ {
		if preferMediaVersion(views[i].Media, preferred.Media) {
			preferred = views[i]
		}
	}
	return &preferred, nil
}

func (e *EmbyService) mediaViewsForItemID(ctx context.Context, id, userID string) ([]model.MediaView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	if strings.HasPrefix(id, "hg-") {
		return e.repo.MediaView.HongGuoItemViews(ctx, id, e.mediaQueryFilter(ctx, userID))
	}
	if strings.HasPrefix(id, "nfo-") {
		return e.repo.MediaView.NFOItemViews(ctx, id, e.mediaQueryFilter(ctx, userID))
	}
	if rows, err := e.repo.MediaView.FindByIDs(ctx, []string{id}, e.mediaQueryFilter(ctx, userID)); err != nil {
		return nil, err
	} else if len(rows) > 0 {
		return rows, nil
	}

	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("media.metadata_id = ?", id)
	q = e.applyUserMediaVisibility(ctx, q, userID)
	var rows []model.Media
	if err := q.Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	views, err := e.mediaViewsForRows(ctx, rows, userID)
	return views, err
}

func (e *EmbyService) itemTarget(ctx context.Context, id, userID string) (embyItemTarget, error) {
	if m, err := e.mediaViewForItemID(ctx, id, userID); err != nil {
		return embyItemTarget{}, err
	} else if m != nil {
		target := embyItemTarget{ItemID: embyItemID(m), MetadataID: m.MetadataID, MediaID: m.ID}
		if m.CatalogSource == model.CatalogSourceNFO {
			target.NFOItemID = m.CatalogItemID
			if strings.HasPrefix(id, "nfo-") {
				target.ItemID, target.NFOItemID = id, id
			}
		}
		if m.CatalogSource == model.TaskSystemHongGuo {
			target.SourceID, target.SourceEpisode = m.LookupCatalogID, m.EpisodeNum
		}
		return target, nil
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
