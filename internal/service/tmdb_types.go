package service

// Match describes a successful metadata match. The same struct is reused
// across providers; provider-specific IDs sit side-by-side so the scraper
// orchestrator can write them all into a single update.
type Match struct {
	Source       string  `json:"-"`
	TMDbID       int     `json:"tmdb_id"`
	BangumiID    int     `json:"bangumi_id"`
	DoubanID     string  `json:"douban_id,omitempty"`
	TheTVDBID    string  `json:"thetvdb_id,omitempty"`
	IMDbID       string  `json:"imdb_id,omitempty"`
	MediaType    string  `json:"media_type,omitempty"`
	Title        string  `json:"title"`
	OriginalName string  `json:"original_name,omitempty"`
	Overview     string  `json:"overview"`
	PosterURL    string  `json:"poster_url"`
	BackdropURL  string  `json:"backdrop_url"`
	Year         int     `json:"year"`
	ReleaseDate  string  `json:"release_date,omitempty"`
	Rating       float32 `json:"rating"`
	// RuntimeSec 是电影资料源声明的整部影片时长；剧集不把它当作全剧时长。
	RuntimeSec    int      `json:"-"`
	Languages     []string `json:"languages,omitempty"`
	Countries     []string `json:"countries,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	Aliases       []string `json:"aliases,omitempty"`
	NSFW          bool     `json:"nsfw,omitempty"`
	SearchKeyword string   `json:"-"`
	// TMDbDetailsLoaded 表示匹配已包含语言、国家和类型等完整详情字段。
	TMDbDetailsLoaded bool `json:"-"`
	// AllowIdentifierMerge 仅用于 provider 明确 crosswalk 或用户确认的匹配。
	AllowIdentifierMerge bool                `json:"-"`
	Credits              []PersonCredit      `json:"-"`
	LoadedCreditTypes    []string            `json:"-"`
	RawJSON              []byte              `json:"-"`
	CatalogPosterURL     string              `json:"-"`
	CatalogBackdropURL   string              `json:"-"`
	Seasons              []TMDbSeasonSummary `json:"-"`
}

// PersonCredit 是 provider-neutral 的演职员快照。
type PersonCredit struct {
	Provider     string
	ExternalID   string
	Name         string
	Overview     string
	ProfileURL   string
	Type         string
	OriginalRole string
	SortOrder    int
}

// TMDbEpisodeDetails holds per-episode metadata from /tv/{id}/season/{season}/episode/{episode}.
type TMDbEpisodeDetails struct {
	ID                int
	Name              string
	Overview          string
	StillURL          string
	CatalogStillURL   string
	AirDate           string
	AirYear           int
	Rating            float32
	Runtime           int
	ExternalIDs       TMDbExternalIDs
	Credits           []PersonCredit
	LoadedCreditTypes []string
	RawJSON           []byte
}

// TMDbSeasonSummary 是 Series 详情返回的权威季清单项。
type TMDbSeasonSummary struct {
	ID           int
	SeasonNumber int
	Name         string
	Overview     string
	AirDate      string
	PosterPath   string
}

// TMDbEpisodeSummary 是 Season 详情返回的权威集清单项。
type TMDbEpisodeSummary struct {
	ID            int
	EpisodeNumber int
	Name          string
	Overview      string
	AirDate       string
	Rating        float32
	Runtime       int
	StillPath     string
}

// TMDbSeasonDetails 保存一季自身详情及其完整 Episode 清单。
type TMDbSeasonDetails struct {
	ID                int
	SeasonNumber      int
	Name              string
	Overview          string
	AirDate           string
	Rating            float32
	PosterURL         string
	ExternalIDs       TMDbExternalIDs
	Credits           []PersonCredit
	LoadedCreditTypes []string
	Episodes          []TMDbEpisodeSummary
	RawJSON           []byte
}

// TMDbExternalIDs 是 TMDb 详情中可稳定保存的外部标识。
type TMDbExternalIDs struct {
	IMDbID string
	TVDBID int
}
