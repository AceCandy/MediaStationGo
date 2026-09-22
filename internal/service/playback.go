// Package service — playback history / favourites / playlists.
//
// These three concerns are intentionally co-located: they all sit between
// "the user" and "a media item" and share the same join-table flavour. A
// dedicated PlaybackService keeps the wiring simple and lets handlers
// dispatch by feature instead of by repository.
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// PlaybackService bundles history / favourite / playlist business logic.
type PlaybackService struct {
	log  *zap.Logger
	repo *repository.Container
}

// NewPlaybackService is the constructor.
func NewPlaybackService(log *zap.Logger, repo *repository.Container) *PlaybackService {
	return &PlaybackService{log: log, repo: repo}
}

// ─── History ────────────────────────────────────────────────────────────────

const (
	playbackRecordThresholdMs = int64(20_000)
	shortPlaybackDurationMs   = int64(10 * 60 * 1000)
)

var ErrInvalidPlaybackProgress = errors.New("invalid playback progress")

func validatePlaybackProgress(position, duration int64) error {
	if position < 0 {
		return fmt.Errorf("%w: position must not be negative", ErrInvalidPlaybackProgress)
	}
	if duration <= 0 {
		return fmt.Errorf("%w: duration must be positive", ErrInvalidPlaybackProgress)
	}
	if position > duration {
		return fmt.Errorf("%w: position %d exceeds duration %d", ErrInvalidPlaybackProgress, position, duration)
	}
	return nil
}

func playbackCompleted(position, duration int64) bool {
	if position < 0 || duration <= 0 {
		return false
	}
	if duration < shortPlaybackDurationMs {
		threshold := duration - 30_000
		if threshold < 0 {
			threshold = 0
		}
		return position >= threshold
	}
	return position >= duration*9/10
}

func shouldRecordPlaybackProgress(position, duration int64) bool {
	if duration > shortPlaybackDurationMs {
		return position >= 60_000
	}
	return position >= playbackRecordThresholdMs
}

// RecordProgress validates and stores automatic playback progress for visible media.
func (p *PlaybackService) RecordProgress(ctx context.Context, userID, mediaID, sessionID string, position, duration int64, visibility MediaVisibility) error {
	if userID == "" || mediaID == "" {
		return errors.New("missing user or media")
	}
	if err := validatePlaybackProgress(position, duration); err != nil {
		return err
	}
	if !shouldRecordPlaybackProgress(position, duration) {
		return nil
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	rows, err := p.repo.MediaView.FindByIDs(ctx, []string{mediaID}, filter)
	if err != nil {
		return err
	}
	if len(rows) > 0 && rows[0].PartGroupKey == "" && rows[0].ProbeDurationMS > 0 {
		// 当前文件的探测时长优先；不同版本或旧客户端可携带过期片长。
		duration = rows[0].ProbeDurationMS
		position = min(position, duration)
	}
	if len(rows) > 0 && rows[0].CatalogSource == model.CatalogSourceNFO {
		if !visibility.AllowsView(&rows[0]) {
			return errors.New("media not found")
		}
		completed := playbackCompleted(position, duration)
		autoMark, err := p.autoMarkPreviousEpisodes(ctx, completed)
		if err != nil {
			return err
		}
		return p.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			repos := repository.New(tx)
			if err := repos.NFO.RecordProgress(ctx, userID, sessionID, rows[0], position, duration, completed); err != nil {
				return err
			}
			if autoMark {
				return repos.NFO.MarkPreviousEpisodes(ctx, userID, rows[0], filter)
			}
			return nil
		})
	}
	if len(rows) > 0 && rows[0].CatalogSource == model.TaskSystemHongGuo {
		if !visibility.AllowsView(&rows[0]) {
			return errors.New("media not found")
		}
		completed := playbackCompleted(position, duration)
		autoMark, err := p.autoMarkPreviousEpisodes(ctx, completed)
		if err != nil {
			return err
		}
		if !autoMark {
			return p.repo.HongGuo.RecordProgress(ctx, userID, sessionID, rows[0], position, duration, completed)
		}
		return p.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			repos := repository.New(tx)
			if err := repos.HongGuo.RecordProgress(ctx, userID, sessionID, rows[0], position, duration, completed); err != nil {
				return err
			}
			return repos.HongGuo.MarkPreviousEpisodes(ctx, userID, rows[0].LookupCatalogID, rows[0].EpisodeNum, filter)
		})
	}
	if len(rows) == 0 || rows[0].MetadataID == "" {
		return errors.New("media not found")
	}
	media := rows[0]
	h := &model.PlaybackHistory{
		UserID:     userID,
		MetadataID: media.MetadataID,
		MediaID:    mediaID,
		PositionMs: position,
		DurationMs: duration,
		WatchedAt:  time.Now(),
		Completed:  playbackCompleted(position, duration),
	}
	return p.saveProgress(ctx, h, sessionID, media.LibraryID, visibility)
}

