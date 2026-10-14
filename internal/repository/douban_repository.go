package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DoubanRepository 管理共享豆瓣资料及持久查询缓存。
type DoubanRepository struct{ db *gorm.DB }

// DoubanSubjectSummary 是资料中心的分页投影，不携带完整原始响应。
type DoubanSubjectSummary struct {
	ID             string     `json:"id"`
	DoubanID       string     `json:"douban_id"`
	EntityKind     string     `json:"entity_kind"`
	Title          string     `json:"title"`
	Year           string     `json:"year"`
	Rating         float32    `json:"rating"`
	Quality        string     `json:"quality"`
	HasPayload     bool       `json:"has_payload"`
	RefreshPending bool       `json:"refresh_pending"`
	SourceError    bool       `json:"source_error,omitempty"`
	PosterURL      string     `gorm:"-" json:"poster_url"`
	FetchedAt      *time.Time `json:"fetched_at"`
	RichFetchedAt  *time.Time `json:"rich_fetched_at"`
	LastAttemptAt  *time.Time `json:"last_attempt_at"`
	NextRetryAt    *time.Time `json:"next_retry_at"`
}

// ListSubjects 对共享资料直接筛选分页，失败占位记录同样可见。
func (r *DoubanRepository) ListSubjects(ctx context.Context, keyword, kind string, page, pageSize int, genres ...string) ([]DoubanSubjectSummary, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.DoubanSubject{}).Where("entity_kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries})
	if kind != "" {
		q = q.Where("entity_kind = ?", kind)
	}
	if keyword != "" {
		q = q.Where("douban_id = ? OR strpos(lower(list_title), lower(?)) > 0", keyword, keyword)
	}
	if len(genres) > 0 && genres[0] != "" {
		q = q.Where("list_genres @> ARRAY[?]::text[]", genres[0])
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]DoubanSubjectSummary, 0)
	err := q.Select("id, douban_id, entity_kind, list_title AS title, list_year AS year, rating, quality, (payload IS NOT NULL OR summary IS NOT NULL) AS has_payload, fetched_at, rich_fetched_at, last_attempt_at, next_retry_at, (supplement_pending AND (supplement_retry_at IS NULL OR supplement_retry_at <= NOW()) AND (next_retry_at IS NULL OR next_retry_at <= NOW())) AS refresh_pending, (supplement_retry_at IS NOT NULL OR next_retry_at IS NOT NULL) AS source_error").
		Order("fetched_at DESC NULLS LAST, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	return rows, total, err
}

func (r *DoubanRepository) FindSubjectByID(ctx context.Context, id string) (*model.DoubanSubject, error) {
	var subject model.DoubanSubject
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&subject).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &subject, err
}

// SetPoster 仅在资料与图片引用均未变化时关联新图，避免迟到下载覆盖新资料。
func (r *DoubanRepository) SetPoster(ctx context.Context, observed *model.DoubanSubject, assetID, sourceURL string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.DoubanSubject{}).
		Where("id = ? AND payload IS NOT DISTINCT FROM ?::jsonb AND summary IS NOT DISTINCT FROM ?::jsonb AND poster_asset_id IS NOT DISTINCT FROM ? AND poster_source_url = ?",
			observed.ID, observed.Payload, observed.Summary, observed.PosterAssetID, observed.PosterSourceURL).
		UpdateColumns(map[string]any{"poster_asset_id": assetID, "poster_source_url": sourceURL})
	return result.RowsAffected > 0, result.Error
}

func (r *DoubanRepository) Find(ctx context.Context, id, kind string) (*model.DoubanSubject, error) {
	var subject model.DoubanSubject
	q := r.db.WithContext(ctx).Where("douban_id = ?", id)
	if kind != "" {
		q = q.Where("entity_kind = ?", kind)
	}
	err := q.Order("fetched_at DESC NULLS LAST").First(&subject).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &subject, err
}

