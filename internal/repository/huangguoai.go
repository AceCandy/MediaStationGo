package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HuangGuoAIRepository struct{ db *gorm.DB }

type HuangGuoAIListWork struct {
	model.HuangGuoAIWork
	Tags       []string `gorm:"-" json:"tags"`
	RawTags    string   `gorm:"column:raw_tags" json:"-"`
	Categories []string `gorm:"-" json:"categories"`
	ArtworkID  string   `json:"artwork_id"`
	Hydrated   bool     `json:"hydrated"`
	DisplayID  string   `gorm:"-" json:"display_id"`
	Downloaded bool     `gorm:"-" json:"downloaded"`
}

func HuangGuoAIWorkID(w model.HuangGuoAIWork) string {
	if w.Kind == model.MetadataKindSeries {
		return "hga-group-" + w.SourceID
	}
	return "hga-work-" + w.ID
}

func (r *HuangGuoAIRepository) saveSummaries(tx *gorm.DB, items []huangguoai.Summary) ([]string, error) {
	if len(items) == 0 {
		return []string{}, nil
	}
	ids := make([]string, 0, len(items))
	rows := make([]model.HuangGuoAIDiscovery, 0, len(items))
	members := []model.HuangGuoAICategoryMembership{}
	seen := map[string]bool{}
	for _, x := range items {
		if !huangguoai.ValidID(x.SourceID) || strings.TrimSpace(x.Title) == "" || (x.Category != "" && !huangguoai.ValidCategory(x.Category)) {
			return nil, errors.New("黄果 AI 摘要无效")
		}
		if seen[x.SourceID] {
			return nil, errors.New("黄果 AI 摘要重复")
		}
		seen[x.SourceID] = true
		tags := x.Tags
		if tags == nil {
			tags = []string{}
		}
		raw, e := json.Marshal(tags)
		if e != nil {
			return nil, e
		}
		ids = append(ids, x.SourceID)
		rows = append(rows, model.HuangGuoAIDiscovery{SourceID: x.SourceID, SourceCategory: x.Category, Title: x.Title, Overview: x.Overview, Tags: string(raw), CoverURL: x.CoverURL, Rating: x.Rating, EpisodeCount: x.EpisodeCount, TotalEpisodes: x.TotalEpisodes, Completed: x.Finished, SourceCreatedAt: x.SourceCreatedAt, SourceUpdatedAt: x.SourceUpdatedAt})
		if x.Category != "" {
			members = append(members, model.HuangGuoAICategoryMembership{SourceID: x.SourceID, Category: x.Category})
		}
	}
	var inserted []string
	if err := tx.Raw(`INSERT INTO huangguoai_discoveries (source_id,created_at,updated_at) SELECT source_id,now(),now() FROM unnest(?::text[]) AS input(source_id) ON CONFLICT (source_id) DO NOTHING RETURNING source_id`, &ids).Scan(&inserted).Error; err != nil {
		return nil, err
	}
	updates := map[string]any{"updated_at": gorm.Expr("EXCLUDED.updated_at")}
	for _, name := range []string{"title", "overview", "cover_url", "source_created_at"} {
		updates[name] = gorm.Expr("COALESCE(NULLIF(EXCLUDED." + name + ",''),huangguoai_discoveries." + name + ")")
	}
	for _, name := range []string{"total_episodes", "completed", "source_updated_at"} {
		updates[name] = gorm.Expr("COALESCE(EXCLUDED." + name + ",huangguoai_discoveries." + name + ")")
	}
	for _, name := range []string{"rating", "episode_count"} {
		updates[name] = gorm.Expr("CASE WHEN EXCLUDED." + name + " > 0 THEN EXCLUDED." + name + " ELSE huangguoai_discoveries." + name + " END")
	}
	updates["tags"] = gorm.Expr("CASE WHEN EXCLUDED.tags <> '[]'::jsonb THEN EXCLUDED.tags ELSE huangguoai_discoveries.tags END")
	updates["source_category"] = gorm.Expr("COALESCE(NULLIF(huangguoai_discoveries.source_category,''),EXCLUDED.source_category)")
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(updates)}).CreateInBatches(&rows, 100).Error; err != nil {
		return nil, err
	}
	if len(members) > 0 {
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(&members, 100).Error; err != nil {
			return nil, err
		}
	}
	var canonical []string
	if err := tx.Model(&model.HuangGuoAIWork{}).Where("source_id IN ?", ids).Pluck("source_id", &canonical).Error; err != nil {
		return nil, err
	}
	hydrated := map[string]bool{}
	for _, id := range canonical {
		hydrated[id] = true
	}
	for _, x := range items {
		if !hydrated[x.SourceID] {
			if err := saveHuangGuoAIArtwork(tx, x.SourceID, nil, x.CoverURL); err != nil {
				return nil, err
			}
		}
	}
	if len(inserted) == 0 {
		return inserted, nil
	}
	var existing []string
	if err := tx.Model(&model.HuangGuoAIWork{}).Where("source_id IN ?", inserted).Pluck("source_id", &existing).Error; err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, id := range existing {
		known[id] = true
	}
	result := []string{}
	for _, id := range inserted {
		if !known[id] {
			result = append(result, id)
		}
	}
	return result, nil
}

