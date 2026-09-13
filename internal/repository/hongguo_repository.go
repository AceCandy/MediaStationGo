package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HongGuoRepository 仅操作红果资料表；公共媒体通过独立绑定接入。
type HongGuoRepository struct{ db *gorm.DB }

func (r *HongGuoRepository) RecordSyncFailure(ctx context.Context, sourceID string) error {
	row := model.HongGuoSyncFailure{SourceID: sourceID, Attempts: 1, RetryAt: time.Now().Add(time.Hour)}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(map[string]any{"attempts": gorm.Expr("hongguo_sync_failures.attempts + 1"), "retry_at": row.RetryAt, "updated_at": time.Now()})}).Create(&row).Error
}

func (r *HongGuoRepository) ClearSyncFailure(ctx context.Context, sourceID string) error {
	return r.db.WithContext(ctx).Where("source_id = ?", sourceID).Delete(&model.HongGuoSyncFailure{}).Error
}

func (r *HongGuoRepository) DueSyncFailures(ctx context.Context) ([]model.HongGuoSyncFailure, error) {
	rows := []model.HongGuoSyncFailure{}
	err := r.db.WithContext(ctx).Where("retry_at <= ?", time.Now()).Order("retry_at, source_id").Limit(50).Find(&rows).Error
	return rows, err
}

// SaveDetail 原子替换成功资料快照，刷新保持作品和分集 ID 不变。
func (r *HongGuoRepository) SaveDetail(ctx context.Context, input hongguo.Work) (*model.HongGuoWork, error) {
	if !hongguo.ValidID(input.SourceID) || input.Title == "" || !json.Valid(input.Snapshot) || input.EpisodeCount < 0 || input.EpisodeCount > 100000 {
		return nil, errors.New("红果详情无效")
	}
	tags, err := json.Marshal(input.Tags)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	work := model.HongGuoWork{SourceID: input.SourceID, Kind: model.MetadataKindSeries, Title: input.Title, Overview: input.Overview, Tags: string(tags), EpisodeCount: input.EpisodeCount, TotalEpisodes: input.TotalEpisodes, AccessibleEpisodes: input.AccessibleEpisodes, UpdateText: input.UpdateText, SourceStatus: input.SourceStatus, Completed: input.Completed, FirstVisibleAt: input.FirstVisibleAt, Rating: input.Rating, RatingCount: input.RatingCount, RefreshedAt: now}
	if input.IsMovie() {
		work.Kind = model.MetadataKindMovie
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 冲突更新同时持有作品行锁，串行刷新其分集、人物与快照。
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.AssignmentColumns([]string{"kind", "title", "overview", "tags", "episode_count", "total_episodes", "accessible_episodes", "update_text", "source_status", "completed", "first_visible_at", "rating", "rating_count", "refreshed_at", "updated_at"})}, clause.Returning{}).Create(&work).Error; err != nil {
			return err
		}
		if work.Kind == model.MetadataKindMovie {
			var members int64
			if err := tx.Model(&model.HongGuoGroupMember{}).Where("work_id = ?", work.ID).Count(&members).Error; err != nil {
				return err
			}
			if members > 0 {
				return errors.New("已聚合剧不能自动变为电影，请先解除聚合")
			}
		}
		// 不删除已存在分集：上游临时缩短列表不应破坏绑定或观看身份。
		episodes := make([]model.HongGuoEpisode, 0, work.EpisodeCount)
		for number := 1; number <= work.EpisodeCount; number++ {
			episode := model.HongGuoEpisode{WorkID: work.ID, Number: number}
			if number <= len(input.VideoIDs) {
				episode.SourceVideoID = input.VideoIDs[number-1]
			}
			episodes = append(episodes, episode)
		}
		if len(episodes) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "work_id"}, {Name: "number"}}, DoUpdates: clause.Assignments(map[string]any{"source_video_id": gorm.Expr("COALESCE(NULLIF(EXCLUDED.source_video_id, ''), hongguo_episodes.source_video_id)"), "updated_at": now})}).CreateInBatches(&episodes, 500).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("work_id = ?", work.ID).Delete(&model.HongGuoCredit{}).Error; err != nil {
			return err
		}
		seen := make(map[string]bool)
		for order, p := range input.People {
			if !hongguo.ValidID(p.SourceID) || p.Name == "" {
				return errors.New("红果人物无效")
			}
			if seen[p.SourceID] {
				continue
			}
			seen[p.SourceID] = true
			person := model.HongGuoPerson{SourceID: p.SourceID, Name: p.Name}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "updated_at"})}, clause.Returning{}).Create(&person).Error; err != nil {
				return err
			}
			credit := model.HongGuoCredit{WorkID: work.ID, PersonID: person.ID, Subtitle: p.Subtitle, SortOrder: order}
			if err := tx.Create(&credit).Error; err != nil {
				return err
			}
			if err := saveHongGuoArtwork(tx, nil, &person.ID, p.AvatarURL, now); err != nil {
				return err
			}
		}
		if err := saveHongGuoArtwork(tx, &work.ID, nil, input.CoverURL, now); err != nil {
			return err
		}
		snapshot := model.HongGuoSnapshot{WorkID: work.ID, Payload: string(input.Snapshot), FetchedAt: now}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "work_id"}}, DoUpdates: clause.AssignmentColumns([]string{"payload", "fetched_at"})}).Create(&snapshot).Error
	})
	if err != nil {
		return nil, err
	}
	return &work, nil
}

