package model

import "time"

// HuangGuoAIDiscovery is a source summary, never authoritative playable metadata.
type HuangGuoAIDiscovery struct {
	SourceID        string     `gorm:"primaryKey;size:32" json:"source_id"`
	SourceCategory  string     `gorm:"size:32;index" json:"source_category"`
	Title           string     `gorm:"type:text" json:"title"`
	Overview        string     `gorm:"type:text" json:"overview"`
	Tags            string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	CoverURL        string     `gorm:"type:text" json:"-"`
	Rating          float32    `json:"rating"`
	EpisodeCount    int        `json:"episode_count"`
	TotalEpisodes   *int       `json:"total_episodes"`
	Completed       *bool      `json:"completed"`
	SourceCreatedAt string     `gorm:"type:text" json:"source_created_at"`
	SourceUpdatedAt *time.Time `json:"source_updated_at,omitempty"`
	CreatedAt       time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (HuangGuoAIDiscovery) TableName() string { return "huangguoai_discoveries" }

type HuangGuoAICategoryMembership struct {
	SourceID  string    `gorm:"primaryKey;size:32" json:"source_id"`
	Category  string    `gorm:"primaryKey;size:32;index" json:"category"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HuangGuoAICategoryMembership) TableName() string { return "huangguoai_category_memberships" }

// HuangGuoAIWork separates internal UUID, upstream identity and fixed category type.
type HuangGuoAIWork struct {
	PermanentBase
	SourceID              string     `gorm:"size:32;not null;uniqueIndex" json:"source_id"`
	SourceCategory        string     `gorm:"size:32;not null;index" json:"source_category"`
	Kind                  string     `gorm:"size:16;not null;index;check:chk_hga_kind,kind IN ('movie','series')" json:"kind"`
	ProjectionError       string     `gorm:"size:48;not null;default:''" json:"projection_error,omitempty"`
	Title                 string     `gorm:"type:text;not null" json:"title"`
	Overview              string     `gorm:"type:text" json:"overview"`
	Tags                  string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	Rating                float32    `json:"rating"`
	EpisodeCount          int        `json:"episode_count"`
	ConfirmedEpisodeCount int        `json:"confirmed_episode_count"`
	TotalEpisodes         *int       `json:"total_episodes"`
	Completed             *bool      `json:"completed"`
	SourceCreatedAt       string     `gorm:"type:text" json:"source_created_at"`
	SourceUpdatedAt       *time.Time `json:"source_updated_at,omitempty"`
	RefreshedAt           time.Time  `gorm:"not null;index" json:"refreshed_at"`
	LatestMediaAddedAt    *time.Time `gorm:"->" json:"latest_media_added_at,omitempty"`
	LibraryIDs            *string    `gorm:"type:jsonb;->" json:"-"`
}

func (HuangGuoAIWork) TableName() string { return "huangguoai_works" }

type HuangGuoAIEpisode struct {
	PermanentBase
	WorkID   string         `gorm:"size:36;not null;uniqueIndex:uidx_hga_episode,priority:1" json:"work_id"`
	Number   int            `gorm:"not null;uniqueIndex:uidx_hga_episode,priority:2;check:chk_hga_episode_number,number > 0" json:"number"`
	PagePath string         `gorm:"type:text;not null" json:"-"`
	Work     HuangGuoAIWork `gorm:"foreignKey:WorkID;constraint:OnDelete:RESTRICT" json:"-"`
}

func (HuangGuoAIEpisode) TableName() string { return "huangguoai_episodes" }

type HuangGuoAISnapshot struct {
	WorkID    string         `gorm:"primaryKey;size:36" json:"-"`
	Payload   string         `gorm:"type:jsonb;not null" json:"-"`
	FetchedAt time.Time      `json:"-"`
	Work      HuangGuoAIWork `gorm:"foreignKey:WorkID;constraint:OnDelete:CASCADE" json:"-"`
}

func (HuangGuoAISnapshot) TableName() string { return "huangguoai_snapshots" }

type HuangGuoAIArtwork struct {
	PermanentBase
	SourceID      string     `gorm:"size:32;not null;uniqueIndex" json:"-"`
	WorkID        *string    `gorm:"size:36;uniqueIndex" json:"work_id,omitempty"`
	SourceURL     string     `gorm:"type:text;not null" json:"-"`
	LocalKey      string     `gorm:"type:text" json:"-"`
	Attempts      int        `gorm:"not null;default:0" json:"-"`
	NextAttemptAt *time.Time `gorm:"index" json:"-"`
}

func (HuangGuoAIArtwork) TableName() string { return "huangguoai_artworks" }

type HuangGuoAISyncState struct {
	Category  string    `gorm:"primaryKey;size:32" json:"category"`
	Ordering  string    `gorm:"primaryKey;size:16" json:"ordering"`
	NextPage  int       `gorm:"not null;default:1" json:"next_page"`
	Round     int64     `gorm:"not null;default:0" json:"round"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HuangGuoAISyncState) TableName() string { return "huangguoai_sync_states" }

type HuangGuoAISyncFailure struct {
	Stage     string    `gorm:"primaryKey;size:24" json:"stage"`
	SourceKey string    `gorm:"primaryKey;size:64" json:"source_key"`
	Attempts  int       `gorm:"not null;default:0" json:"attempts"`
	RetryAt   time.Time `gorm:"not null;index" json:"retry_at"`
	ErrorCode string    `gorm:"size:48" json:"error_code"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HuangGuoAISyncFailure) TableName() string { return "huangguoai_sync_failures" }

type HuangGuoAIMediaBinding struct {
	MediaID   string            `gorm:"primaryKey;size:36" json:"media_id"`
	WorkID    string            `gorm:"size:36;not null;index" json:"work_id"`
	EpisodeID string            `gorm:"size:36;not null;index" json:"episode_id"`
	Media     Media             `gorm:"foreignKey:MediaID;constraint:OnDelete:CASCADE" json:"-"`
	Work      HuangGuoAIWork    `gorm:"foreignKey:WorkID;constraint:OnDelete:RESTRICT" json:"-"`
	Episode   HuangGuoAIEpisode `gorm:"foreignKey:EpisodeID;constraint:OnDelete:RESTRICT" json:"-"`
}

func (HuangGuoAIMediaBinding) TableName() string { return "huangguoai_media_bindings" }

// User-owned state survives source metadata and physical-file removal.
type HuangGuoAIFavorite struct {
	UserID    string    `gorm:"primaryKey;size:36" json:"user_id"`
	SourceID  string    `gorm:"primaryKey;size:32;index" json:"source_id"`
	Favorite  bool      `gorm:"not null;default:false" json:"favorite"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HuangGuoAIFavorite) TableName() string { return "huangguoai_favorites" }

type HuangGuoAIUserState struct {
	UserID           string     `gorm:"primaryKey;size:36" json:"user_id"`
	SourceID         string     `gorm:"primaryKey;size:32" json:"source_id"`
	EpisodeNumber    int        `gorm:"primaryKey;check:chk_hga_state_episode,episode_number > 0" json:"episode_number"`
	MediaID          string     `gorm:"size:128" json:"media_id"`
	PositionMs       int64      `gorm:"not null;default:0" json:"position_ms"`
	DurationMs       int64      `gorm:"not null;default:0" json:"duration_ms"`
	Completed        bool       `gorm:"not null;default:false" json:"completed"`
	ResumePositionMs *int64     `json:"-"`
	WatchedAt        *time.Time `gorm:"index" json:"watched_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (HuangGuoAIUserState) TableName() string { return "huangguoai_user_states" }

type HuangGuoAIPlaybackEvent struct {
	PermanentBase
	UserID        string    `gorm:"size:36;not null;uniqueIndex:uidx_hga_event,priority:1;index" json:"user_id"`
	SessionID     string    `gorm:"size:128;not null;uniqueIndex:uidx_hga_event,priority:2" json:"session_id"`
	SourceID      string    `gorm:"size:32;not null;uniqueIndex:uidx_hga_event,priority:3" json:"source_id"`
	EpisodeNumber int       `gorm:"not null;uniqueIndex:uidx_hga_event,priority:4" json:"episode_number"`
	MediaID       string    `gorm:"size:128;not null" json:"media_id"`
	LibraryID     string    `gorm:"size:36;not null;index" json:"library_id"`
	PlayedAt      time.Time `gorm:"not null;index" json:"played_at"`
}

func (HuangGuoAIPlaybackEvent) TableName() string { return "huangguoai_playback_events" }

type HuangGuoAIDownloadWork struct {
	SourceID  string    `gorm:"primaryKey;size:32" json:"source_id"`
	Title     string    `gorm:"type:text;not null" json:"title"`
	Root      string    `gorm:"type:text;not null" json:"-"`
	Directory string    `gorm:"type:text;not null" json:"directory"`
	CreatedAt time.Time `json:"created_at"`
}

func (HuangGuoAIDownloadWork) TableName() string { return "huangguoai_download_works" }

type HuangGuoAIDownload struct {
	PermanentBase
	SourceID     string     `gorm:"size:32;not null;uniqueIndex:uidx_hga_download_episode,priority:1" json:"source_id"`
	Episode      int        `gorm:"not null;uniqueIndex:uidx_hga_download_episode,priority:2" json:"episode"`
	Title        string     `gorm:"type:text" json:"title"`
	Root         string     `gorm:"type:text" json:"-"`
	RelativePath string     `gorm:"type:text" json:"relative_path"`
	Status       string     `gorm:"size:24;not null;index:idx_hga_download_due,priority:1" json:"status"`
	Bytes        int64      `json:"bytes"`
	TotalBytes   int64      `json:"total_bytes"`
	Attempts     int        `json:"attempts"`
	Error        string     `gorm:"type:text" json:"error"`
	LeaseToken   string     `gorm:"size:36" json:"-"`
	LeaseUntil   *time.Time `gorm:"index:idx_hga_download_due,priority:2" json:"-"`
	RawSize      int64      `gorm:"not null;default:0" json:"-"`
	StagingPath  string     `gorm:"type:text" json:"-"`
	SHA256       string     `gorm:"size:64" json:"-"`
	VerifiedSize int64      `json:"-"`
	Duration     float64    `json:"-"`
}

func (HuangGuoAIDownload) TableName() string { return "huangguoai_downloads" }

type HuangGuoAIRankEntry struct {
	RankKey   string    `gorm:"primaryKey;size:24" json:"rank_key"`
	SourceID  string    `gorm:"primaryKey;size:32;index" json:"source_id"`
	Position  int       `gorm:"not null;index:idx_hga_rank_position" json:"position"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HuangGuoAIRankEntry) TableName() string { return "huangguoai_rank_entries" }

func HuangGuoAIModels() []interface{} {
	return []interface{}{
		&HuangGuoAIDiscovery{}, &HuangGuoAICategoryMembership{}, &HuangGuoAIWork{}, &HuangGuoAIEpisode{}, &HuangGuoAISnapshot{}, &HuangGuoAIArtwork{}, &HuangGuoAISyncState{}, &HuangGuoAISyncFailure{}, &HuangGuoAIMediaBinding{}, &HuangGuoAIFavorite{}, &HuangGuoAIUserState{}, &HuangGuoAIPlaybackEvent{}, &HuangGuoAIDownloadWork{}, &HuangGuoAIDownload{}, &HuangGuoAIRankEntry{},
	}
}