func saveHuangGuoAIArtwork(tx *gorm.DB, id string, workID *string, source string) error {
	if source == "" {
		return nil
	}
	a := model.HuangGuoAIArtwork{SourceID: id, WorkID: workID, SourceURL: source}
	updates := map[string]any{"source_url": source, "updated_at": time.Now(), "next_attempt_at": gorm.Expr("CASE WHEN huangguoai_artworks.source_url = EXCLUDED.source_url THEN huangguoai_artworks.next_attempt_at ELSE now() END"), "attempts": gorm.Expr("CASE WHEN huangguoai_artworks.source_url = EXCLUDED.source_url THEN huangguoai_artworks.attempts ELSE 0 END")}
	if workID != nil {
		updates["work_id"] = *workID
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(updates)}).Create(&a).Error
}

func (r *HuangGuoAIRepository) SaveDiscoveryPage(ctx context.Context, items []huangguoai.Summary, state model.HuangGuoAISyncState) ([]string, error) {
	if !huangguoai.ValidCategory(state.Category) || state.Ordering != "hot" || state.NextPage < 1 || state.NextPage > huangguoai.MaxPage {
		return nil, errors.New("黄果 AI 检查点无效")
	}
	for _, item := range items {
		if item.Category != state.Category {
			return nil, errors.New("黄果 AI 分类与检查点不一致")
		}
	}
	var ids []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		ids, e = r.saveSummaries(tx, items)
		if e != nil {
			return e
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&state).Error
	})
	return ids, err
}

func (r *HuangGuoAIRepository) RegisterSummaries(ctx context.Context, items []huangguoai.Summary) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { _, err := r.saveSummaries(tx, items); return err })
}

func (r *HuangGuoAIRepository) SyncState(ctx context.Context, category string) (model.HuangGuoAISyncState, error) {
	state := model.HuangGuoAISyncState{Category: category, Ordering: "hot", NextPage: 1}
	err := r.db.WithContext(ctx).Where("category = ? AND ordering = 'hot'", category).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return state, err
}

func (r *HuangGuoAIRepository) ReplaceRank(ctx context.Context, key string, items []huangguoai.Summary) ([]string, error) {
	if !huangguoai.ValidRank(key) || len(items) == 0 {
		return nil, errors.New("黄果 AI 榜单无效")
	}
	var ids []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		ids, e = r.saveSummaries(tx, items)
		if e != nil {
			return e
		}
		if e := tx.Where("rank_key = ?", key).Delete(&model.HuangGuoAIRankEntry{}).Error; e != nil {
			return e
		}
		rows := make([]model.HuangGuoAIRankEntry, len(items))
		for i, x := range items {
			rows[i] = model.HuangGuoAIRankEntry{RankKey: key, SourceID: x.SourceID, Position: i + 1}
		}
		return tx.CreateInBatches(&rows, 100).Error
	})
	return ids, err
}

