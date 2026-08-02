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

// RecordProgress upserts the resume position for a (user, media) pair. A
// position within 30 seconds of the duration auto-flags the item as
// completed so the home page can hide it from "Continue Watching".
func (p *PlaybackService) RecordProgress(ctx context.Context, userID, mediaID string, position, duration int64) error {
	completed := duration > 0 && position >= duration-30_000
	return p.recordProgress(ctx, userID, mediaID, position, duration, completed)
}

func (p *PlaybackService) RecordProgressEvent(ctx context.Context, userID, mediaID string, position, duration int64, completed bool) error {
	return p.recordProgress(ctx, userID, mediaID, position, duration, completed)
}

func (p *PlaybackService) recordProgress(ctx context.Context, userID, mediaID string, position, duration int64, completed bool) error {
	if userID == "" || mediaID == "" {
		return errors.New("missing user or media")
	}
	media, err := p.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return err
	}
	if media == nil {
		return errors.New("media not found")
	}
	h := &model.PlaybackHistory{
		UserID:     userID,
		MetadataID: media.MetadataID,
		MediaID:    mediaID,
		PositionMs: position,
		DurationMs: duration,
		WatchedAt:  time.Now(),
		Completed:  completed,
	}
	return p.repo.History.Upsert(ctx, h)
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
	Media *model.MediaView `json:"media,omitempty"`
}

// RecentHistory returns the most recently-watched items for a user. We
// fetch the history rows first then attach each Media row in a single
// follow-up query.
func (p *PlaybackService) RecentHistory(ctx context.Context, userID string, limit int, visibility MediaVisibility) ([]HistoryItem, error) {
	return p.historyItems(ctx, userID, limit, nil, visibility)
}

func (p *PlaybackService) ContinueHistory(ctx context.Context, userID string, limit int, visibility MediaVisibility) ([]HistoryItem, error) {
	completed := false
	return p.historyItems(ctx, userID, limit, &completed, visibility)
}

func (p *PlaybackService) historyItems(ctx context.Context, userID string, limit int, completed *bool, visibility MediaVisibility) ([]HistoryItem, error) {
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, p.repo, visibility)
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	rows, err := p.repo.History.ListByUserFiltered(ctx, userID, limit, completed, filter)
	if err != nil {
		return nil, err
	}
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
	favs, err := p.repo.Favorite.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(favs) == 0 {
		return nil, nil
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, p.repo, visibility)
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	out := make([]model.MediaView, 0, len(favs))
	for _, favorite := range favs {
		media, err := p.mediaViewForState(ctx, favorite.MetadataID, favorite.MediaID, filter)
		if err != nil {
			return nil, err
		}
		if media != nil {
			out = append(out, *media)
		}
	}
	return out, nil
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
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, p.repo, visibility)
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
