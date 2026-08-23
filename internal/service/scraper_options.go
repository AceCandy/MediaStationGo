package service

import "time"

type scrapeTimings struct {
	CandidateGeneration time.Duration
	ProviderLookup      time.Duration
	MetadataPersist     time.Duration
	Artwork             time.Duration
	TMDbExtendedDetails time.Duration
}

type scrapeResult struct {
	Source string
}

type ScrapeOptions struct {
	RetryNoMatch        bool
	IncludeMatched      bool
	RefreshWeakMatched  bool
	EpisodeArtwork      *bool
	DeferEpisodeDetails bool
	timings             *scrapeTimings
	result              *scrapeResult
}

func recordScrapeSource(options ScrapeOptions, source string, err error) error {
	if err == nil && options.result != nil {
		options.result.Source = source
	}
	return err
}

func (o ScrapeOptions) episodeArtworkEnabled() bool {
	return o.EpisodeArtwork == nil || *o.EpisodeArtwork
}

func skipEpisodeArtworkOptions(retryNoMatch bool) ScrapeOptions {
	episodeArtwork := false
	return ScrapeOptions{RetryNoMatch: retryNoMatch, EpisodeArtwork: &episodeArtwork}
}