func (r *HuangGuoAIRepository) SaveDetail(ctx context.Context, input huangguoai.Work) (*model.HuangGuoAIWork, string, error) {
	if !huangguoai.ValidID(input.SourceID) || input.Title == "" || len(input.Episodes) == 0 {
		return nil, "", errors.New("黄果 AI 详情无效")
	}
	work := model.HuangGuoAIWork{}
	change := "new"
	var rejected error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var summary model.HuangGuoAIDiscovery
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", input.SourceID).Take(&summary).Error; e != nil {
			return e
		}
		var categories []string
		if e := tx.Model(&model.HuangGuoAICategoryMembership{}).Where("source_id = ?", input.SourceID).Pluck("category", &categories).Error; e != nil {
			return e
		}
		kind := huangguoai.Kind(summary.SourceCategory)
		if kind == "" {
			return errors.New("黄果 AI 来源分类未补齐")
		}
		for _, category := range categories {
			if huangguoai.Kind(category) != kind {
				rejected = errors.New("黄果 AI 跨类型分类冲突")
				f := model.HuangGuoAISyncFailure{Stage: "classification", SourceKey: input.SourceID, Attempts: 1, RetryAt: time.Now().Add(24 * time.Hour), ErrorCode: "category_kind_conflict"}
				if e := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&f).Error; e != nil {
					return e
				}
				return tx.Model(&model.HuangGuoAIWork{}).Where("source_id = ?", input.SourceID).Update("projection_error", "category_kind_conflict").Error
			}
		}
		var previous model.HuangGuoAIWork
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", input.SourceID).Limit(1).Find(&previous)
		if result.Error != nil {
			return result.Error
		}
		input.Category = summary.SourceCategory
		input.EpisodeCount = summary.EpisodeCount
		input.TotalEpisodes = summary.TotalEpisodes
		input.Finished = summary.Completed
		input.Rating = summary.Rating
		input.SourceCreatedAt = summary.SourceCreatedAt
		input.SourceUpdatedAt = summary.SourceUpdatedAt
		if input.Tags == nil {
			_ = json.Unmarshal([]byte(summary.Tags), &input.Tags)
		}
		if input.Tags == nil {
			input.Tags = []string{}
		}
		tags, e := json.Marshal(input.Tags)
		if e != nil {
			return e
		}
		payload, e := json.Marshal(input)
		if e != nil {
			return e
		}
		if previous.ID != "" {
			change = "updated"
			var snapshot model.HuangGuoAISnapshot
			if e := tx.Where("work_id = ?", previous.ID).Take(&snapshot).Error; e != nil {
				return e
			}
			var old any
			var next any
			_ = json.Unmarshal([]byte(snapshot.Payload), &old)
			_ = json.Unmarshal(payload, &next)
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(next)
			if string(a) == string(b) {
				change = "unchanged"
			}
		}
		work = model.HuangGuoAIWork{PermanentBase: previous.PermanentBase, SourceID: input.SourceID, SourceCategory: input.Category, Kind: kind, Title: input.Title, Overview: input.Overview, Tags: string(tags), Rating: input.Rating, EpisodeCount: input.EpisodeCount, TotalEpisodes: input.TotalEpisodes, Completed: input.Finished, SourceCreatedAt: input.SourceCreatedAt, SourceUpdatedAt: input.SourceUpdatedAt, RefreshedAt: time.Now().UTC()}
		if e := tx.Omit("LatestMediaAddedAt", "LibraryIDs").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.AssignmentColumns([]string{"source_category", "kind", "title", "overview", "tags", "rating", "episode_count", "total_episodes", "completed", "source_created_at", "source_updated_at", "refreshed_at", "updated_at", "projection_error"})}, clause.Returning{}).Create(&work).Error; e != nil {
			return e
		}
		episodes := make([]model.HuangGuoAIEpisode, 0, len(input.Episodes))
		seen := map[int]bool{}
		for _, ep := range input.Episodes {
			if ep.Number < 1 || ep.Number > 100000 || seen[ep.Number] {
				return errors.New("黄果 AI 分集坐标无效")
			}
			seen[ep.Number] = true
			episodes = append(episodes, model.HuangGuoAIEpisode{WorkID: work.ID, Number: ep.Number, PagePath: ep.PagePath})
		}
		if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "work_id"}, {Name: "number"}}, DoUpdates: clause.AssignmentColumns([]string{"page_path", "updated_at"})}).CreateInBatches(&episodes, 100).Error; e != nil {
			return e
		}
		var count int64
		if e := tx.Model(&model.HuangGuoAIEpisode{}).Where("work_id = ?", work.ID).Count(&count).Error; e != nil {
			return e
		}
		work.ConfirmedEpisodeCount = int(count)
		if e := tx.Model(&model.HuangGuoAIWork{}).Where("id = ?", work.ID).Update("confirmed_episode_count", count).Error; e != nil {
			return e
		}
		snapshot := model.HuangGuoAISnapshot{WorkID: work.ID, Payload: string(payload), FetchedAt: time.Now().UTC()}
		if e := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&snapshot).Error; e != nil {
			return e
		}
		if e := saveHuangGuoAIArtwork(tx, work.SourceID, &work.ID, input.CoverURL); e != nil {
			return e
		}
		return tx.Where("stage = 'detail' AND source_key = ?", work.SourceID).Delete(&model.HuangGuoAISyncFailure{}).Error
	})
	if err == nil && rejected != nil {
		err = rejected
	}
	return &work, change, err
}