func (p *PlaybackService) DeleteHistoryForMedia(ctx context.Context, userID, mediaID string, completed *bool) (int64, error) {
	media, err := p.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return 0, err
	}
	q := p.repo.DB.WithContext(ctx).Where("user_id = ?", userID)
	if media == nil {
		return 0, errors.New("media not found")
	}
	if media.CatalogSource == model.CatalogSourceNFO {
		var binding model.NFOMediaBinding
		if err := p.repo.DB.WithContext(ctx).First(&binding, "media_id = ?", mediaID).Error; err != nil {
			return 0, err
		}
		q = q.Model(&model.NFOUserState{}).Where("item_id = ? AND watched_at IS NOT NULL", binding.ItemID)
		if completed != nil {
			q = q.Where("completed = ?", *completed)
		}
		result := q.Updates(map[string]any{"position_ms": 0, "duration_ms": 0, "resume_position_ms": nil, "completed": false, "watched_at": nil})
		return result.RowsAffected, result.Error
	}
	q = q.Where("metadata_id = ?", media.MetadataID)
	if completed != nil {
		q = q.Where("completed = ?", *completed)
	}
	result := q.Delete(&model.PlaybackHistory{})
	return result.RowsAffected, result.Error
}

// HistoryItem joins the playback row with its media so the API consumer
// gets a fully-populated card without a second round-trip.
type HistoryItem struct {
	model.PlaybackHistory
	Media  *model.MediaView `json:"media,omitempty"`
	IsNext bool             `json:"is_next,omitempty"`
}

// RecentHistory returns the most recently-watched items for a user. We
// fetch the history rows first then attach each Media row in a single
// follow-up query.
func (p *PlaybackService) RecentHistory(ctx context.Context, userID string, limit int, visibility MediaVisibility) ([]HistoryItem, error) {
	return p.historyItems(ctx, userID, limit, nil, visibility)
}

func (p *PlaybackService) ContinueHistory(ctx context.Context, userID string, limit int, visibility MediaVisibility) ([]HistoryItem, error) {
	return p.continueHistory(ctx, userID, limit, "", visibility)
}

// ContinueSeriesHistory 只返回当前媒体库中指定剧集的一个跨季续播或下一集候选。
func (p *PlaybackService) ContinueSeriesHistory(ctx context.Context, userID, libraryID, seriesID string, visibility MediaVisibility) (*HistoryItem, error) {
	if seriesID == "" || !visibility.allows(libraryID, false) {
		return nil, nil
	}
	visibility.AllowedLibraryIDs = []string{libraryID}
	items, err := p.continueHistory(ctx, userID, 1, seriesID, visibility)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func (p *PlaybackService) continueHistory(ctx context.Context, userID string, limit int, seriesID string, visibility MediaVisibility) ([]HistoryItem, error) {
	if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
		return []HistoryItem{}, nil
	}
	filter := repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs}
	candidates, _, err := p.repo.History.Continuations(ctx, userID, filter, repository.ContinuationWeb, seriesID, 0, limit)
	if err != nil {
		return nil, err
	}
	rows := make([]model.PlaybackHistory, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, model.PlaybackHistory{Base: model.Base{ID: candidate.HistoryID}, UserID: userID,
			MetadataID: candidate.ItemID, MediaID: candidate.MediaID, PositionMs: candidate.PositionMs,
			DurationMs: candidate.DurationMs, WatchedAt: candidate.WatchedAt, Completed: candidate.Completed})
	}
	items, err := p.hydrateHistory(ctx, rows, filter)
	for i := range items {
		items[i].IsNext = candidates[i].IsNext
	}
	return items, err
}

func (p *PlaybackService) historyItems(ctx context.Context, userID string, limit int, completed *bool, visibility MediaVisibility) ([]HistoryItem, error) {
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	rows, err := p.repo.History.ListByUserFiltered(ctx, userID, limit, completed, filter)
	if err != nil {
		return nil, err
	}
	if has, err := p.repo.NFO.HasMedia(ctx); err != nil {
		return nil, err
	} else if has {
		local, err := p.repo.NFO.History(ctx, userID, limit, completed, filter)
		if err != nil {
			return nil, err
		}
		rows = append(rows, local...)
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].WatchedAt.Equal(rows[j].WatchedAt) {
				return rows[i].ID > rows[j].ID
			}
			return rows[i].WatchedAt.After(rows[j].WatchedAt)
		})
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}
	}
	return p.hydrateHistory(ctx, rows, filter)
}

