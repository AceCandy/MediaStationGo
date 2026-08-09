package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type persistedMetadataMatch struct {
	Target      *model.MetadataItem
	Series      *model.MetadataItem
	PosterURL   string
	BackdropURL string
}

func (s *ScraperService) persistProviderMetadata(ctx context.Context, media *model.Media, lib *model.Library, match *Match) (*persistedMetadataMatch, error) {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || media == nil || match == nil {
		return nil, errors.New("metadata persistence is unavailable")
	}
	source := metadataMatchSource(match)
	mediaType := s.determineMediaTypeForMedia(lib, media, match)
	entityKind := model.MetadataKindMovie
	if mediaType == "tv" {
		entityKind = model.MetadataKindSeries
	}
	identifiers := metadataIdentifiersFromMatch(match, entityKind)
	base := metadataItemFromMatch(match, entityKind, source)
	preferredID := media.MetadataID
	if entityKind == model.MetadataKindSeries {
		preferredID = media.SeriesID
	}
	canonical, err := s.repo.Metadata.UpsertCanonicalWithMerge(ctx, base, identifiers, preferredID, match.AllowIdentifierMerge)
	if err != nil {
		return nil, err
	}
	result := &persistedMetadataMatch{Target: canonical}
	if entityKind == model.MetadataKindSeries {
		result.Series = canonical
	}
	seriesPoster, seriesBackdrop, err := s.persistMetadataArtwork(ctx, canonical.ID, source, match.PosterURL, match.BackdropURL)
	if err != nil {
		return nil, err
	}
	result.PosterURL, result.BackdropURL = seriesPoster, seriesBackdrop
	if err := s.persistCredits(ctx, canonical.ID, match.LoadedCreditTypes, match.Credits); err != nil {
		return nil, err
	}

	if entityKind != model.MetadataKindSeries || media.EpisodeNum <= 0 {
		return result, nil
	}
	season, err := s.upsertSeasonMetadata(ctx, canonical, media.SeasonNum, source)
	if err != nil {
		return nil, err
	}
	episode := metadataItemFromMatch(match, model.MetadataKindEpisode, source)
	episode.ParentID = &season.ID
	episode.SeasonNum = 0
	episode.EpisodeNum = media.EpisodeNum
	episode.EpisodeTitle = strings.TrimSpace(media.EpisodeTitle)
	episode.Overview = ""
	episode.Rating = 0
	if existing, findErr := s.repo.Metadata.FindEpisode(ctx, canonical.ID, media.SeasonNum, media.EpisodeNum); findErr != nil {
		return nil, findErr
	} else if existing != nil {
		preserveEpisodeDetails(episode, existing)
	}
	target, err := s.repo.Metadata.UpsertEpisode(ctx, episode)
	if err != nil {
		return nil, err
	}
	result.Target = target
	posterURL, backdropURL, err := s.persistMetadataArtwork(ctx, target.ID, source, match.PosterURL, match.BackdropURL)
	if err != nil {
		return nil, err
	}
	result.PosterURL, result.BackdropURL = posterURL, backdropURL
	return result, nil
}

