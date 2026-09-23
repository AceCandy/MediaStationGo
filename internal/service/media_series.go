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
	Key            string         `json:"key"`
	Rep            model.Media    `json:"rep"`
	LinkMedia      model.Media    `json:"linkMedia"`
	Count          int            `json:"count"`
	Seasons        []int          `json:"seasons,omitempty"`
	SeasonMediaIDs map[int]string `json:"season_media_ids,omitempty"`
}

// GetMediaSeriesVisible 从可见文件定位整剧，复用 canonical 展示投影而非分集信息。
func (s *MediaService) GetMediaSeriesVisible(ctx context.Context, mediaID string, visibility MediaVisibility) (*model.MediaView, error) {
	media, err := s.repo.MediaView.FindByID(ctx, mediaID)
	if err != nil || media == nil || !visibility.AllowsView(media) || media.SeriesID == "" {
		return nil, err
	}
	series, err := s.repo.MediaView.FindSeriesPresentation(ctx, media.SeriesID, visibility.IncludeNSFW)
	if err != nil || series == nil {
		return nil, err
	}
	if err := s.attachMediaProviderDetails(ctx, series); err != nil {
		return nil, err
	}
	return series, nil
}

// GetMediaSeasonVisible 仅沿可见文件的真实关联读取季，不按扫描季号猜测归属。
func (s *MediaService) GetMediaSeasonVisible(ctx context.Context, mediaID string, visibility MediaVisibility) (*model.MediaView, error) {
	media, err := s.repo.MediaView.FindByID(ctx, mediaID)
	if err != nil || media == nil || !visibility.AllowsView(media) || media.SeasonID == "" {
		return nil, err
	}
	season, err := s.repo.MediaView.FindSeasonPresentation(ctx, media.SeasonID, visibility.IncludeNSFW)
	if err != nil || season == nil {
		return nil, err
	}
	season.SeasonID = media.SeasonID
	season.SeriesID = media.SeriesID
	if season.PosterURL == "" && media.CatalogSource != model.TaskSystemHongGuo {
		series, findErr := s.repo.MediaView.FindSeriesPresentation(ctx, media.SeriesID, visibility.IncludeNSFW)
		if findErr != nil {
			return nil, findErr
		}
		if series != nil {
			season.PosterURL = series.PosterURL
		}
	}
	if err := s.attachMediaProviderDetails(ctx, season); err != nil {
		return nil, err
	}
	return season, nil
}

type seriesCardGroup struct {
	card       SeriesCard
	latest     time.Time
	episodeIDs map[string]struct{}
}

func (s *MediaService) ListLibrarySeriesCards(ctx context.Context, libraryID string, page, pageSize int, seriesID, key string, visibility MediaVisibility) ([]SeriesCard, int64, error) {
	page, pageSize = normalizeGroupedMediaPage(page, pageSize)
	if !visibility.allows(libraryID, false) {
		return []SeriesCard{}, 0, nil
	}
	if seriesID == "" && key != "" {
		var err error
		seriesID, err = s.resolveLibrarySeriesKey(ctx, libraryID, key, visibility)
		if err != nil {
			return nil, 0, err
		}
		if seriesID == "" {
			return []SeriesCard{}, 0, nil
		}
	}
	filter := repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs, MissingPoster: visibility.MissingPoster, MissingChineseTitle: visibility.MissingChineseTitle}
	rows, summaries, total, err := s.repo.MediaView.ListLibraryMetadataPage(ctx, libraryID, model.MetadataKindSeries, seriesID, (page-1)*pageSize, pageSize, filter)
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadataViews(ctx, rows)
	byID := make(map[string]model.Media, len(rows))
	for _, row := range mediaViewsAsMedia(rows) {
		byID[row.ID] = row
	}
	cards := make([]SeriesCard, 0, len(summaries))
	for _, summary := range summaries {
		if row, ok := byID[summary.MediaID]; ok {
			if row.CatalogSource != model.TaskSystemHongGuo || row.SeriesID != "" {
				row.SeriesID = summary.MetadataID
			}
			cards = append(cards, SeriesCard{Key: "metadata:" + summary.MetadataID, Rep: row, LinkMedia: row, Count: summary.Count})
		}
	}
	if err := s.attachSeriesCardPresentations(ctx, cards, visibility); err != nil {
		return nil, 0, err
	}
	if seriesID != "" && len(cards) == 1 {
		seasons, err := s.repo.MediaView.ListLibrarySeriesSeasons(ctx, libraryID, seriesID, repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		if err != nil {
			return nil, 0, err
		}
		cards[0].SeasonMediaIDs = make(map[int]string, len(seasons))
		for _, season := range seasons {
			cards[0].Seasons = append(cards[0].Seasons, season.Season)
			cards[0].SeasonMediaIDs[season.Season] = season.MediaID
		}
	}
	if key != "" && len(cards) == 1 {
		cards[0].Key = key
	}
	return cards, total, nil
}