func (r *HuangGuoAIRepository) FindBySourceID(ctx context.Context, id string) (*model.HuangGuoAIWork, error) {
	var work model.HuangGuoAIWork
	err := r.db.WithContext(ctx).Where("source_id = ?", id).Take(&work).Error
	return &work, err
}

func (r *HuangGuoAIRepository) List(ctx context.Context, keyword, category, tag, rank string, page, size int) ([]HuangGuoAIListWork, int64, error) {
	rows := []HuangGuoAIListWork{}
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, 0, errors.New("分页参数无效")
	}
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		base := tx.Table("huangguoai_discoveries d").Joins("LEFT JOIN huangguoai_works w ON w.source_id=d.source_id").Joins("LEFT JOIN huangguoai_artworks a ON a.source_id=d.source_id")
		if keyword != "" {
			pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keyword) + "%"
			base = base.Where("COALESCE(w.title,d.title) ILIKE ? OR d.source_id = ?", pattern, keyword)
		}
		if category != "" {
			base = base.Where("EXISTS (SELECT 1 FROM huangguoai_category_memberships cm WHERE cm.source_id=d.source_id AND cm.category=?)", category)
		}
		if tag != "" {
			raw, _ := json.Marshal([]string{tag})
			base = base.Where("COALESCE(w.tags,d.tags) @> ?::jsonb", string(raw))
		}
		if rank != "" {
			base = base.Joins("JOIN huangguoai_rank_entries rk ON rk.source_id=d.source_id AND rk.rank_key=?", rank)
		}
		if e := base.Session(&gorm.Session{}).Count(&total).Error; e != nil {
			return e
		}
		order := "COALESCE(w.source_updated_at,d.source_updated_at) DESC NULLS LAST,d.created_at DESC,d.source_id DESC"
		if rank != "" {
			order = "rk.position,d.source_id"
		}
		selects := `COALESCE(w.id,'') AS id,COALESCE(w.projection_error,'') AS projection_error,d.source_id,COALESCE(w.source_category,d.source_category) AS source_category,COALESCE(w.kind,CASE WHEN d.source_category IN ('ai-duanju','ai-manju') THEN 'series' WHEN d.source_category IN ('ai-huanlian','ai-mogai') THEN 'movie' ELSE '' END) AS kind,COALESCE(w.title,d.title) AS title,COALESCE(w.overview,d.overview) AS overview,COALESCE(w.tags,d.tags)::text AS raw_tags,COALESCE(w.rating,d.rating) AS rating,COALESCE(w.episode_count,d.episode_count) AS episode_count,COALESCE(w.confirmed_episode_count,0) AS confirmed_episode_count,COALESCE(w.total_episodes,d.total_episodes) AS total_episodes,COALESCE(w.completed,d.completed) AS completed,COALESCE(w.source_created_at,d.source_created_at) AS source_created_at,COALESCE(w.source_updated_at,d.source_updated_at) AS source_updated_at,d.created_at,COALESCE(a.id,'') AS artwork_id,w.id IS NOT NULL AS hydrated`
		if e := base.Select(selects).Order(order).Offset((page - 1) * size).Limit(size).Scan(&rows).Error; e != nil {
			return e
		}
		return hydrateHuangGuoAIList(tx, rows)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, total, err
}

