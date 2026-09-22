package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HongGuoRepository 仅操作红果资料表；公共媒体通过独立绑定接入。
type HongGuoRepository struct {
	db *gorm.DB
	searchIndex
}

func (r *HongGuoRepository) RecordSyncFailure(ctx context.Context, sourceID string, retryAt time.Time) error {
	row := model.HongGuoSyncFailure{SourceID: sourceID, Attempts: 1, RetryAt: retryAt}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(map[string]any{"attempts": gorm.Expr("hongguo_sync_failures.attempts + 1"), "retry_at": row.RetryAt, "updated_at": time.Now()})}).Create(&row).Error
}

func (r *HongGuoRepository) ClearSyncFailure(ctx context.Context, sourceID string) error {
	return r.db.WithContext(ctx).Where("source_id = ?", sourceID).Delete(&model.HongGuoSyncFailure{}).Error
}

func (r *HongGuoRepository) DueSyncFailures(ctx context.Context) ([]model.HongGuoSyncFailure, error) {
	rows := []model.HongGuoSyncFailure{}
	err := r.db.WithContext(ctx).Where("retry_at <= ?", time.Now()).
		Where("NOT EXISTS (SELECT 1 FROM hongguo_works w WHERE w.source_id = hongguo_sync_failures.source_id AND w.completed = true)").
		Order("retry_at, source_id").Limit(50).Find(&rows).Error
	return rows, err
}

// SaveDetail 原子替换成功资料快照，刷新保持作品和分集 ID 不变。
func (r *HongGuoRepository) SaveDetail(ctx context.Context, input hongguo.Work) (*model.HongGuoWork, error) {
	work, _, err := r.SaveDetailWithChange(ctx, input)
	return work, err
}

// SaveDetailWithChange 按已持久化的业务快照区分首次入库、内容更新和无变化。
func (r *HongGuoRepository) SaveDetailWithChange(ctx context.Context, input hongguo.Work) (*model.HongGuoWork, string, error) {
	if !hongguo.ValidID(input.SourceID) || input.Title == "" || !json.Valid(input.Snapshot) || input.EpisodeCount < 0 || input.EpisodeCount > 100000 {
		return nil, "", errors.New("红果详情无效")
	}
	tags, err := json.Marshal(input.Tags)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	change := "new"
	work := model.HongGuoWork{SourceID: input.SourceID, Kind: model.MetadataKindSeries, Title: input.Title, Overview: input.Overview, Tags: string(tags), EpisodeCount: input.EpisodeCount, TotalEpisodes: input.TotalEpisodes, AccessibleEpisodes: input.AccessibleEpisodes, UpdateText: input.UpdateText, SourceStatus: input.SourceStatus, Completed: input.Completed, FirstVisibleAt: input.FirstVisibleAt, Rating: input.Rating, RatingCount: input.RatingCount, RefreshedAt: now}
	if input.IsMovie() {
		work.Kind = model.MetadataKindMovie
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous model.HongGuoWork
		found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", input.SourceID).Take(&previous)
		if found.Error != nil && !errors.Is(found.Error, gorm.ErrRecordNotFound) {
			return found.Error
		}
		if err := tx.Model(&model.HongGuoDiscovery{}).Select("source_category").Where("source_id = ?", input.SourceID).Scan(&work.SourceCategory).Error; err != nil {
			return err
		}
		if work.SourceCategory == "" {
			if err := tx.Model(&model.HongGuoWork{}).Select("source_category").Where("source_id = ?", input.SourceID).Scan(&work.SourceCategory).Error; err != nil {
				return err
			}
		}
		if found.Error == nil {
			change = "updated"
			var previousSnapshot model.HongGuoSnapshot
			result := tx.Where("work_id = ?", previous.ID).Take(&previousSnapshot)
			if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return result.Error
			}
			if result.Error == nil && previous.SourceCategory == work.SourceCategory {
				var before, after any
				beforeDecoder := json.NewDecoder(strings.NewReader(previousSnapshot.Payload))
				beforeDecoder.UseNumber()
				if err := beforeDecoder.Decode(&before); err != nil {
					return err
				}
				afterDecoder := json.NewDecoder(strings.NewReader(string(input.Snapshot)))
				afterDecoder.UseNumber()
				if err := afterDecoder.Decode(&after); err != nil {
					return err
				}
				if reflect.DeepEqual(before, after) {
					change = "unchanged"
				}
			}
		}
		// 冲突更新同时持有作品行锁，串行刷新其分集、人物与快照。
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.AssignmentColumns([]string{"source_category", "kind", "title", "overview", "tags", "episode_count", "total_episodes", "accessible_episodes", "update_text", "source_status", "completed", "first_visible_at", "rating", "rating_count", "refreshed_at", "updated_at"})}, clause.Returning{}).Create(&work).Error; err != nil {
			return err
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
			if err := saveHongGuoArtwork(tx, nil, nil, &person.ID, p.AvatarURL, now); err != nil {
				return err
			}
		}
		if err := saveHongGuoArtwork(tx, &input.SourceID, &work.ID, nil, input.CoverURL, now); err != nil {
			return err
		}
		snapshot := model.HongGuoSnapshot{WorkID: work.ID, Payload: string(input.Snapshot), FetchedAt: now}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "work_id"}}, DoUpdates: clause.AssignmentColumns([]string{"payload", "fetched_at"})}).Create(&snapshot).Error
	})
	if err != nil {
		return nil, "", err
	}
	r.refreshSearchWork(ctx, work.ID, work.RelatedAlbumID)
	return &work, change, nil
}