func (s *ScraperService) persistLocalMetadata(ctx context.Context, media *model.Media, lib *model.Library, local *LocalMetadata) (*persistedMetadataMatch, error) {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || media == nil || local == nil {
		return nil, errors.New("local metadata persistence is unavailable")
	}
	seriesLike := mediaIsEpisodic(media, lib)
	entityKind := model.MetadataKindMovie
	if seriesLike {
		entityKind = model.MetadataKindSeries
	}
	match := localMetadataMatch(local, entityKind)
	identifiers := metadataIdentifiersFromMatch(match, entityKind)
	if entityKind == model.MetadataKindSeries && len(identifiers) == 0 {
		identifiers = append(identifiers, model.MetadataIdentifier{
			Provider: "local", EntityKind: model.MetadataKindSeries, ExternalID: localSeriesIdentity(media),
		})
	}
	preferredID := media.MetadataID
	if entityKind == model.MetadataKindSeries {
		preferredID = media.SeriesID
	}
	base := metadataItemFromMatch(match, entityKind, "local")
	canonical, err := s.repo.Metadata.UpsertCanonical(ctx, base, identifiers, preferredID)
	if err != nil {
		return nil, err
	}
	result := &persistedMetadataMatch{Target: canonical}
	if entityKind == model.MetadataKindSeries {
		result.Series = canonical
	}
	posterURL, backdropURL, err := s.persistMetadataArtwork(ctx, canonical.ID, "local", local.PosterURL, local.BackdropURL)
	if err != nil {
		return nil, err
	}
	result.PosterURL, result.BackdropURL = posterURL, backdropURL
	if err := s.persistCredits(ctx, canonical.ID, local.LoadedCreditTypes, local.Credits); err != nil {
		return nil, err
	}
	if entityKind != model.MetadataKindSeries || media.EpisodeNum <= 0 {
		return result, nil
	}
	season, err := s.upsertSeasonMetadata(ctx, canonical, media.SeasonNum, "local")
	if err != nil {
		return nil, err
	}
	episode := metadataItemFromMatch(match, model.MetadataKindEpisode, "local")
	episode.ParentID = &season.ID
	episode.SeasonNum = 0
	episode.EpisodeNum = media.EpisodeNum
	episode.EpisodeTitle = strings.TrimSpace(local.EpisodeTitle)
	if existing, findErr := s.repo.Metadata.FindEpisode(ctx, canonical.ID, media.SeasonNum, media.EpisodeNum); findErr != nil {
		return nil, findErr
	} else if existing != nil {
		preserveMissingLocalEpisodeDetails(episode, existing)
	}
	target, err := s.repo.Metadata.UpsertEpisode(ctx, episode)
	if err != nil {
		return nil, err
	}
	result.Target = target
	posterURL, backdropURL, err = s.persistMetadataArtwork(ctx, target.ID, "local", local.PosterURL, local.BackdropURL)
	if err != nil {
		return nil, err
	}
	result.PosterURL, result.BackdropURL = posterURL, backdropURL
	episodeCredits, episodeLoaded := local.Credits, local.LoadedCreditTypes
	if len(local.EpisodeLoadedCreditTypes) > 0 {
		episodeCredits, episodeLoaded = local.EpisodeCredits, local.EpisodeLoadedCreditTypes
	}
	if err := s.persistCredits(ctx, target.ID, episodeLoaded, episodeCredits); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ScraperService) persistCredits(ctx context.Context, metadataID string, loaded []string, credits []PersonCredit) error {
	if s == nil || s.repo == nil || s.repo.Person == nil || len(loaded) == 0 {
		return nil
	}
	inputs := make([]repository.CreditInput, 0, len(credits))
	for _, credit := range credits {
		input := repository.CreditInput{Provider: credit.Provider, ExternalID: credit.ExternalID, Name: credit.Name, Overview: credit.Overview, ProfileURL: credit.ProfileURL, Type: credit.Type, OriginalRole: credit.OriginalRole, SortOrder: credit.SortOrder}
		if s.people != nil && strings.TrimSpace(credit.ProfileURL) != "" {
			if key, err := s.people.Import(ctx, credit.ProfileURL); err != nil {
				if s.log != nil {
					s.log.Warn("people image localization failed during scrape", zap.String("person", credit.Name), zap.Error(err))
				}
			} else {
				input.ProfileImageKey = key
			}
		}
		inputs = append(inputs, input)
	}
	if err := s.repo.Person.ReplaceCredits(ctx, metadataID, loaded, inputs); err != nil {
		return err
	}
	s.queuePeopleTranslation()
	return nil
}

const peopleAITranslateSettingKey = "metadata.people_ai_translate"

func (s *ScraperService) upsertSeasonMetadata(ctx context.Context, series *model.MetadataItem, seasonNum int, source string) (*model.MetadataItem, error) {
	if series == nil {
		return nil, errors.New("series metadata is required")
	}
	return s.repo.Metadata.UpsertSeason(ctx, &model.MetadataItem{
		Kind:      model.MetadataKindSeason,
		ParentID:  &series.ID,
		SeasonNum: seasonNum,
		Title:     seasonName(seasonNum),
		Source:    source,
	})
}

func (s *ScraperService) persistMetadataArtwork(ctx context.Context, metadataID, provider, posterSource, backdropSource string) (string, string, error) {
	posterURL, err := s.persistOneMetadataArtwork(ctx, metadataID, model.ArtworkTypePoster, provider, posterSource)
	if err != nil {
		return "", "", err
	}
	backdropURL, err := s.persistOneMetadataArtwork(ctx, metadataID, model.ArtworkTypeBackdrop, provider, backdropSource)
	if err != nil {
		return "", "", err
	}
	return posterURL, backdropURL, nil
}

func (s *ScraperService) persistOneMetadataArtwork(ctx context.Context, metadataID, artworkType, provider, source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" || s.artwork == nil {
		return "", nil
	}
	var (
		asset *model.ArtworkAsset
		err   error
	)
	switch {
	case isHTTPish(source):
		asset, err = s.artwork.ImportRemote(ctx, metadataID, artworkType, provider, source)
	case func() bool { _, _, ok := ParseCloudArtworkURL(source); return ok }():
		asset, err = s.artwork.ImportCloud(ctx, metadataID, artworkType, source)
	default:
		asset, err = s.artwork.ImportLocal(ctx, metadataID, artworkType, source)
	}
	if err != nil {
		return "", err
	}
	return ArtworkURL(asset.ID), nil
}