// hydrateHistory 只补齐当前页展示信息，供真实历史和只读续播候选共用。
func (p *PlaybackService) hydrateHistory(ctx context.Context, rows []model.PlaybackHistory, filter repository.MediaQueryFilter) ([]HistoryItem, error) {
	mediaIDs := make([]string, 0, len(rows))
	for i := range rows {
		if rows[i].MediaID != "" {
			mediaIDs = append(mediaIDs, rows[i].MediaID)
		}
	}
	mediaByID := map[string]model.MediaView{}
	if len(mediaIDs) > 0 {
		mediaRows, err := p.repo.MediaView.FindByIDs(ctx, mediaIDs, filter)
		if err != nil {
			return nil, err
		}
		for _, media := range mediaRows {
			mediaByID[media.ID] = media
		}
	}
	items := make([]HistoryItem, 0, len(rows))
	for i := range rows {
		if m, ok := mediaByID[rows[i].MediaID]; ok {
			media := m
			items = append(items, HistoryItem{PlaybackHistory: rows[i], Media: &media})
			continue
		}
		media, err := p.mediaViewForState(ctx, rows[i].MetadataID, rows[i].MediaID, filter)
		if err != nil {
			return nil, err
		}
		items = append(items, HistoryItem{PlaybackHistory: rows[i], Media: media})
	}
	// 观看记录使用竖版海报；只在展示时借用季或整剧的图片。
	artworkIDs := []string{}
	for _, item := range items {
		if m := item.Media; m != nil && m.PosterURL == "" && m.MetadataKind == model.MetadataKindEpisode && m.CatalogSource != model.TaskSystemHongGuo {
			artworkIDs = append(artworkIDs, m.SeasonID, m.SeriesID)
		}
	}
	if len(artworkIDs) > 0 {
		selections, err := p.repo.Artwork.ListSelectionsByMetadataIDs(ctx, artworkIDs)
		if err != nil {
			return nil, err
		}
		posters := map[string]string{}
		for _, selection := range selections {
			if selection.ArtworkType == model.ArtworkTypePoster {
				posters[selection.MetadataID] = ArtworkURL(selection.AssetID)
			}
		}
		for _, item := range items {
			if m := item.Media; m != nil && m.PosterURL == "" && m.MetadataKind == model.MetadataKindEpisode && m.CatalogSource != model.TaskSystemHongGuo {
				m.PosterURL = firstNonEmpty(posters[m.SeasonID], posters[m.SeriesID], m.BackdropURL)
			}
		}
	}
	return items, nil
}

// ─── Favourites ─────────────────────────────────────────────────────────────

// ToggleFavourite flips the favourite flag and reports the new state.
func (p *PlaybackService) ToggleFavourite(ctx context.Context, userID, mediaID string) (bool, error) {
	return p.repo.Favorite.Toggle(ctx, userID, mediaID)
}

func (p *PlaybackService) SetFavourite(ctx context.Context, userID, mediaID string, favorite bool) (bool, error) {
	return p.repo.Favorite.Set(ctx, userID, mediaID, favorite)
}

func (p *PlaybackService) IsFavourite(ctx context.Context, userID, mediaID string) (bool, error) {
	return p.repo.Favorite.IsFavorite(ctx, userID, mediaID)
}

// ListFavourites returns every favourited media for a user.
func (p *PlaybackService) ListFavourites(ctx context.Context, userID string, visibility MediaVisibility) ([]model.MediaView, error) {
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	rows, err := p.repo.MediaView.ListFavoriteCards(ctx, userID, filter)
	if err != nil {
		return nil, err
	}
	if has, err := p.repo.NFO.HasMedia(ctx); err != nil {
		return nil, err
	} else if has {
		local, err := p.repo.NFO.FavoriteCards(ctx, userID, filter)
		if err != nil {
			return nil, err
		}
		rows = append(rows, local...)
	}
	return rows, nil
}

// ─── Playlists ──────────────────────────────────────────────────────────────

// CreatePlaylist persists a new playlist owned by userID.
func (p *PlaybackService) CreatePlaylist(ctx context.Context, userID, name string, isPublic bool) (*model.Playlist, error) {
	if name == "" {
		return nil, errors.New("name required")
	}
	pl := &model.Playlist{UserID: userID, Name: name, IsPublic: isPublic}
	if err := p.repo.Playlist.Create(ctx, pl); err != nil {
		return nil, err
	}
	return pl, nil
}