func saveHongGuoArtwork(tx *gorm.DB, workID, personID *string, sourceURL string, now time.Time) error {
	if sourceURL == "" {
		return nil
	}
	owner := "work_id"
	if personID != nil {
		owner = "person_id"
	}
	asset := model.HongGuoArtwork{WorkID: workID, PersonID: personID, SourceURL: sourceURL, NextAttemptAt: &now}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: owner}}, DoUpdates: clause.Assignments(map[string]any{
		"source_url": sourceURL, "updated_at": now,
		"next_attempt_at": gorm.Expr("CASE WHEN hongguo_artworks.source_url <> EXCLUDED.source_url OR hongguo_artworks.local_key = '' THEN EXCLUDED.next_attempt_at ELSE hongguo_artworks.next_attempt_at END"),
		"attempts":        gorm.Expr("CASE WHEN hongguo_artworks.source_url <> EXCLUDED.source_url THEN 0 ELSE hongguo_artworks.attempts END"),
	})}).Create(&asset).Error
}

func (r *HongGuoRepository) FindBySourceID(ctx context.Context, id string) (*model.HongGuoWork, error) {
	var work model.HongGuoWork
	err := r.db.WithContext(ctx).Where("source_id = ?", id).First(&work).Error
	return &work, err
}

// HongGuoListWork 是海报目录投影，只携带本地图片标识和题材，不暴露上游图片地址。
type HongGuoListWork struct {
	model.HongGuoWork
	ArtworkID string   `json:"artwork_id"`
	TagList   []string `gorm:"-" json:"tags"`
}

// List 先分页作品，不在资料列表中加载全部分集、人物或图片原始地址。
func (r *HongGuoRepository) List(ctx context.Context, search string, page, pageSize int) ([]HongGuoListWork, int64, error) {
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 {
		return nil, 0, errors.New("分页参数无效")
	}
	query := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.HongGuoWork{})
		if search != "" {
			q = q.Where("POSITION(LOWER(?) IN LOWER(title)) > 0 OR source_id = ?", search, search)
		}
		return q
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]HongGuoListWork, 0)
	err := query().Select("hongguo_works.*, COALESCE(a.id, '') AS artwork_id").
		Joins("LEFT JOIN hongguo_artworks a ON a.work_id = hongguo_works.id").
		Order("hongguo_works.created_at DESC, hongguo_works.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].TagList = []string{}
		if err := json.Unmarshal([]byte(rows[i].Tags), &rows[i].TagList); err != nil {
			return nil, 0, err
		}
	}
	return rows, total, err
}

func (r *HongGuoRepository) SyncState(ctx context.Context, category string) (model.HongGuoSyncState, error) {
	state := model.HongGuoSyncState{Category: category, NextPage: 1}
	err := r.db.WithContext(ctx).Where("category = ?", category).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return state, err
}

func (r *HongGuoRepository) SaveSyncState(ctx context.Context, state model.HongGuoSyncState) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "category"}}, DoUpdates: clause.AssignmentColumns([]string{"next_page", "after_id", "updated_at"})}).Create(&state).Error
}