// SetRichPayload 只更新观察到的资料版本，头像下载和网页获取不得覆盖并发刷新。
func (r *DoubanRepository) SetRichPayload(ctx context.Context, observed *model.DoubanSubject, payload []byte) (bool, error) {
	if !json.Valid(payload) {
		return false, errors.New("invalid douban rich payload")
	}
	var freshness struct {
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.Unmarshal(payload, &freshness); err != nil {
		return false, err
	}
	// PostgreSQL 时间戳精度为微秒，头像沿用同次网页时间时不能误判为新抓取。
	freshness.FetchedAt = freshness.FetchedAt.Truncate(time.Microsecond)
	updates := map[string]any{"rich_payload": string(payload)}
	if !freshness.FetchedAt.IsZero() && (observed.RichFetchedAt == nil || freshness.FetchedAt.After(*observed.RichFetchedAt)) {
		updates["rich_fetched_at"], updates["rich_next_retry_at"] = freshness.FetchedAt, nil
	}
	result := r.db.WithContext(ctx).Model(&model.DoubanSubject{}).
		Where("id = ? AND rich_payload IS NOT DISTINCT FROM ?::jsonb AND fetched_at IS NOT DISTINCT FROM ? AND rich_next_retry_at IS NOT DISTINCT FROM ?", observed.ID, observed.RichPayload, observed.FetchedAt, observed.RichNextRetryAt).
		UpdateColumns(updates)
	return result.RowsAffected > 0, result.Error
}

// RecordRichFailure 保留已有补充资料，且不把迟到失败写到更新后的资料上。
func (r *DoubanRepository) RecordRichFailure(ctx context.Context, observed *model.DoubanSubject, retryAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.DoubanSubject{}).
		Where("id = ? AND fetched_at IS NOT DISTINCT FROM ? AND rich_fetched_at IS NOT DISTINCT FROM ? AND rich_next_retry_at IS NOT DISTINCT FROM ?", observed.ID, observed.FetchedAt, observed.RichFetchedAt, observed.RichNextRetryAt).
		UpdateColumn("rich_next_retry_at", retryAt).Error
}

// Save 只接受真实 JSON；旧响应或降级资料不得覆盖已保存的完整资料。
func (r *DoubanRepository) Save(ctx context.Context, id, kind string, payload []byte, quality string, fetchedAt time.Time) error {
	_, err := r.SaveWithResult(ctx, id, kind, payload, quality, fetchedAt)
	return err
}

// SaveWithResult 同时报告新资料是否被接纳，迟到或降级写入不计为更新。
func (r *DoubanRepository) SaveWithResult(ctx context.Context, id, kind string, payload []byte, quality string, fetchedAt time.Time) (bool, error) {
	if strings.TrimSpace(id) == "" || (kind != model.MetadataKindMovie && kind != model.MetadataKindSeries) || !model.DoubanPayloadMatchesIdentity(payload, id, kind) || (quality != model.DoubanQualityComplete && quality != model.DoubanQualityDegraded && quality != model.DoubanQualityPartial) {
		return false, errors.New("invalid douban subject")
	}
	text := string(payload)
	subject := model.DoubanSubject{DoubanID: id, EntityKind: kind, Payload: &text,
		Rating: doubanSnapshotRating(text), Quality: quality, FetchedAt: &fetchedAt, LastAttemptAt: &fetchedAt, RatingCheckedAt: &fetchedAt}
	updates := clause.AssignmentColumns([]string{"payload", "quality", "fetched_at", "updated_at"})
	// 成功资料仍可补录，但迟到结果不能清除较新的失败退避。
	updates = append(updates,
		clause.Assignment{Column: clause.Column{Name: "rating"}, Value: clause.Expr{SQL: `CASE WHEN excluded.rating > 0 AND (douban_subjects.rating_checked_at IS NULL OR douban_subjects.rating_checked_at <= excluded.fetched_at) THEN excluded.rating ELSE douban_subjects.rating END`}},
		clause.Assignment{Column: clause.Column{Name: "rating_checked_at"}, Value: clause.Expr{SQL: `GREATEST(douban_subjects.rating_checked_at, excluded.fetched_at)`}},
		clause.Assignment{Column: clause.Column{Name: "rating_retry_at"}, Value: clause.Expr{SQL: `CASE WHEN douban_subjects.rating_checked_at IS NULL OR douban_subjects.rating_checked_at <= excluded.fetched_at THEN NULL ELSE douban_subjects.rating_retry_at END`}},
		clause.Assignment{Column: clause.Column{Name: "last_attempt_at"}, Value: clause.Expr{SQL: `CASE WHEN douban_subjects.last_attempt_at IS NULL OR douban_subjects.last_attempt_at <= excluded.last_attempt_at THEN excluded.last_attempt_at ELSE douban_subjects.last_attempt_at END`}},
		clause.Assignment{Column: clause.Column{Name: "next_retry_at"}, Value: clause.Expr{SQL: `CASE WHEN douban_subjects.last_attempt_at IS NULL OR douban_subjects.last_attempt_at <= excluded.last_attempt_at THEN excluded.next_retry_at ELSE douban_subjects.next_retry_at END`}},
	)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "douban_id"}, {Name: "entity_kind"}},
		DoUpdates: updates,
		Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: `
(douban_subjects.fetched_at IS NULL OR douban_subjects.fetched_at <= excluded.fetched_at)
AND (douban_subjects.quality <> 'complete' OR excluded.quality = 'complete')
AND (douban_subjects.quality <> 'degraded' OR excluded.quality <> 'partial')`}}},
	}).Create(&subject)
	return result.RowsAffected > 0, result.Error
}

