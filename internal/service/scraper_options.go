package service

import "time"

type scrapeTimings struct {
	CandidateGeneration time.Duration
	ProviderLookup      time.Duration
	MetadataPersist     time.Duration
	Artwork             time.Duration
	TMDbExtendedDetails time.Duration
}

type ScrapeOptions struct {
	RetryNoMatch        bool
	IncludeMatched      bool
	RefreshWeakMatched  bool
	EpisodeArtwork      *bool
	DeferEpisodeDetails bool
	timings             *scrapeTimings
}

func (o ScrapeOptions) episodeArtworkEnabled() bool {
	return o.EpisodeArtwork == nil || *o.EpisodeArtwork
}

func skipEpisodeArtworkOptions(retryNoMatch bool) ScrapeOptions {
	episodeArtwork := false
	return ScrapeOptions{RetryNoMatch: retryNoMatch, EpisodeArtwork: &episodeArtwork}
}
