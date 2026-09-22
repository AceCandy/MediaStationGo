// Package model 定义刷新令牌数据模型。
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RefreshToken 用于双令牌认证机制中的刷新令牌。
// 存储时使用 SHA256 哈希，原始令牌不存储。
type RefreshToken struct {
	ID     string `gorm:"primaryKey;size:36" json:"id"`
	UserID string `gorm:"index;size:36;not null" json:"user_id"`
	// TokenVersion 保留签发时的账号版本，拒绝并发改密期间生成的旧会话。
	TokenVersion int64     `gorm:"not null;default:0" json:"-"`
	TokenHash    string    `gorm:"uniqueIndex;size:128;not null" json:"-"`
	ExpiresAt    time.Time `gorm:"index" json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	Revoked      bool      `gorm:"default:false" json:"revoked"`
}

// BeforeCreate 生成 UUID。
func (t *RefreshToken) BeforeCreate(_ *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	return nil
}

// IsExpired 检查刷新令牌是否已过期。
func (t *RefreshToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// IsValid 检查刷新令牌是否有效（未撤销且未过期）。
func (t *RefreshToken) IsValid() bool {
	return !t.Revoked && !t.IsExpired()
}