func (r *HongGuoRepository) WorksAfter(ctx context.Context, id string, limit int) ([]model.HongGuoWork, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("批次大小无效")
	}
	var rows []model.HongGuoWork
	err := r.db.WithContext(ctx).Where("id > ?", id).
		Where("refreshed_at < ?", time.Now().Add(-24*time.Hour)).
		Where("source_id NOT IN (SELECT source_id FROM hongguo_sync_failures)").
		Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *HongGuoRepository) DueArtwork(ctx context.Context, after string, cutoff time.Time) ([]model.HongGuoArtwork, error) {
	var rows []model.HongGuoArtwork
	err := r.db.WithContext(ctx).Where("id > ? AND next_attempt_at <= ?", after, cutoff).Order("id").Limit(50).Find(&rows).Error
	return rows, err
}

// FinishArtwork 仅写回同一来源地址的结果，避免并发刷新时关联到旧图片。
func (r *HongGuoRepository) FinishArtwork(ctx context.Context, asset model.HongGuoArtwork, localKey string, retryAt *time.Time) error {
	values := map[string]any{"next_attempt_at": retryAt, "attempts": asset.Attempts + 1}
	if localKey != "" {
		values["local_key"] = localKey
		values["attempts"] = 0
	}
	return r.db.WithContext(ctx).Model(&model.HongGuoArtwork{}).Where("id = ? AND source_url = ?", asset.ID, asset.SourceURL).Updates(values).Error
}

func (r *HongGuoRepository) Artwork(ctx context.Context, id string) (*model.HongGuoArtwork, error) {
	var row model.HongGuoArtwork
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	return &row, err
}

// ScheduleMissingArtwork 只唤醒已完成但本地文件丢失的图片，不覆盖现有失败退避。
func (r *HongGuoRepository) ScheduleMissingArtwork(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&model.HongGuoArtwork{}).Where("id = ? AND next_attempt_at IS NULL", id).Update("next_attempt_at", time.Now()).Error
}

// HongGuoDetail 是独立资料详情，不包含原始快照、图片源地址和播放地址。
type HongGuoDetail struct {
	model.HongGuoWork
	TagList  []string                  `json:"tags"`
	Episodes []model.HongGuoEpisode    `json:"episodes"`
	Credits  []model.HongGuoCredit     `json:"credits"`
	Artwork  []model.HongGuoArtwork    `json:"artwork"`
	Group    *model.HongGuoGroupMember `json:"group,omitempty"`
}

func (r *HongGuoRepository) Detail(ctx context.Context, sourceID string) (*HongGuoDetail, error) {
	work, err := r.FindBySourceID(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	detail := &HongGuoDetail{HongGuoWork: *work, TagList: []string{}, Episodes: []model.HongGuoEpisode{}, Credits: []model.HongGuoCredit{}, Artwork: []model.HongGuoArtwork{}}
	if err := json.Unmarshal([]byte(work.Tags), &detail.TagList); err != nil {
		return nil, err
	}
	// 详情只加载有界分集预览；完整分集通过独立分页接口读取。
	if err := r.db.WithContext(ctx).Where("work_id = ?", work.ID).Order("number").Limit(100).Find(&detail.Episodes).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Preload("Person").Where("work_id = ?", work.ID).Order("sort_order").Limit(200).Find(&detail.Credits).Error; err != nil {
		return nil, err
	}
	personIDs := make([]string, 0, len(detail.Credits))
	for _, credit := range detail.Credits {
		personIDs = append(personIDs, credit.PersonID)
	}
	if err := r.db.WithContext(ctx).Where("work_id = ? OR person_id IN ?", work.ID, personIDs).Find(&detail.Artwork).Error; err != nil {
		return nil, err
	}
	var member model.HongGuoGroupMember
	err = r.db.WithContext(ctx).Where("work_id = ?", work.ID).First(&member).Error
	if err == nil {
		detail.Group = &member
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return detail, nil
}

func (r *HongGuoRepository) Episodes(ctx context.Context, sourceID string, page, size int) ([]model.HongGuoEpisode, int64, error) {
	if !hongguo.ValidID(sourceID) || page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, 0, errors.New("分集查询参数无效")
	}
	work, err := r.FindBySourceID(ctx, sourceID)
	if err != nil {
		return nil, 0, err
	}
	q := r.db.WithContext(ctx).Model(&model.HongGuoEpisode{}).Where("work_id = ?", work.ID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.HongGuoEpisode{}
	err = q.Order("number").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
