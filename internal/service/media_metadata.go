package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type MediaMetadataUpdate struct {
	Title        *string  `json:"title"`
	OriginalName *string  `json:"original_name"`
	Overview     *string  `json:"overview"`
	Year         *int     `json:"year"`
	ReleaseDate  *string  `json:"release_date"`
	Rating       *float32 `json:"rating"`
	SeasonNum    *int     `json:"season_num"`
	EpisodeNum   *int     `json:"episode_num"`
	TMDbID       *int     `json:"tmdb_id"`
	BangumiID    *int     `json:"bangumi_id"`
	DoubanID     *string  `json:"douban_id"`
	TheTVDBID    *string  `json:"thetvdb_id"`
	Languages    *string  `json:"languages"`
	Countries    *string  `json:"countries"`
	Genres       *string  `json:"genres"`
	NSFW         *bool    `json:"nsfw"`
}

func (s *MediaService) UpdateMetadata(ctx context.Context, id string, req MediaMetadataUpdate) (*model.MediaView, error) {
	if s == nil || s.repo == nil || s.repo.DB == nil || s.repo.Metadata == nil || s.repo.MediaView == nil {
		return nil, errors.New("media service unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("media id required")
	}
	media, err := s.repo.Media.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if media == nil {
		return nil, errors.New("media not found")
	}
	view, err := s.repo.MediaView.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, errors.New("media not found")
	}

	target, isNew, err := s.manualMetadataTarget(ctx, media, view, req)
	if err != nil {
		return nil, err
	}
	applyManualMetadataUpdate(target, req)
	if strings.TrimSpace(target.Title) == "" {
		return nil, errors.New("title required")
	}
	target.Source = "manual"

	if target.Kind == model.MetadataKindEpisode {
		parent, parentErr := s.manualEpisodeParent(ctx, target)
		if parentErr != nil {
			return nil, parentErr
		}
		target.ParentID = &parent.ID
	}
	if isNew {
		if target.Kind == model.MetadataKindEpisode {
			target, err = s.repo.Metadata.UpsertEpisode(ctx, target)
		} else {
			target, err = s.repo.Metadata.UpsertCanonical(ctx, target, nil, "")
		}
	} else {
		err = s.repo.Metadata.Update(ctx, target)
	}
	if err != nil {
		return nil, err
	}
	if err := s.replaceManualIdentifiers(ctx, target, media, req, isNew); err != nil {
		return nil, err
	}
	updates := map[string]any{
		"metadata_id": target.ID, "scrape_status": "matched", "scrape_error": "", "local_metadata_hint": "",
	}
	if req.SeasonNum != nil {
		updates["season_num"] = clampNonNegativeInt(*req.SeasonNum)
	}
	if req.EpisodeNum != nil {
		updates["episode_num"] = clampNonNegativeInt(*req.EpisodeNum)
	}
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	s.repo.MediaView.RefreshMetadataIDs(ctx, media.MetadataID, target.ID)
	s.invalidateMediaCache(ctx)
	return s.repo.MediaView.FindByID(ctx, id)
}

func (s *MediaService) manualMetadataTarget(ctx context.Context, media *model.Media, view *model.MediaView, req MediaMetadataUpdate) (*model.MetadataItem, bool, error) {
	if strings.TrimSpace(media.MetadataID) != "" {
		item, err := s.repo.Metadata.FindByID(ctx, media.MetadataID)
		if err != nil || item != nil {
			return item, false, err
		}
	}
	season, episode := view.SeasonNum, view.EpisodeNum
	if req.SeasonNum != nil {
		season = clampNonNegativeInt(*req.SeasonNum)
	}
	if req.EpisodeNum != nil {
		episode = clampNonNegativeInt(*req.EpisodeNum)
	}
	kind := model.MetadataKindMovie
	if episode > 0 {
		kind = model.MetadataKindEpisode
	} else if lib, err := s.repo.Library.FindByID(ctx, media.LibraryID); err != nil {
		return nil, false, err
	} else if librarySupportsSeasons(lib) {
		kind = model.MetadataKindSeries
	}
	return &model.MetadataItem{
		Kind: kind, Title: firstNonEmpty(view.Title, media.Title), OriginalName: view.OriginalName,
		Overview: view.Overview, Rating: view.Rating,
		Year: view.Year, ReleaseDate: view.ReleaseDate, SeasonNum: season, EpisodeNum: episode,
		Languages: view.Languages, Countries: view.Countries, Genres: view.Genres,
		NSFW: view.NSFW, Source: "manual",
	}, true, nil
}

