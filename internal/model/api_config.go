package model

// APIConfig 保存第三方来源配置；密钥加密存储且不参与 JSON 序列化。
type APIConfig struct {
	Base
	Provider         string `gorm:"uniqueIndex;size:32;not null" json:"provider"`
	APIKey           string `gorm:"type:text" json:"-"` // ciphertext (never serialised)
	BaseURL          string `gorm:"size:512" json:"base_url,omitempty"`
	Model            string `gorm:"size:128" json:"model,omitempty"`
	Extra            string `gorm:"type:text" json:"extra,omitempty"` // free-form JSON
	Enabled          bool   `gorm:"default:true" json:"enabled"`
	ImageDirect      bool   `gorm:"default:false" json:"image_direct"`
	UseProxyPool     bool   `gorm:"default:false" json:"use_proxy_pool"`
	ProxyPoolType    string `gorm:"size:16;default:normal" json:"proxy_pool_type"`
	ResinProxyURL    string `gorm:"size:512" json:"resin_proxy_url,omitempty"`
	ResinProxyToken  string `gorm:"type:text" json:"-"`
	ResinAccount     string `gorm:"size:128" json:"resin_account,omitempty"`
	WebSearchEnabled bool   `gorm:"default:false" json:"web_search_enabled"`
	Description      string `gorm:"size:255" json:"description,omitempty"`
}