// ListPlaylists returns every playlist owned by userID.
func (p *PlaybackService) ListPlaylists(ctx context.Context, userID string) ([]model.Playlist, error) {
	return p.repo.Playlist.ListByUser(ctx, userID)
}

// PlaylistDetail returns the playlist together with its ordered media items.
type PlaylistDetail struct {
	Playlist model.Playlist    `json:"playlist"`
	Items    []model.MediaView `json:"items"`
}

// GetPlaylist returns the playlist + its ordered media. Visibility is
// enforced at the handler level; the service trusts callers.
func (p *PlaybackService) GetPlaylist(ctx context.Context, playlistID string, visibility MediaVisibility) (*PlaylistDetail, error) {
	var pl model.Playlist
	if err := p.repo.DB.Where("id = ?", playlistID).First(&pl).Error; err != nil {
		return nil, err
	}
	var rows []model.PlaylistItem
	if err := p.repo.DB.
		Where("playlist_id = ?", playlistID).
		Order("position asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &PlaylistDetail{Playlist: pl}, nil
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	media := make([]model.MediaView, 0, len(rows))
	for _, row := range rows {
		view, err := p.mediaViewForState(ctx, row.MetadataID, row.MediaID, filter)
		if err != nil {
			return nil, err
		}
		if view != nil {
			media = append(media, *view)
		}
	}
	return &PlaylistDetail{Playlist: pl, Items: media}, nil
}

// AddToPlaylist appends a media item to the end of a playlist.
func (p *PlaybackService) AddToPlaylist(ctx context.Context, playlistID, mediaID string) error {
	media, err := p.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return err
	}
	if media == nil {
		return errors.New("media not found")
	}
	var count int64
	if err := p.repo.DB.Model(&model.PlaylistItem{}).
		Where("playlist_id = ?", playlistID).Count(&count).Error; err != nil {
		return err
	}
	var existing model.PlaylistItem
	err = p.repo.DB.Where("playlist_id = ? AND metadata_id = ?", playlistID, media.MetadataID).First(&existing).Error
	if err == nil {
		return p.repo.DB.Model(&existing).Update("media_id", mediaID).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	item := &model.PlaylistItem{
		PlaylistID: playlistID,
		MetadataID: media.MetadataID,
		MediaID:    mediaID,
		Position:   int(count) + 1,
	}
	return p.repo.DB.Create(item).Error
}

// RemoveFromPlaylist removes a media item from a playlist (idempotent).
func (p *PlaybackService) RemoveFromPlaylist(ctx context.Context, playlistID, mediaID string) error {
	media, err := p.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return err
	}
	q := p.repo.DB.Where("playlist_id = ?", playlistID)
	if media == nil {
		return errors.New("media not found")
	}
	q = q.Where("metadata_id = ?", media.MetadataID)
	return q.
		Delete(&model.PlaylistItem{}).Error
}

func (p *PlaybackService) ReorderPlaylist(ctx context.Context, playlistID string, mediaIDs []string) error {
	for position, mediaID := range mediaIDs {
		media, err := p.repo.Media.FindByID(ctx, mediaID)
		if err != nil {
			return err
		}
		q := p.repo.DB.WithContext(ctx).Model(&model.PlaylistItem{}).Where("playlist_id = ?", playlistID)
		if media == nil {
			return errors.New("media not found")
		}
		q = q.Where("metadata_id = ?", media.MetadataID)
		if err := q.Update("position", position).Error; err != nil {
			return err
		}
	}
	return nil
}

func (p *PlaybackService) mediaViewForState(ctx context.Context, metadataID, mediaID string, filter repository.MediaQueryFilter) (*model.MediaView, error) {
	if mediaID != "" {
		rows, err := p.repo.MediaView.FindByIDs(ctx, []string{mediaID}, filter)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			if rows[0].MetadataID == metadataID {
				return &rows[0], nil
			}
		}
	}
	rows, err := p.repo.MediaView.FindByMetadataID(ctx, metadataID, filter)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// DeletePlaylist removes a playlist and all of its items.
func (p *PlaybackService) DeletePlaylist(ctx context.Context, playlistID string) error {
	if err := p.repo.DB.Where("playlist_id = ?", playlistID).
		Delete(&model.PlaylistItem{}).Error; err != nil {
		return err
	}
	return p.repo.DB.Where("id = ?", playlistID).Delete(&model.Playlist{}).Error
}