func (s *MediaService) manualEpisodeParent(ctx context.Context, episode *model.MetadataItem) (*model.MetadataItem, error) {
	if episode == nil || episode.ParentID == nil || strings.TrimSpace(*episode.ParentID) == "" {
		return nil, errors.New("episode season parent is required")
	}
	parent, err := s.repo.Metadata.FindByID(ctx, *episode.ParentID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, errors.New("episode season parent not found")
	}
	if parent.Kind != model.MetadataKindSeason {
		return nil, errors.New("episode parent must be a season")
	}
	return parent, nil
}

func applyManualMetadataUpdate(item *model.MetadataItem, req MediaMetadataUpdate) {
	if req.Title != nil {
		item.Title = strings.TrimSpace(*req.Title)
	}
	if req.OriginalName != nil {
		item.OriginalName = strings.TrimSpace(*req.OriginalName)
	}
	if req.Overview != nil {
		item.Overview = strings.TrimSpace(*req.Overview)
	}
	if req.Year != nil {
		item.Year = clampNonNegativeInt(*req.Year)
	}
	if req.ReleaseDate != nil {
		item.ReleaseDate = normalizeReleaseDate(*req.ReleaseDate)
	}
	if req.Rating != nil {
		item.Rating = clampRating(*req.Rating)
	}
	if req.SeasonNum != nil && item.Kind == model.MetadataKindEpisode {
		item.SeasonNum = clampNonNegativeInt(*req.SeasonNum)
	}
	if req.EpisodeNum != nil && item.Kind == model.MetadataKindEpisode {
		item.EpisodeNum = clampNonNegativeInt(*req.EpisodeNum)
	}
	if req.Languages != nil {
		item.Languages = normalizeMetadataCSV(*req.Languages)
	}
	if req.Countries != nil {
		item.Countries = normalizeMetadataCSV(*req.Countries)
	}
	if req.Genres != nil {
		item.Genres = normalizeMetadataCSV(*req.Genres)
	}
	if req.NSFW != nil {
		item.NSFW = *req.NSFW
	}
}

func (s *MediaService) replaceManualIdentifiers(ctx context.Context, item *model.MetadataItem, media *model.Media, req MediaMetadataUpdate, isNew bool) error {
	values := []struct {
		provider string
		value    string
		set      bool
	}{
		{provider: "tmdb", value: manualIntIdentifier(req.TMDbID, media.TMDbID), set: req.TMDbID != nil || (isNew && media.TMDbID > 0)},
		{provider: "bangumi", value: manualIntIdentifier(req.BangumiID, media.BangumiID), set: req.BangumiID != nil || (isNew && media.BangumiID > 0)},
		{provider: "douban", value: manualStringIdentifier(req.DoubanID, media.DoubanID), set: req.DoubanID != nil || (isNew && strings.TrimSpace(media.DoubanID) != "")},
		{provider: "thetvdb", value: manualStringIdentifier(req.TheTVDBID, media.TheTVDBID), set: req.TheTVDBID != nil || (isNew && strings.TrimSpace(media.TheTVDBID) != "")},
	}
	for _, entry := range values {
		if entry.provider == "douban" && item.Kind != model.MetadataKindMovie {
			continue
		}
		if entry.set {
			if err := s.repo.Metadata.ReplaceIdentifier(ctx, item.ID, entry.provider, item.Kind, entry.value); err != nil {
				return err
			}
		}
	}
	return nil
}

func manualIntIdentifier(value *int, fallback int) string {
	if value != nil {
		if n := clampNonNegativeInt(*value); n > 0 {
			return strconv.Itoa(n)
		}
		return ""
	}
	if fallback > 0 {
		return strconv.Itoa(fallback)
	}
	return ""
}

func manualStringIdentifier(value *string, fallback string) string {
	if value != nil {
		return strings.TrimSpace(*value)
	}
	return strings.TrimSpace(fallback)
}

func normalizeMetadataCSV(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r', '\t':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := strings.ToLower(part)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, ",")
}

func clampNonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func clampRating(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 10 {
		return 10
	}
	return value
}