func hydrateHuangGuoAIList(db *gorm.DB, rows []HuangGuoAIListWork) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, len(rows))
	for i, x := range rows {
		ids[i] = x.SourceID
	}
	var memberships []model.HuangGuoAICategoryMembership
	if e := db.Where("source_id IN ?", ids).Order("category").Find(&memberships).Error; e != nil {
		return e
	}
	var downloadedIDs []string
	if e := db.Model(&model.HuangGuoAIDownload{}).Where("source_id IN ? AND status = ?", ids, "completed").Distinct("source_id").Pluck("source_id", &downloadedIDs).Error; e != nil {
		return e
	}
	downloaded := map[string]bool{}
	for _, id := range downloadedIDs {
		downloaded[id] = true
	}
	byID := map[string][]string{}
	for _, x := range memberships {
		byID[x.SourceID] = append(byID[x.SourceID], x.Category)
	}
	for i := range rows {
		rows[i].Downloaded = downloaded[rows[i].SourceID]
		rows[i].Tags = []string{}
		if e := json.Unmarshal([]byte(rows[i].RawTags), &rows[i].Tags); e != nil {
			return e
		}
		rows[i].Categories = byID[rows[i].SourceID]
		if rows[i].Categories == nil {
			rows[i].Categories = []string{}
		}
		if rows[i].Hydrated {
			rows[i].DisplayID = HuangGuoAIWorkID(rows[i].HuangGuoAIWork)
		}
	}
	return nil
}

func (r *HuangGuoAIRepository) Detail(ctx context.Context, id string) (HuangGuoAIListWork, error) {
	w, e := r.FindBySourceID(ctx, id)
	if e != nil {
		return HuangGuoAIListWork{}, e
	}
	row := HuangGuoAIListWork{HuangGuoAIWork: *w, RawTags: w.Tags, Hydrated: true}
	var a model.HuangGuoAIArtwork
	result := r.db.WithContext(ctx).Where("source_id = ?", id).Limit(1).Find(&a)
	if result.Error != nil {
		return row, result.Error
	}
	row.ArtworkID = a.ID
	rows := []HuangGuoAIListWork{row}
	e = hydrateHuangGuoAIList(r.db.WithContext(ctx), rows)
	return rows[0], e
}

