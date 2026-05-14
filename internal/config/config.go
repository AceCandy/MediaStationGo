// Package config loads layered configuration from defaults, config files and
// environment variables, mirroring the conventions used by nowen-video.
//
// Priority (low -> high):
//  1. Built-in defaults
//  2. config.yaml in the working directory (nested format)
//  3. config/*.yaml shard files (per-module)
//  4. Environment variables prefixed with MEDIASTATION_
//
// Environment variable example:
//
//	MEDIASTATION_APP_PORT=8080
//	MEDIASTATION_SECRETS_JWT_SECRET=please-change-me
//	MEDIASTATION_DATABASE_DB_PATH=/data/mediastation.db
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// EnvPrefix is the prefix used for all env-var-driven overrides.
const EnvPrefix = "MEDIASTATION"

// Config is the root config aggregate.
type Config struct {
	App      AppConfig      `mapstructure:"app"`
	Database DatabaseConfig `mapstructure:"database"`
	Secrets  SecretsConfig  `mapstructure:"secrets"`
	Logging  LoggingConfig  `mapstructure:"logging"`
	Cache    CacheConfig    `mapstructure:"cache"`
	Media    MediaConfig    `mapstructure:"media"`
	AI       AIConfig       `mapstructure:"ai"`
}

// AppConfig holds runtime app parameters.
type AppConfig struct {
	Port        int      `mapstructure:"port"`
	Debug       bool     `mapstructure:"debug"`
	Env         string   `mapstructure:"env"`
	DataDir     string   `mapstructure:"data_dir"`
	WebDir      string   `mapstructure:"web_dir"`
	FFmpegPath  string   `mapstructure:"ffmpeg_path"`
	FFprobePath string   `mapstructure:"ffprobe_path"`
	VAAPIDevice string   `mapstructure:"vaapi_device"`
	CORSOrigins []string `mapstructure:"cors_origins"`
	ServerURL   string   `mapstructure:"server_url"`
}

