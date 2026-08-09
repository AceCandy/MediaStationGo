package service

import "strings"

func cloudFileJSONIsEpisodeMetadata(seriesLike bool, parsedSeason, parsedEpisode int, meta *LocalMetadata) bool {
	if !seriesLike || meta == nil {
		return false
	}
	return parsedSeason > 0 ||
		parsedEpisode > 0 ||
		meta.SeasonNum > 0 ||
		meta.EpisodeNum > 0 ||
		strings.TrimSpace(meta.EpisodeTitle) != ""
}

func mergeCloudEpisodeMetadata(dst, episode *LocalMetadata) *LocalMetadata {
	if episode == nil {
		return dst
	}
	if dst == nil {
		dst = &LocalMetadata{}
	}
	mergeCloudEpisodeIdentity(dst, episode)
	mergeCloudEpisodeDisplay(dst, episode)
	mergeCloudEpisodeNumbersAndTaxonomy(dst, episode)
	dst.EpisodeNSFW = episode.NSFW
	dst.HasNFO = dst.HasNFO || episode.HasNFO
	dst.HasArtwork = dst.HasArtwork || episode.HasArtwork
	return dst
}

func mergeCloudEpisodeIdentity(dst, episode *LocalMetadata) {
	showTitle := ""
	if episode.EpisodeTitle != "" && episode.Title != "" && !strings.EqualFold(episode.Title, episode.EpisodeTitle) {
		showTitle = episode.Title
	}
	if showTitle != "" {
		dst.Title = showTitle
	}
	episodeTitle := strings.TrimSpace(episode.EpisodeTitle)
	if episodeTitle == "" && (episode.SeasonNum > 0 || episode.EpisodeNum > 0) {
		episodeTitle = strings.TrimSpace(episode.Title)
	}
	if episodeTitle != "" && !strings.EqualFold(episodeTitle, strings.TrimSpace(dst.Title)) {
		dst.EpisodeTitle = episodeTitle
	}
}

func mergeCloudEpisodeDisplay(dst, episode *LocalMetadata) {
	dst.EpisodeYear = episode.Year
	dst.EpisodeReleaseDate = episode.ReleaseDate
	dst.EpisodeOverview = episode.Overview
	dst.EpisodeRating = episode.Rating
	dst.EpisodeStillURL = firstText(episode.PosterURL, episode.BackdropURL)
}

func mergeCloudEpisodeNumbersAndTaxonomy(dst, episode *LocalMetadata) {
	if episode.SeasonNum > 0 {
		dst.SeasonNum = episode.SeasonNum
	}
	if episode.EpisodeNum > 0 {
		dst.EpisodeNum = episode.EpisodeNum
	}
	dst.EpisodeGenres = episode.Genres
	dst.EpisodeCountries = episode.Countries
	dst.EpisodeLanguages = episode.Languages
}