func saveHongGuoArtwork(tx *gorm.DB, sourceID, workID, personID *string, sourceURL string, now time.Time) error {
	if personID != nil {
		if sourceURL == "" {
			return nil
		}
		asset := model.HongGuoArtwork{PersonID: personID, SourceURL: sourceURL, NextAttemptAt: &now}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "person_id"}}, DoUpdates: clause.Assignments(hongGuoArtworkUpdates(sourceURL, now))}).Create(&asset).Error
	}
	if sourceID == nil {
		return errors.New("红果海报缺少来源 ID")
	}
	if sourceURL == "" {
		if workID == nil {
			return nil
		}
		return tx.Model(&model.HongGuoArtwork{}).Where("source_id = ?", *sourceID).Update("work_id", *workID).Error
	}
	asset := model.HongGuoArtwork{SourceID: sourceID, WorkID: workID, SourceURL: sourceURL, NextAttemptAt: &now}
	updates := hongGuoArtworkUpdates(sourceURL, now)
	if workID == nil {
		updates = map[string]any{
			"source_url":      gorm.Expr("CASE WHEN hongguo_artworks.work_id IS NULL THEN EXCLUDED.source_url ELSE hongguo_artworks.source_url END"),
			"updated_at":      gorm.Expr("CASE WHEN hongguo_artworks.work_id IS NULL THEN EXCLUDED.updated_at ELSE hongguo_artworks.updated_at END"),
			"next_attempt_at": gorm.Expr("CASE WHEN hongguo_artworks.work_id IS NULL AND (hongguo_artworks.source_url <> EXCLUDED.source_url OR hongguo_artworks.local_key = '') THEN EXCLUDED.next_attempt_at ELSE hongguo_artworks.next_attempt_at END"),
			"attempts":        gorm.Expr("CASE WHEN hongguo_artworks.work_id IS NULL AND hongguo_artworks.source_url <> EXCLUDED.source_url THEN 0 ELSE hongguo_artworks.attempts END"),
		}
	} else {
		updates["work_id"] = *workID
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(updates)}).Create(&asset).Error
}

