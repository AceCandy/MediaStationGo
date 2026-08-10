package config

// Config 是根配置聚合。
type Config struct {
	App       AppConfig       `mapstructure:"app"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Secrets   SecretsConfig   `mapstructure:"secrets"`
	Logging   LoggingConfig   `mapstructure:"logging"`
	Cache     CacheConfig     `mapstructure:"cache"`
	Search    SearchConfig    `mapstructure:"search"`
	Media     MediaConfig     `mapstructure:"media"`
	AI        AIConfig        `mapstructure:"ai"`
	ApiConfig ApiConfigConfig `mapstructure:"api_config"`
	Organizer OrganizerConfig `mapstructure:"organizer"`
}

// ApiConfigConfig API 配置相关设置。
type ApiConfigConfig struct {
	// AutoEncrypt 是否自动加密敏感字段
	AutoEncrypt bool `mapstructure:"auto_encrypt"`
	// DefaultTimeout 默认请求超时（秒）
	DefaultTimeout int `mapstructure:"default_timeout"`
}

// AppConfig 保存运行时应用参数。
type AppConfig struct {
	Port        int    `mapstructure:"port"`
	Debug       bool   `mapstructure:"debug"`
	Env         string `mapstructure:"env"`
	DataDir     string `mapstructure:"data_dir"`
	WebDir      string `mapstructure:"web_dir"`
	FFprobePath string `mapstructure:"ffprobe_path"`
	// FFprobeMaxConcurrent limits concurrent ffprobe metadata probes.
	// NAS devices can become unresponsive when a scan starts many probe
	// processes at once, so the default is deliberately conservative.
	FFprobeMaxConcurrent int      `mapstructure:"ffprobe_max_concurrent"`
	MaxCPUThreads        int      `mapstructure:"max_cpu_threads"`
	CORSOrigins          []string `mapstructure:"cors_origins"`
	ServerURL            string   `mapstructure:"server_url"`
}

// DatabaseConfig 配置 PostgreSQL 连接。
type DatabaseConfig struct {
	Type         string `mapstructure:"type"`
	DSN          string `mapstructure:"dsn"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// SecretsConfig 保存 JWT / 第三方 API 密钥（不要提交值）。
type SecretsConfig struct {
	JWTSecret      string `mapstructure:"jwt_secret"`
	TMDbAPIKey     string `mapstructure:"tmdb_api_key"`
	TMDbAPIProxy   string `mapstructure:"tmdb_api_proxy"`
	TMDbImageProxy string `mapstructure:"tmdb_image_proxy"`
	BangumiToken   string `mapstructure:"bangumi_access_token"`
	TheTVDBAPIKey  string `mapstructure:"thetvdb_api_key"`
	FanartAPIKey   string `mapstructure:"fanart_tv_api_key"`
	DoubanCookie   string `mapstructure:"douban_cookie"`
	// 用于加密的密钥，如果为空则使用 JWTSecret
	EncryptionKey string `mapstructure:"encryption_key"`
}

// LoggingConfig 配置 Zap。
type LoggingConfig struct {
	Level          string `mapstructure:"level"`
	Format         string `mapstructure:"format"`
	OutputPath     string `mapstructure:"output_path"`
	EnableRotation bool   `mapstructure:"enable_rotation"`
	MaxSizeMB      int    `mapstructure:"max_size_mb"`
	MaxAgeDays     int    `mapstructure:"max_age_days"`
	MaxBackups     int    `mapstructure:"max_backups"`
}

// CacheConfig 控制刮削与运行时缓存。
type CacheConfig struct {
	CacheDir        string `mapstructure:"cache_dir"`
	RedisURL        string `mapstructure:"redis_url"`
	RedisPrefix     string `mapstructure:"redis_prefix"`
	MediaTTLSeconds int    `mapstructure:"media_ttl_seconds"`
}

type SearchConfig struct {
	Backend       string `mapstructure:"backend"`
	OpenSearchURL string `mapstructure:"opensearch_url"`
	Index         string `mapstructure:"index"`
	Username      string `mapstructure:"username"`
	Password      string `mapstructure:"password"`
}

// MediaConfig 保存默认库位置（用于引导库）。
type MediaConfig struct {
	MoviesDir string `mapstructure:"movies_dir"`
	TVDir     string `mapstructure:"tv_dir"`
	AnimeDir  string `mapstructure:"anime_dir"`
}

// AIConfig 配置可选的 LLM 提供者。
type AIConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Provider      string `mapstructure:"provider"`
	APIBase       string `mapstructure:"api_base"`
	APIKey        string `mapstructure:"api_key"`
	Model         string `mapstructure:"model"`
	Timeout       int    `mapstructure:"timeout"`
	MaxConcurrent int    `mapstructure:"max_concurrent"`
}

// OrganizerConfig 配置媒体文件智能分类整理。
type OrganizerConfig struct {
	SmartClassify bool              `mapstructure:"smart_classify"`
	Categories    map[string]string `mapstructure:"categories"`
}