func (r *DoubanRepository) RecordFailure(ctx context.Context, id, kind string, at, retryAt time.Time) error {
	if strings.TrimSpace(id) == "" || (kind != model.MetadataKindMovie && kind != model.MetadataKindSeries) {
		return errors.New("invalid douban subject")
	}
	subject := model.DoubanSubject{DoubanID: id, EntityKind: kind, LastAttemptAt: &at, NextRetryAt: &retryAt}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "douban_id"}, {Name: "entity_kind"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_attempt_at", "next_retry_at", "updated_at"}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "douban_subjects.last_attempt_at IS NULL OR douban_subjects.last_attempt_at <= excluded.last_attempt_at"}}},
	}).Create(&subject).Error
}

func (r *DoubanRepository) FindQuery(ctx context.Context, key string, now time.Time) ([]byte, error) {
	var row model.DoubanQueryCache
	err := r.db.WithContext(ctx).Where("key = ? AND expires_at > ?", key, now).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return []byte(row.Payload), err
}

func (r *DoubanRepository) SaveQuery(ctx context.Context, key string, payload []byte, at time.Time, ttl time.Duration) error {
	row := model.DoubanQueryCache{Key: key, Payload: string(payload), FetchedAt: at, ExpiresAt: at.Add(ttl)}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, UpdateAll: true}).Create(&row).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Where("expires_at <= ?", at).Delete(&model.DoubanQueryCache{}).Error
}

// FindForMetadata 按当前关联和人工确认的实际类型取资料，改绑后不再读取旧条目。
func (r *DoubanRepository) FindForMetadata(ctx context.Context, metadataID string) (*model.DoubanSubject, error) {
	identifier, err := uniqueDoubanIdentifier(ctx, r.db, metadataID)
	if err != nil || identifier == nil {
		return nil, err
	}
	return r.Find(ctx, identifier.ExternalID, doubanIdentifierKind(*identifier))
}

func uniqueDoubanIdentifier(ctx context.Context, db *gorm.DB, metadataID string) (*model.MetadataIdentifier, error) {
	var identifiers []model.MetadataIdentifier
	err := db.WithContext(ctx).Table("metadata_identifiers AS mid").Select("mid.*").
		Joins("JOIN metadata_items AS mi ON mi.id = mid.metadata_id AND mi.kind = mid.entity_kind").
		Where("mid.metadata_id = ? AND mid.provider = 'douban'", metadataID).Limit(2).Find(&identifiers).Error
	if err != nil || len(identifiers) != 1 {
		return nil, err
	}
	return &identifiers[0], nil
}

func doubanIdentifierKind(identifier model.MetadataIdentifier) string {
	if identifier.DoubanEntityKind != "" {
		return identifier.DoubanEntityKind
	}
	return identifier.EntityKind
}

func (r *MetadataRepository) saveDoubanSubject(ctx context.Context, metadataID string, payload []byte, at time.Time, degraded bool) error {
	identifier, err := uniqueDoubanIdentifier(ctx, r.db, metadataID)
	if err != nil || identifier == nil {
		return err
	}
	kind := doubanIdentifierKind(*identifier)
	repo := &DoubanRepository{db: r.db}
	existing, err := repo.Find(ctx, identifier.ExternalID, kind)
	if err != nil {
		return err
	}
	// 使用缓存完成本地补齐不能把旧响应重新标记为刚获取。
	if existing != nil && existing.Payload != nil && sameDoubanJSON([]byte(*existing.Payload), payload) {
		return nil
	}
	return repo.Save(ctx, identifier.ExternalID, kind, payload, model.DoubanPayloadQuality(payload, degraded), at)
}

// JSONB 会规范化空白和键顺序；兼容写入必须按 JSON 值比较获取时间。
func sameDoubanJSON(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

// refreshCandidates 的计数与选择共用基础资料到期和失败退避规则。
func (r *DoubanRepository) refreshCandidates(ctx context.Context, before, now time.Time) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.DoubanSubject{}).
		Where("supplement_pending = false").
		Where("entity_kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries}).
		Where("next_retry_at IS NULL OR next_retry_at <= ?", now).
		Where("payload IS NULL OR fetched_at IS NULL OR fetched_at <= ? OR quality = ? OR (quality = ? AND fetched_at <= ?)", before, model.DoubanQualityPartial, model.DoubanQualityDegraded, now.Add(-24*time.Hour))
}

// ListRefreshCandidates 限量选择最久未成功获取的基础资料，不受网页补充状态影响。
func (r *DoubanRepository) ListRefreshCandidates(ctx context.Context, before, now time.Time, limit int) ([]model.DoubanSubject, error) {
	var rows []model.DoubanSubject
	err := r.refreshCandidates(ctx, before, now).Select("douban_id, entity_kind").
		Order("fetched_at ASC NULLS FIRST, douban_id, entity_kind").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *DoubanRepository) CountRefreshCandidates(ctx context.Context, before, now time.Time) (int64, error) {
	var count int64
	err := r.refreshCandidates(ctx, before, now).Count(&count).Error
	return count, err
}