// DatabaseConfig configures GORM + SQLite.
type DatabaseConfig struct {
	DBPath       string `mapstructure:"db_path"`
	WALMode      bool   `mapstructure:"wal_mode"`
	BusyTimeout  int    `mapstructure:"busy_timeout"`
	CacheSize    int    `mapstructure:"cache_size"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// SecretsConfig holds JWT / 3rd-party API keys (do NOT commit values).
type SecretsConfig struct {
	JWTSecret      string `mapstructure:"jwt_secret"`
	TMDbAPIKey     string `mapstructure:"tmdb_api_key"`
	TMDbAPIProxy   string `mapstructure:"tmdb_api_proxy"`
	TMDbImageProxy string `mapstructure:"tmdb_image_proxy"`
	BangumiToken   string `mapstructure:"bangumi_access_token"`
	TheTVDBAPIKey  string `mapstructure:"thetvdb_api_key"`
	FanartAPIKey   string `mapstructure:"fanart_tv_api_key"`
	DoubanCookie   string `mapstructure:"douban_cookie"`
}

// LoggingConfig configures Zap.
type LoggingConfig struct {
	Level          string `mapstructure:"level"`
	Format         string `mapstructure:"format"`
	OutputPath     string `mapstructure:"output_path"`
	EnableRotation bool   `mapstructure:"enable_rotation"`
	MaxSizeMB      int    `mapstructure:"max_size_mb"`
	MaxAgeDays     int    `mapstructure:"max_age_days"`
	MaxBackups     int    `mapstructure:"max_backups"`
}

// CacheConfig controls the on-disk transcode/scrape cache.
type CacheConfig struct {
	CacheDir           string `mapstructure:"cache_dir"`
	MaxDiskUsageMB     int    `mapstructure:"max_disk_usage_mb"`
	TTLHours           int    `mapstructure:"ttl_hours"`
	AutoCleanup        bool   `mapstructure:"auto_cleanup"`
	CleanupIntervalMin int    `mapstructure:"cleanup_interval_min"`
}

// MediaConfig holds default library locations (used by the bootstrap library).
type MediaConfig struct {
	MoviesDir string `mapstructure:"movies_dir"`
	TVDir     string `mapstructure:"tv_dir"`
	AnimeDir  string `mapstructure:"anime_dir"`
}

// AIConfig configures the optional LLM provider.
type AIConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Provider      string `mapstructure:"provider"`
	APIBase       string `mapstructure:"api_base"`
	APIKey        string `mapstructure:"api_key"`
	Model         string `mapstructure:"model"`
	Timeout       int    `mapstructure:"timeout"`
	MaxConcurrent int    `mapstructure:"max_concurrent"`
}

// Load reads configuration from defaults / files / environment.
//
// It always returns a usable Config, even if no files are present.
func Load() (*Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !asConfigFileNotFound(err, &notFound) {
			return nil, fmt.Errorf("read main config: %w", err)
		}
	}

	// Merge sharded files under ./config/*.yaml.
	if entries, err := os.ReadDir("config"); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			s := viper.New()
			s.SetConfigFile(filepath.Join("config", e.Name()))
			if err := s.ReadInConfig(); err == nil {
				_ = v.MergeConfigMap(s.AllSettings())
			}
		}
	}

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.port", 8080)
	v.SetDefault("app.debug", false)
	v.SetDefault("app.env", "production")
	v.SetDefault("app.data_dir", "./data")
	v.SetDefault("app.web_dir", "./web/dist")
	v.SetDefault("app.ffmpeg_path", "ffmpeg")
	v.SetDefault("app.ffprobe_path", "ffprobe")
	v.SetDefault("app.vaapi_device", "/dev/dri/renderD128")
	v.SetDefault("app.cors_origins", []string{})
	v.SetDefault("app.server_url", "")

	v.SetDefault("database.db_path", "./data/mediastation.db")
	v.SetDefault("database.wal_mode", true)
	v.SetDefault("database.busy_timeout", 5000)
	v.SetDefault("database.cache_size", -20000)
	v.SetDefault("database.max_open_conns", 1)
	v.SetDefault("database.max_idle_conns", 1)

	v.SetDefault("secrets.jwt_secret", "")

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "console")
	v.SetDefault("logging.max_size_mb", 100)
	v.SetDefault("logging.max_age_days", 30)
	v.SetDefault("logging.max_backups", 10)

	v.SetDefault("cache.cache_dir", "./cache")
	v.SetDefault("cache.cleanup_interval_min", 60)

	v.SetDefault("ai.enabled", false)
	v.SetDefault("ai.provider", "openai")
	v.SetDefault("ai.api_base", "https://api.openai.com/v1")
	v.SetDefault("ai.model", "gpt-4o-mini")
	v.SetDefault("ai.timeout", 30)
	v.SetDefault("ai.max_concurrent", 3)
}

// normalize fills derived defaults and self-heals empty critical fields.
func (c *Config) normalize() error {
	if c.App.DataDir == "" {
		c.App.DataDir = "./data"
	}
	if c.Database.DBPath == "" {
		c.Database.DBPath = filepath.Join(c.App.DataDir, "mediastation.db")
	}
	if c.Cache.CacheDir == "" {
		c.Cache.CacheDir = filepath.Join(c.App.DataDir, "cache")
	}
	if c.Secrets.JWTSecret == "" {
		// Persist an auto-generated secret to keep sessions stable across
		// restarts even when the operator forgot to configure one.
		path := filepath.Join(c.App.DataDir, ".jwt_secret")
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			c.Secrets.JWTSecret = strings.TrimSpace(string(data))
		} else {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return fmt.Errorf("generate jwt secret: %w", err)
			}
			c.Secrets.JWTSecret = hex.EncodeToString(buf)
			_ = os.MkdirAll(c.App.DataDir, 0o755)
			_ = os.WriteFile(path, []byte(c.Secrets.JWTSecret), 0o600)
		}
	}
	return nil
}

// asConfigFileNotFound is a small helper around errors.As that avoids importing
// errors in this short file.
func asConfigFileNotFound(err error, target *viper.ConfigFileNotFoundError) bool {
	if err == nil {
		return false
	}
	if v, ok := err.(viper.ConfigFileNotFoundError); ok {
		*target = v
		return true
	}
	return false
}