func hongGuoArtworkUpdates(sourceURL string, now time.Time) map[string]any {
	return map[string]any{
		"source_url": sourceURL, "updated_at": now,
		"next_attempt_at": gorm.Expr("CASE WHEN hongguo_artworks.source_url <> EXCLUDED.source_url OR hongguo_artworks.local_key = '' THEN EXCLUDED.next_attempt_at ELSE hongguo_artworks.next_attempt_at END"),
		"attempts":        gorm.Expr("CASE WHEN hongguo_artworks.source_url <> EXCLUDED.source_url THEN 0 ELSE hongguo_artworks.attempts END"),
	}
}

func (r *HongGuoRepository) FindBySourceID(ctx context.Context, id string) (*model.HongGuoWork, error) {
	var work model.HongGuoWork
	err := r.db.WithContext(ctx).Where("source_id = ?", id).First(&work).Error
	return &work, err
}

// SetSourceCategory 同步修改摘要和完整资料，避免后续刷新从另一张表恢复旧分类。
func (r *HongGuoRepository) SetSourceCategory(ctx context.Context, sourceID, category string) error {
	if !hongguo.ValidID(sourceID) || !hongguo.ValidCategory(category) {
		return errors.New("红果作品分类无效")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var affected int64
		for _, target := range []any{&model.HongGuoDiscovery{}, &model.HongGuoWork{}} {
			result := tx.Model(target).Where("source_id = ?", sourceID).Update("source_category", category)
			if result.Error != nil {
				return result.Error
			}
			affected += result.RowsAffected
		}
		if affected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// HongGuoListWork 是海报目录投影，只携带本地图片标识和题材，不暴露上游图片地址。
type HongGuoListWork struct {
	model.HongGuoWork
	ArtworkID  string   `json:"artwork_id"`
	TagList    []string `gorm:"-" json:"tags"`
	Hydrated   bool     `json:"hydrated"`
	GroupID    string   `gorm:"-" json:"group_id,omitempty"`
	Downloaded bool     `gorm:"-" json:"downloaded"`
}

// List 在数据库分页前合并正式作品和仅发现摘要，不加载分集、人物或图片原始地址。
func (r *HongGuoRepository) List(ctx context.Context, search, sourceCategory, category, rank string, page, pageSize int) ([]HongGuoListWork, int64, error) {
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 {
		return nil, 0, errors.New("分页参数无效")
	}
	if rank != "" && !hongguo.ValidRank(rank) {
		return nil, 0, errors.New("榜单类型无效")
	}
	order := "catalog.first_visible_at DESC NULLS LAST, catalog.created_at DESC, catalog.id DESC"
	if rank != "" {
		order = "rank_entry.position ASC, catalog.source_id ASC"
	}
	const catalog = `WITH catalog AS (
SELECT w.id, w.source_id, w.source_category, w.kind, w.title, w.overview, w.tags,
       w.episode_count, w.total_episodes, w.accessible_episodes, w.update_text,
       w.source_status, w.completed, w.first_visible_at, w.rating, w.rating_count,
       w.refreshed_at, w.created_at, w.updated_at, TRUE AS hydrated
FROM hongguo_works AS w
UNION ALL
SELECT d.source_id AS id, d.source_id, d.source_category, '' AS kind, d.title, d.overview, '[]' AS tags,
       d.episode_count, 0 AS total_episodes, 0 AS accessible_episodes, d.update_text,
       '' AS source_status, FALSE AS completed, NULL::timestamptz AS first_visible_at,
       0::real AS rating, 0::bigint AS rating_count, TIMESTAMPTZ '0001-01-01 00:00:00+00' AS refreshed_at,
       d.created_at, d.updated_at, FALSE AS hydrated
FROM hongguo_discoveries AS d
WHERE NOT EXISTS (SELECT 1 FROM hongguo_works AS w WHERE w.source_id = d.source_id)
)`
	joins := " FROM catalog"
	args := []any{}
	if rank != "" {
		joins += " JOIN hongguo_rank_entries AS rank_entry ON rank_entry.source_id = catalog.source_id AND rank_entry.rank_key = ?"
		args = append(args, rank)
	}
	conditions := []string{"(catalog.source_category IS NULL OR catalog.source_category <> 'comic')"}
	if search != "" {
		conditions = append(conditions, "(POSITION(LOWER(?) IN LOWER(catalog.title)) > 0 OR catalog.source_id = ?)")
		args = append(args, search, search)
	}
	if sourceCategory == "other" {
		conditions = append(conditions, "COALESCE(catalog.source_category, '') = ''")
	} else if sourceCategory != "" {
		conditions = append(conditions, "catalog.source_category = ?")
		args = append(args, sourceCategory)
	}
	if category != "" {
		conditions = append(conditions, "catalog.hydrated AND jsonb_exists(catalog.tags::jsonb, ?)")
		args = append(args, category)
	}
	where := " WHERE " + strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.WithContext(ctx).Raw(catalog+" SELECT COUNT(*)"+joins+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]HongGuoListWork, 0)
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	err := r.db.WithContext(ctx).Raw(catalog+" SELECT catalog.*, COALESCE(a.id, '') AS artwork_id"+joins+
		" LEFT JOIN hongguo_artworks AS a ON a.source_id = catalog.source_id"+where+
		" ORDER BY "+order+" LIMIT ? OFFSET ?", listArgs...).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].TagList = []string{}
		if err := json.Unmarshal([]byte(rows[i].Tags), &rows[i].TagList); err != nil {
			return nil, 0, err
		}
	}
	return rows, total, r.loadListBadges(ctx, rows)
}