// resolveLibrarySeriesKey 接受 canonical ID；旧哈希链接只扫描作品 ID，不读取分集。
func (s *MediaService) resolveLibrarySeriesKey(ctx context.Context, libraryID, key string, visibility MediaVisibility) (string, error) {
	if strings.HasPrefix(key, "hongguo:") {
		return s.repo.MediaView.HongGuoSeriesForSource(ctx, libraryID, strings.TrimPrefix(key, "hongguo:"), repository.MediaQueryFilter{AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
	}
	if strings.HasPrefix(key, "metadata:") {
		return strings.TrimPrefix(key, "metadata:"), nil
	}
	ids, err := s.repo.MediaView.LibrarySeriesMetadataIDs(ctx, libraryID, repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if key == compactSeriesKey("series:"+id) || key == compactSeriesKey("metadata:"+id) {
			return id, nil
		}
	}
	return "", nil
}

func seriesCardMetadataID(card SeriesCard) string {
	if card.Rep.CatalogSource == model.CatalogSourceNFO && card.Rep.SeriesID == "" {
		return "nfo-" + card.Rep.LookupCatalogID
	}
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
	if err := s.attachSeriesCardPresentations(ctx, cards, visibility); err != nil {
		return nil, err
	}
	recentAt := make(map[string]time.Time, len(cards))
	for _, row := range rows {
		id := row.MetadataID
		if row.CatalogSource == model.CatalogSourceNFO {
			id = row.CatalogItemID
		}
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

// attachSeriesCardPresentations 仅替换整剧卡片的展示字段，保留文件身份、技术信息和播放目标。
func (s *MediaService) attachSeriesCardPresentations(ctx context.Context, cards []SeriesCard, visibility MediaVisibility) error {
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		if card.Rep.SeriesID != "" {
			ids = append(ids, card.Rep.SeriesID)
		}
	}
	presentations, err := s.repo.MediaView.FindSeriesPresentations(ctx, ids, visibility.IncludeNSFW)
	if err != nil {
		return err
	}
	for i := range cards {
		if view, ok := presentations[cards[i].Rep.SeriesID]; ok {
			doubanRating := view.DoubanRating
			view.Media = cards[i].Rep
			view.DoubanRating = doubanRating
			cards[i].Rep = mediaViewsAsMedia([]model.MediaView{view})[0]
		}
	}
	return nil
}

func (s *MediaService) ListLibrarySeriesEpisodes(ctx context.Context, libraryID, key string, season *int, visibility MediaVisibility) ([]model.MediaView, error) {
	if !visibility.allows(libraryID, false) {
		return []model.MediaView{}, nil
	}
	id, err := s.resolveLibrarySeriesKey(ctx, libraryID, key, visibility)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return []model.MediaView{}, nil
	}
	rows, err := s.repo.MediaView.ListLibrarySeriesViewsForSeason(ctx, libraryID, id, season, repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
	if err != nil {
		return nil, err
	}
	s.attachLibraryMetadataViews(ctx, rows)
	return rows, nil
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