func metadataItemFromMatch(match *Match, kind, source string) *model.MetadataItem {
	return &model.MetadataItem{
		Kind: kind, Title: strings.TrimSpace(match.Title), OriginalName: strings.TrimSpace(match.OriginalName),
		Overview: strings.TrimSpace(match.Overview), Rating: match.Rating, Year: match.Year,
		ReleaseDate: strings.TrimSpace(match.ReleaseDate), Languages: strings.Join(match.Languages, ","),
		Countries: strings.Join(match.Countries, ","), Genres: strings.Join(match.Genres, ","),
		NSFW: match.NSFW, Source: source,
	}
}

func metadataIdentifiersFromMatch(match *Match, entityKind string) []model.MetadataIdentifier {
	if match == nil {
		return nil
	}
	out := make([]model.MetadataIdentifier, 0, 4)
	if match.TMDbID > 0 {
		out = append(out, model.MetadataIdentifier{Provider: "tmdb", EntityKind: entityKind, ExternalID: strconv.Itoa(match.TMDbID)})
	}
	if match.BangumiID > 0 {
		out = append(out, model.MetadataIdentifier{Provider: "bangumi", EntityKind: entityKind, ExternalID: strconv.Itoa(match.BangumiID)})
	}
	if value := strings.TrimSpace(match.DoubanID); value != "" {
		out = append(out, model.MetadataIdentifier{Provider: "douban", EntityKind: entityKind, ExternalID: value})
	}
	if value := strings.TrimSpace(match.TheTVDBID); value != "" {
		out = append(out, model.MetadataIdentifier{Provider: "thetvdb", EntityKind: entityKind, ExternalID: value})
	}
	if value := strings.TrimSpace(match.IMDbID); value != "" {
		out = append(out, model.MetadataIdentifier{Provider: "imdb", EntityKind: entityKind, ExternalID: value})
	}
	if len(out) == 0 && match.Source == "adult" && strings.TrimSpace(match.OriginalName) != "" {
		out = append(out, model.MetadataIdentifier{Provider: "adult", EntityKind: entityKind, ExternalID: strings.TrimSpace(match.OriginalName)})
	}
	return out
}

func metadataMatchSource(match *Match) string {
	if match == nil {
		return "manual"
	}
	if source := strings.ToLower(strings.TrimSpace(match.Source)); source != "" {
		return source
	}
	switch {
	case match.TMDbID > 0:
		return "tmdb"
	case strings.TrimSpace(match.DoubanID) != "":
		return "douban"
	case match.BangumiID > 0:
		return "bangumi"
	case strings.TrimSpace(match.TheTVDBID) != "":
		return "thetvdb"
	case match.NSFW:
		return "adult"
	default:
		return "manual"
	}
}

func localMetadataMatch(local *LocalMetadata, entityKind string) *Match {
	mediaType := "movie"
	if entityKind == model.MetadataKindSeries {
		mediaType = "tv"
	}
	return &Match{
		Source: "local_nfo", MediaType: mediaType, Title: strings.TrimSpace(local.Title),
		OriginalName: strings.TrimSpace(firstText(local.OriginalName, local.AdultCode)),
		Overview:     strings.TrimSpace(local.Overview), PosterURL: strings.TrimSpace(local.PosterURL),
		BackdropURL: strings.TrimSpace(local.BackdropURL), Year: local.Year,
		ReleaseDate: strings.TrimSpace(local.ReleaseDate), Rating: local.Rating,
		TMDbID: local.TMDbID, BangumiID: local.BangumiID, DoubanID: strings.TrimSpace(local.DoubanID),
		TheTVDBID: strings.TrimSpace(local.TheTVDBID), Languages: splitNFOList(local.Languages),
		Countries: splitNFOList(local.Countries), Genres: splitNFOList(local.Genres), NSFW: local.NSFW,
	}
}

func localSeriesIdentity(media *model.Media) string {
	path := ""
	if media != nil {
		path = showDirFromEpisodePath(media.Path)
		if strings.TrimSpace(path) == "" {
			path = filepath.Dir(media.Path)
		}
	}
	path = strings.ToLower(filepath.Clean(strings.TrimSpace(path)))
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

func preserveEpisodeDetails(next, existing *model.MetadataItem) {
	if next == nil || existing == nil {
		return
	}
	next.EpisodeTitle = existing.EpisodeTitle
	next.Overview = existing.Overview
	next.Rating = existing.Rating
	next.ReleaseDate = existing.ReleaseDate
	if existing.Year > 0 {
		next.Year = existing.Year
	}
}

func preserveMissingLocalEpisodeDetails(next, existing *model.MetadataItem) {
	if next.EpisodeTitle == "" {
		next.EpisodeTitle = existing.EpisodeTitle
	}
	if next.Overview == "" {
		next.Overview = existing.Overview
	}
	if next.Rating <= 0 {
		next.Rating = existing.Rating
	}
	if next.ReleaseDate == "" {
		next.ReleaseDate = existing.ReleaseDate
	}
	if next.Year <= 0 {
		next.Year = existing.Year
	}
}
