package service

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (o *OrganizerService) refreshOrganizeMediaMetadata(ctx context.Context, media *model.Media, lib *model.Library, requestedType string) error {
	if o == nil || media == nil || lib == nil || !organizeMediaNeedsMetadataRefresh(*media) {
		return nil
	}
	mediaType := normalizeOrganizeMediaType(requestedType)
	if mediaType == "" {
		mediaType = normalizeOrganizeMediaType(lib.Type)
	}
	match := o.lookupReclassifyMetadata(ctx, *media, *lib, mediaType)
	if match == nil {
		return nil
	}
	refreshed := mediaWithReclassifyMatch(*media, match)
	if err := o.persistOrganizerMatch(ctx, &refreshed, lib, match); err != nil {
		return err
	}
	*media = refreshed
	if o.log != nil {
		o.log.Info("organize media metadata refreshed before rename",
			zap.String("media", media.ID),
			zap.String("path", media.Path),
			zap.String("title", media.Title),
			zap.Int("tmdb_id", media.TMDbID),
			zap.Int("bangumi_id", media.BangumiID),
			zap.String("douban_id", media.DoubanID),
			zap.String("thetvdb_id", media.TheTVDBID))
	}
	return nil
}

func organizeMediaNeedsMetadataRefresh(media model.Media) bool {
	if strings.TrimSpace(media.ScrapeStatus) != "matched" {
		return true
	}
	if organizeMediaTitleLooksLikeRelease(media.Title) {
		return true
	}
	return media.TMDbID <= 0 &&
		media.BangumiID <= 0 &&
		strings.TrimSpace(media.DoubanID) == "" &&
		strings.TrimSpace(media.TheTVDBID) == ""
}

func organizeMediaTitleLooksLikeRelease(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || organizeWeakFileTitle(title) {
		return true
	}
	if season, episode := ParseEpisode(title); season > 0 || episode > 0 {
		return true
	}
	normalized := strings.ToLower(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(title))
	for _, field := range strings.Fields(normalized) {
		if _, ok := releaseBoundaryTokenSet[field]; ok {
			return true
		}
	}
	return false
}

func (o *OrganizerService) persistOrganizerMatch(ctx context.Context, media *model.Media, lib *model.Library, match *Match) error {
	if o == nil || o.scraper == nil || o.repo == nil || o.repo.DB == nil || media == nil || lib == nil || match == nil {
		return nil
	}
	var (
		persisted *persistedMetadataMatch
		err       error
	)
	if strings.EqualFold(strings.TrimSpace(match.Source), "local_nfo") {
		persisted, err = o.scraper.persistLocalMetadata(ctx, media, lib, localMetadataFromMatch(match))
	} else {
		persisted, err = o.scraper.persistProviderMetadata(ctx, media, lib, match)
	}
	if err != nil {
		return err
	}
	updates := map[string]any{
		"metadata_id": persisted.Target.ID, "scrape_status": "matched",
		"scrape_error": "", "local_metadata_hint": "",
	}
	applyScrapeMediaTypeResets(updates, match)
	if err := o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", media.ID).Updates(updates).Error; err != nil {
		return err
	}
	oldMetadataID := media.MetadataID
	media.MetadataID = persisted.Target.ID
	media.ScrapeStatus = "matched"
	o.repo.MediaView.RefreshMetadataIDs(ctx, oldMetadataID, media.MetadataID)
	return nil
}

func localMetadataFromMatch(match *Match) *LocalMetadata {
	if match == nil {
		return nil
	}
	return &LocalMetadata{
		Title: match.Title, OriginalName: match.OriginalName, Overview: match.Overview,
		PosterURL: match.PosterURL, BackdropURL: match.BackdropURL, Year: match.Year,
		ReleaseDate: match.ReleaseDate, Rating: match.Rating, TMDbID: match.TMDbID,
		BangumiID: match.BangumiID, DoubanID: match.DoubanID, TheTVDBID: match.TheTVDBID,
		Languages: strings.Join(match.Languages, ","), Countries: strings.Join(match.Countries, ","),
		Genres: strings.Join(match.Genres, ","), NSFW: match.NSFW, HasNFO: true,
	}
}
