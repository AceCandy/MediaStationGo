package model

// ProxyPoolEntry stores one ordered encrypted proxy URL.
type ProxyPoolEntry struct {
	PermanentBase
	URL      string `gorm:"type:text;not null" json:"-"`
	Position int    `gorm:"not null;index" json:"position"`
}
