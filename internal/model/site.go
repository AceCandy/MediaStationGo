// Package model — PT 站点配置数据模型。
package model

import (
	"time"
)

// Site PT 站点配置。
type Site struct {
	Base
	Name       string `gorm:"size:128;not null" json:"name"`
	Type       string `gorm:"size:32;not null" json:"type"` // nexusphp / gazelle / unit3d / mteam / yemapt / discuz / custom_rss
	URL        string `gorm:"size:512;not null" json:"url"`
	AuthType   string `gorm:"size:32;not null" json:"auth_type"` // cookie / api_key / auth_header
	Cookie     string `gorm:"type:text" json:"-"`                // AES 加密
	APIKey     string `gorm:"type:text" json:"-"`                // AES 加密
	AuthHeader string `gorm:"type:text" json:"-"`                // AES 加密

	// ── 高级设置 ──────────────────────────────────────────────────────────
	UserAgent        string `gorm:"size:500" json:"user_agent"`             // 自定义 User-Agent
	Timeout          int    `gorm:"default:15" json:"timeout"`              // 请求超时(秒), 0=不限制
	Priority         int    `gorm:"default:50" json:"priority"`             // 优先级, 越小越优先
	UseProxy         bool   `gorm:"default:false" json:"use_proxy"`         // 是否使用代理
	RateLimit        bool   `gorm:"default:false" json:"rate_limit"`        // 是否限制访问频率
	BrowserEmulation bool   `gorm:"default:false" json:"browser_emulation"` // 浏览器仿真(防爬)

	// ── 状态 ──────────────────────────────────────────────────────────────
	LoginStatus string `gorm:"size:20;default:unknown" json:"login_status"` // unknown / ok / fail

	Enabled     bool       `gorm:"default:true" json:"enabled"`
	IsDefault   bool       `gorm:"default:false" json:"is_default"`
	Extra       string     `gorm:"type:text" json:"-"` // JSON 扩展配置, AES 加密
	LastError   string     `gorm:"size:1024" json:"last_error"`
	LastCheckAt *time.Time `json:"last_check_at"`
}

// SiteType 返回支持的站点类型列表。
func SiteTypes() []string {
	return []string{"nexusphp", "gazelle", "unit3d", "mteam", "yemapt", "discuz", "custom_rss"}
}

// AuthTypes 返回支持的认证方式列表。
func AuthTypes() []string {
	return []string{"cookie", "api_key", "auth_header"}
}

// DefaultUserAgent 是默认浏览器 User-Agent（用于 HTTP 请求头）。
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

// SiteTypes 返回支持的站点类型列表（用于前端下拉）。
// 注意：此函数供 API 返回类型列表使用，不在此处添加新类型。
