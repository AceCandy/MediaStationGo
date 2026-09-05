package service

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type SeriesCard struct {
	Key       string      `json:"key"`
	Rep       model.Media `json:"rep"`
	LinkMedia model.Media `json:"linkMedia"`
	Count     int         `json:"count"`
}

// GetMediaSeriesVisible 从可见文件定位整剧，复用 canonical 展示投影而非分集信息。
func (s *MediaService) GetMediaSeriesVisible(ctx context.Context, mediaID string, visibility MediaVisibility) (*model.MediaView, error) {
	media, err := s.repo.MediaView.FindByID(ctx, mediaID)
	if err != nil || media == nil || !visibility.AllowsView(media) || media.SeriesID == "" {
		return nil, err
	}
	rows, err := s.repo.MediaView.FindMetadataSearchRepresentatives(ctx, []string{media.SeriesID}, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: []string{media.LibraryID},
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	series := rows[0]
	if series.MetadataKind != model.MetadataKindSeries {
		return nil, nil
	}
	// 整剧不是可播放文件，不暴露代表集的技术和路径信息。
	series.Media = model.Media{PermanentBase: model.PermanentBase{ID: series.MetadataID}, MetadataID: series.MetadataID}
	series.SeasonNum, series.EpisodeNum = 0, 0
	series.SeasonID = ""
	if err := s.attachMediaProviderDetails(ctx, &series); err != nil {
		return nil, err
	}
	return &series, nil
}

type seriesCardGroup struct {
	card       SeriesCard
	latest     time.Time
	episodeIDs map[string]struct{}
}

func (s *MediaService) ListLibrarySeriesCards(ctx context.Context, libraryID string, visibility MediaVisibility) ([]SeriesCard, int64, error) {
	mediaVisibility := visibility
	mediaVisibility.MissingPoster = false
	mediaVisibility.MissingChineseTitle = false
	rows, _, err := s.listAllMediaVisible(ctx, libraryID, mediaVisibility)
	if err != nil {
		return nil, 0, err
	}
	cards := groupMediaSeriesCards(mediaViewsAsMedia(rows))
	cards = filterLibrarySeriesCards(cards, visibility)
	return cards, int64(len(cards)), nil
}

func filterLibrarySeriesCards(cards []SeriesCard, visibility MediaVisibility) []SeriesCard {
	if !visibility.MissingPoster && !visibility.MissingChineseTitle {
		return cards
	}
	filtered := cards[:0]
	for _, card := range cards {
		if visibility.MissingPoster && strings.TrimSpace(card.Rep.PosterURL) != "" {
			continue
		}
		title := firstNonEmpty(card.Rep.SeriesTitle, card.Rep.Title, card.Rep.OriginalName)
		if visibility.MissingChineseTitle && containsCJK(title) {
			continue
		}
		filtered = append(filtered, card)
	}
	return filtered
}

func seriesCardMetadataID(card SeriesCard) string {
	if card.Rep.SeriesID != "" {
		return card.Rep.SeriesID
	}
	return card.Rep.MetadataID
}

func (s *MediaService) ListRecentSeriesCards(ctx context.Context, limit int, visibility MediaVisibility) ([]SeriesCard, error) {
	if limit <= 0 {
		limit = 24
	} else if limit > 100 {
		limit = 100
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	rows, err := s.repo.MediaView.ListRecentLogicalWorks(ctx, limit, filter)
	if err != nil {
		return nil, err
	}
	s.attachLibraryMetadataViews(ctx, rows)
	cards := groupMediaSeriesCards(mediaViewsAsMedia(rows))
	recentAt := make(map[string]time.Time, len(cards))
	for _, row := range rows {
		id := row.MetadataID
		if row.SeriesID != "" {
			id = row.SeriesID
		}
		if row.CreatedAt.After(recentAt[id]) {
			recentAt[id] = row.CreatedAt
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		return recentAt[seriesCardMetadataID(cards[i])].After(recentAt[seriesCardMetadataID(cards[j])])
	})
	if len(cards) == 0 {
		return []SeriesCard{}, nil
	}
	if len(cards) > limit {
		cards = cards[:limit]
	}
	return cards, nil
}

func (s *MediaService) ListLibrarySeriesEpisodes(ctx context.Context, libraryID, key string, visibility MediaVisibility) ([]model.MediaView, error) {
	rows, _, err := s.listAllMediaVisible(ctx, libraryID, visibility)
	if err != nil {
		return nil, err
	}
	out := make([]model.MediaView, 0)
	groupingRows := mediaViewsAsMedia(rows)
	resolver := newMediaSeriesKeyResolver(groupingRows)
	for i, row := range rows {
		if resolver.key(groupingRows[i]) == key {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SeasonNum != out[j].SeasonNum {
			return out[i].SeasonNum < out[j].SeasonNum
		}
		if out[i].EpisodeNum != out[j].EpisodeNum {
			return out[i].EpisodeNum < out[j].EpisodeNum
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MediaService) listAllMediaVisible(ctx context.Context, libraryID string, visibility MediaVisibility) ([]model.MediaView, int64, error) {
	const pageSize = 2000
	var all []model.MediaView
	var total int64
	for page := 1; ; page++ {
		rows, n, err := s.ListMediaVisible(ctx, libraryID, page, pageSize, visibility)
		if err != nil {
			return nil, 0, err
		}
		if page == 1 {
			total = n
			all = make([]model.MediaView, 0, minInt64(n, pageSize))
		}
		all = append(all, rows...)
		if int64(len(all)) >= n || len(rows) < pageSize {
			break
		}
	}
	return all, total, nil
}

func groupMediaSeriesCards(items []model.Media) []SeriesCard {
	if len(items) == 0 {
		return nil
	}
	groups := make([]seriesCardGroup, 0)
	byKey := make(map[string]int, len(items))
	resolver := newMediaSeriesKeyResolver(items)
	for _, item := range items {
		key := resolver.key(item)
		if key == "" {
			continue
		}
		if idx, ok := byKey[key]; ok {
			group := &groups[idx]
			if latest := seriesMediaTime(item); latest.After(group.latest) {
				group.latest = latest
			}
			card := &group.card
			// A shared external ID means duplicate encodes/locations for movies,
			// not multiple episodes. Keep a single movie card without presenting
			// its versions as an "N episodes" collection.
			if mediaLooksEpisodicForGrouping(item) || mediaLooksEpisodicForGrouping(card.LinkMedia) {
				episodeID := firstNonEmpty(item.MetadataID, item.ID, item.Path)
				if _, seen := group.episodeIDs[episodeID]; !seen {
					group.episodeIDs[episodeID] = struct{}{}
					card.Count++
				}
			}
			if betterSeriesLinkMedia(item, card.LinkMedia) {
				card.LinkMedia = item
			}
			currentArtwork := seriesArtworkScore(item)
			representativeArtwork := seriesArtworkScore(card.Rep)
			if currentArtwork > representativeArtwork {
				card.Rep = item
			} else if currentArtwork == representativeArtwork {
				cur := item.SeasonNum*10000 + item.EpisodeNum
				rep := card.Rep.SeasonNum*10000 + card.Rep.EpisodeNum
				if cur > 0 && (rep == 0 || cur < rep) {
					card.Rep = item
				}
			}
			continue
		}
		byKey[key] = len(groups)
		episodeIDs := map[string]struct{}{}
		if mediaLooksEpisodicForGrouping(item) {
			episodeIDs[firstNonEmpty(item.MetadataID, item.ID, item.Path)] = struct{}{}
		}
		groups = append(groups, seriesCardGroup{
			card:       SeriesCard{Key: key, Rep: item, LinkMedia: item, Count: 1},
			latest:     seriesMediaTime(item),
			episodeIDs: episodeIDs,
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].latest.After(groups[j].latest)
	})
	cards := make([]SeriesCard, 0, len(groups))
	for _, group := range groups {
		cards = append(cards, group.card)
	}
	return cards
}

func seriesMediaTime(media model.Media) time.Time {
	if releaseDate := strings.TrimSpace(media.ReleaseDate); releaseDate != "" {
		if parsed, err := time.Parse("2006-01-02", releaseDate); err == nil {
			return parsed
		}
	}
	if media.Year > 0 {
		return time.Date(media.Year, time.December, 31, 0, 0, 0, 0, time.UTC)
	}
	if media.UpdatedAt.After(media.CreatedAt) {
		return media.UpdatedAt
	}
	return media.CreatedAt
}

func betterSeriesLinkMedia(candidate, current model.Media) bool {
	candidateScore := librarySpecificityScore(candidate)
	currentScore := librarySpecificityScore(current)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	return seriesArtworkScore(candidate) > seriesArtworkScore(current)
}

func librarySpecificityScore(media model.Media) int {
	rawPath := strings.TrimSpace(firstNonEmpty(media.DisplayLibraryPath, media.LibraryPath))
	if rawPath == "" {
		return 0
	}
	normalized := strings.TrimRight(strings.ReplaceAll(rawPath, "\\", "/"), "/")
	return 200 + len(nonEmptySlashParts(normalized))
}

func nonEmptySlashParts(value string) []string {
	parts := strings.Split(value, "/")
	out := parts[:0]
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

var (
	posterArtworkRE = regexp.MustCompile(`(poster|folder|cover|movie|show|pl)(?:[._-]|\.[a-z0-9]+$|$)`)
	badArtworkRE    = regexp.MustCompile(`(actor|actress|cast|avatar|sample|screenshot|screen|still|scene|fanart|backdrop|background|landscape|banner|logo|disc)`)
)

func seriesArtworkScore(media model.Media) int {
	poster := strings.ToLower(media.PosterURL)
	backdrop := strings.ToLower(media.BackdropURL)
	if poster == "" {
		if backdrop != "" {
			return 5
		}
		return 0
	}
	if posterArtworkRE.MatchString(poster) {
		return 40
	}
	if badArtworkRE.MatchString(poster) {
		return 10
	}
	if strings.Contains(poster, "thumb") {
		return 20
	}
	return 30
}

func minInt64(a int64, b int) int {
	if a <= 0 {
		return 0
	}
	if a > int64(b) {
		return b
	}
	return int(a)
}