// loadListBadges 批量补充聚合关系和历史下载标识；完成记录不依赖文件是否仍存在。
func (r *HongGuoRepository) loadListBadges(ctx context.Context, rows []HongGuoListWork) error {
	if len(rows) == 0 {
		return nil
	}
	sourceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		sourceIDs = append(sourceIDs, row.SourceID)
	}
	var completed []string
	if err := r.db.WithContext(ctx).Model(&model.HongGuoDownload{}).Where("source_id = ANY(?) AND status = ?", &sourceIDs, "completed").Distinct("source_id").Pluck("source_id", &completed).Error; err != nil {
		return err
	}
	downloaded := make(map[string]bool, len(completed))
	for _, id := range completed {
		downloaded[id] = true
	}
	for i := range rows {
		rows[i].Downloaded = downloaded[rows[i].SourceID]
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Hydrated {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var members []model.HongGuoWork
	if err := r.db.WithContext(ctx).Select("id, kind, related_album_id, season_index").Where("id = ANY(?)", &ids).Find(&members).Error; err != nil {
		return err
	}
	groups := make(map[string]model.HongGuoWork, len(members))
	for _, member := range members {
		groups[member.ID] = member
	}
	for i := range rows {
		member := groups[rows[i].ID]
		rows[i].RelatedAlbumID, rows[i].SeasonIndex = member.RelatedAlbumID, member.SeasonIndex
		if member.Kind == model.MetadataKindSeries && member.SeasonIndex > 0 {
			rows[i].GroupID = member.RelatedAlbumID
		}
	}
	return nil
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
		Where("completed = ?", false).
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
	TagList  []string               `json:"tags"`
	Episodes []model.HongGuoEpisode `json:"episodes"`
	Credits  []model.HongGuoCredit  `json:"credits"`
	Artwork  []model.HongGuoArtwork `json:"artwork"`
	Group    *HongGuoMembership     `json:"group,omitempty"`
}

// HongGuoMembership 仅投影官方关系，供详情和展示消费。
type HongGuoMembership struct {
	GroupID      string `json:"group_id"`
	WorkID       string `json:"work_id"`
	SeasonNumber int    `json:"season_number"`
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
	if work.Kind == model.MetadataKindSeries && work.RelatedAlbumID != "" && work.SeasonIndex > 0 {
		detail.Group = &HongGuoMembership{GroupID: work.RelatedAlbumID, WorkID: work.ID, SeasonNumber: work.SeasonIndex}
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