func (r *HuangGuoAIRepository) Episodes(ctx context.Context, id string, page, size int) ([]model.HuangGuoAIEpisode, int64, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, 0, errors.New("分页参数无效")
	}
	w, e := r.FindBySourceID(ctx, id)
	if e != nil {
		return nil, 0, e
	}
	rows := []model.HuangGuoAIEpisode{}
	var count int64
	q := r.db.WithContext(ctx).Where("work_id = ?", w.ID)
	if e := q.Model(&model.HuangGuoAIEpisode{}).Count(&count).Error; e != nil {
		return nil, 0, e
	}
	e = q.Order("number").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, count, e
}

func (r *HuangGuoAIRepository) Pending(ctx context.Context, after string, cutoff time.Time) ([]string, error) {
	ids := []string{}
	e := r.db.WithContext(ctx).Table("huangguoai_discoveries d").Where("d.source_id > ? AND d.created_at <= ? AND d.source_category <> ''", after, cutoff).Where("NOT EXISTS (SELECT 1 FROM huangguoai_works w WHERE w.source_id=d.source_id) OR EXISTS (SELECT 1 FROM huangguoai_works w WHERE w.source_id=d.source_id AND w.completed IS DISTINCT FROM TRUE AND w.refreshed_at < ?)", cutoff.Add(-24*time.Hour)).Where("NOT EXISTS (SELECT 1 FROM huangguoai_sync_failures f WHERE f.stage='detail' AND f.source_key=d.source_id AND f.retry_at > ?)", cutoff).Order("d.source_id").Limit(100).Pluck("d.source_id", &ids).Error
	return ids, e
}

func (r *HuangGuoAIRepository) RecordFailure(ctx context.Context, stage, key, code string) error {
	x := model.HuangGuoAISyncFailure{Stage: stage, SourceKey: key, Attempts: 1, RetryAt: time.Now().Add(time.Hour), ErrorCode: code}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "stage"}, {Name: "source_key"}}, DoUpdates: clause.Assignments(map[string]any{"attempts": gorm.Expr("huangguoai_sync_failures.attempts+1"), "retry_at": x.RetryAt, "error_code": code, "updated_at": time.Now()})}).Create(&x).Error
}

// SearchResults 保留官网顺序，批量补充本地资料与图片身份。
func (r *HuangGuoAIRepository) SearchResults(ctx context.Context, remote []huangguoai.Summary) ([]HuangGuoAIListWork, error) {
	rows := []HuangGuoAIListWork{}
	if len(remote) == 0 {
		return rows, nil
	}
	ids := []string{}
	for _, x := range remote {
		ids = append(ids, x.SourceID)
	}
	var works []model.HuangGuoAIWork
	var art []model.HuangGuoAIArtwork
	if err := r.db.WithContext(ctx).Where("source_id=ANY(?)", &ids).Find(&works).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Select("source_id,id").Where("source_id=ANY(?) AND local_key<>''", &ids).Find(&art).Error; err != nil {
		return nil, err
	}
	byID := map[string]model.HuangGuoAIWork{}
	images := map[string]string{}
	for _, x := range works {
		byID[x.SourceID] = x
	}
	for _, x := range art {
		images[x.SourceID] = x.ID
	}
	for _, x := range remote {
		tags, _ := json.Marshal(x.Tags)
		if x.Tags == nil {
			tags = []byte("[]")
		}
		row := HuangGuoAIListWork{HuangGuoAIWork: model.HuangGuoAIWork{SourceID: x.SourceID, SourceCategory: x.Category, Kind: huangguoai.Kind(x.Category), Title: x.Title, Overview: x.Overview, Rating: x.Rating, EpisodeCount: x.EpisodeCount, TotalEpisodes: x.TotalEpisodes, Completed: x.Finished}, RawTags: string(tags), ArtworkID: images[x.SourceID]}
		if w, ok := byID[x.SourceID]; ok {
			row.HuangGuoAIWork = w
			row.RawTags = w.Tags
			row.Hydrated = true
		}
		rows = append(rows, row)
	}
	return rows, hydrateHuangGuoAIList(r.db.WithContext(ctx), rows)
}
