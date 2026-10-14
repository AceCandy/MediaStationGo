package model

import (
	"encoding/json"
	"strconv"
	"time"
)

const (
	DoubanQualityComplete = "complete"
	DoubanQualityDegraded = "degraded"
	DoubanQualityPartial  = "partial"
)

// DoubanSubject 按豆瓣实际身份保存共享资料，不随本地作品解绑或删除而丢失。
type DoubanSubject struct {
	PermanentBase
	DoubanID   string  `gorm:"size:32;not null;uniqueIndex:uidx_douban_subject,priority:1"`
	EntityKind string  `gorm:"size:16;not null;uniqueIndex:uidx_douban_subject,priority:2"`
	Payload    *string `gorm:"type:jsonb"`
	// Summary 保存榜单摘要，不替代基础详情的质量和获取时间。
	Summary *string `gorm:"type:jsonb"`
	// SupplementPending 是补全队列事实；租约令牌防止迟到执行结算新请求。
	SupplementPending     bool `gorm:"not null;default:false;index"`
	SupplementLeaseUntil  *time.Time
	SupplementToken       string `gorm:"size:36;not null;default:''"`
	SupplementRequestedBy string `gorm:"size:36;not null;default:'';index"`
	SupplementRetryAt     *time.Time
	CreditsFetchedAt      *time.Time
	// RichPayload 保存公开网页补充资料及头像本地引用，不改写接口原始响应。
	RichPayload   *string `gorm:"type:jsonb"`
	RichFetchedAt *time.Time
	// RichNextRetryAt 独立控制网页补充退避，基础详情成功不推迟补充重试。
	RichNextRetryAt *time.Time
	// 评分检查不推进基础详情时间，失败也保留独立退避。
	RatingCheckedAt *time.Time `gorm:"index"`
	RatingRetryAt   *time.Time
	RatingPayload   *string `gorm:"type:jsonb" json:"-"`
	Rating          float32
	Quality         string     `gorm:"size:16;not null;default:''"`
	FetchedAt       *time.Time `gorm:"index"`
	LastAttemptAt   *time.Time
	NextRetryAt     *time.Time `gorm:"index"`
	// PosterAssetID 独立引用本地原图，未绑定媒体的资料也可保存海报。
	PosterCheckedAt *time.Time    `gorm:"index" json:"-"`
	PosterRetryAt   *time.Time    `json:"-"`
	PosterAssetID   *string       `gorm:"size:36;index"`
	PosterSourceURL string        `gorm:"type:text;not null;default:''"`
	PosterAsset     *ArtworkAsset `gorm:"foreignKey:PosterAssetID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"-"`
}

// DoubanQueryCache 保存搜索和榜单响应；查询结果不作为完整条目资料。
type DoubanQueryCache struct {
	Key       string    `gorm:"primaryKey;size:64"`
	Payload   string    `gorm:"type:text;not null"`
	FetchedAt time.Time `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
}

// DoubanRating 统一原始响应评分解析，缺失或无效评分保持未知。
func DoubanRating(payload string) float32 {
	var subject map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &subject) != nil {
		return 0
	}
	for _, key := range []string{"subject", "data"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(subject[key], &nested) == nil && nested != nil {
			subject = nested
			break
		}
	}
	for _, key := range []string{"rate", "rating"} {
		raw := subject[key]
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil && nested != nil {
			raw = nested["value"]
			if len(raw) == 0 {
				raw = nested["score"]
			}
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			value, err := number.Float64()
			if err == nil && value > 0 && value <= 10 {
				return float32(value)
			}
		}
	}
	return 0
}

// DoubanPayloadQuality 区分摘要包装与详情响应，避免将摘要标为完整资料。
func DoubanPayloadQuality(payload []byte, degraded bool) string {
	if degraded {
		return DoubanQualityDegraded
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) == nil {
		if _, ok := raw["subject"]; ok {
			return DoubanQualityPartial
		}
		if _, ok := raw["data"]; ok {
			return DoubanQualityPartial
		}
	}
	return DoubanQualityComplete
}

// DoubanPayloadMatchesIdentity 拒绝将明确属于其他条目的响应关联到当前豆瓣身份。
// 响应未提供身份字段时，使用已验证的请求身份。
func DoubanPayloadMatchesIdentity(payload []byte, id, kind string) bool {
	var subject map[string]json.RawMessage
	if json.Unmarshal(payload, &subject) != nil || subject == nil {
		return false
	}
	if !doubanMapMatchesIdentity(subject, id, kind) {
		return false
	}
	for _, key := range []string{"subject", "data"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(subject[key], &nested) == nil && nested != nil {
			if !doubanMapMatchesIdentity(nested, id, kind) {
				return false
			}
		}
	}
	return true
}

func doubanMapMatchesIdentity(subject map[string]json.RawMessage, id, kind string) bool {
	var actualID string
	if raw := subject["id"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &actualID) != nil {
			var number json.Number
			if json.Unmarshal(raw, &number) != nil {
				return false
			}
			actualID = number.String()
		}
		if actualID != "" && actualID != id {
			actualNumber, actualErr := strconv.ParseUint(actualID, 10, 64)
			expectedNumber, expectedErr := strconv.ParseUint(id, 10, 64)
			if actualErr != nil || expectedErr != nil || actualNumber != expectedNumber {
				return false
			}
		}
	}
	var actualType string
	_ = json.Unmarshal(subject["type"], &actualType)
	if (actualType == "tv" && kind != "series") || (actualType == "movie" && kind != "movie") {
		return false
	}
	var isTV bool
	if raw := subject["is_tv"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &isTV) != nil || isTV != (kind == "series") {
			return false
		}
	}
	return true
}
