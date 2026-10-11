package repository

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *TMDbCatalogRepository) Subject(ctx context.Context, id string) (*model.TMDbSubject, error) {
	var row model.TMDbSubject
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return &row, err
}
func (r *TMDbCatalogRepository) SubjectIdentity(ctx context.Context, kind string, id int) (*model.TMDbSubject, error) {
	var row model.TMDbSubject
	err := r.db.WithContext(ctx).Where("kind = ? AND tmdb_id = ?", kind, id).First(&row).Error
	return &row, err
}
func (r *TMDbCatalogRepository) Person(ctx context.Context, id string) (*model.TMDbPersonProfile, error) {
	var row model.TMDbPersonProfile
	err := r.db.WithContext(ctx).Where("person_id = ?", id).First(&row).Error
	return &row, err
}
func (r *TMDbCatalogRepository) PersonIdentity(ctx context.Context, id int) (*model.TMDbPersonProfile, error) {
	var row model.TMDbPersonProfile
	err := r.db.WithContext(ctx).Where("tmdb_id = ?", id).First(&row).Error
	return &row, err
}

func (r *TMDbCatalogRepository) Subjects(ctx context.Context, kind, parent, keyword string, quality, page, limit int, states ...string) ([]model.TMDbSubject, int64, error) {
	query := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.TMDbSubject{})
		if len(states) > 0 {
			q = tmdbStateFilter(q, "tmdb_subjects.id", "subject_id", states[0])
		}
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		if parent != "" {
			q = q.Where("parent_id = ?", parent)
		}
		if keyword != "" {
			q = q.Where("title ILIKE ? OR original_name ILIKE ? OR tmdb_id::text = ?", "%"+keyword+"%", "%"+keyword+"%", keyword)
		}
		if quality >= 0 {
			q = q.Where("quality = ?", quality)
		}
		return q
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.TMDbSubject{}
	q := query()
	if parent != "" {
		q = q.Order("number,id")
	} else {
		q = q.Order("updated_at DESC,id")
	}
	err := q.Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, total, err
}
func (r *TMDbCatalogRepository) People(ctx context.Context, keyword string, page, limit int, filters ...string) ([]model.TMDbPersonProfile, int64, error) {
	query := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.TMDbPersonProfile{})
		if len(filters) == 2 {
			if filters[0] != "" {
				q = q.Where("quality=?", filters[0])
			}
			q = tmdbStateFilter(q, "tmdb_person_profiles.id", "person_profile_id", filters[1])
		}
		if keyword != "" {
			q = q.Where("name ILIKE ? OR original_name ILIKE ? OR tmdb_id::text = ?", "%"+keyword+"%", "%"+keyword+"%", keyword)
		}
		return q
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.TMDbPersonProfile{}
	err := query().Order("updated_at DESC,id").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, total, err
}
func (r *TMDbCatalogRepository) Payloads(ctx context.Context, id string, person bool) ([]model.TMDbPayload, error) {
	rows := []model.TMDbPayload{}
	col := "subject_id"
	if person {
		col = "person_profile_id"
	}
	err := r.db.WithContext(ctx).Where(col+" = ?", id).Order("fetched_at DESC,id").Find(&rows).Error
	return rows, err
}
func (r *TMDbCatalogRepository) Jobs(ctx context.Context, id string, person bool) ([]model.TMDbRefreshJob, error) {
	rows := []model.TMDbRefreshJob{}
	col := "subject_id"
	if person {
		col = "person_profile_id"
	}
	err := r.db.WithContext(ctx).Where(col+" = ?", id).Order("scope,language").Find(&rows).Error
	return rows, err
}
func (r *TMDbCatalogRepository) Credits(ctx context.Context, id string, person bool, page, limit int) ([]model.TMDbCredit, int64, error) {
	col := "subject_id"
	if person {
		col = "person_profile_id"
	}
	query := func() *gorm.DB { return r.db.WithContext(ctx).Model(&model.TMDbCredit{}).Where(col+" = ?", id) }
	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.TMDbCredit{}
	err := query().Preload("PersonProfile").Preload("Subject").Order("origin DESC, sort_order,id").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, total, err
}
func (r *TMDbCatalogRepository) Cached(ctx context.Context, req TMDbSourceRequest, force bool) ([]byte, error) {
	if force {
		return nil, nil
	}
	if req.QueryResult {
		var row model.TMDbQueryCache
		err := r.db.WithContext(ctx).Where("key = ? AND expires_at > ?", req.Key, time.Now()).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return []byte(row.Body), err
	}
	var row struct{ Body string }
	err := r.db.WithContext(ctx).Raw(`SELECT p.body FROM tmdb_refresh_jobs j JOIN tmdb_payloads p ON p.id=j.payload_id WHERE j.key=? AND j.next_refresh_at>?`, req.Key, time.Now()).Scan(&row).Error
	if err != nil || row.Body != "" {
		return []byte(row.Body), err
	}
	// 详情响应包含基础字段；基础调用可以复用相同身份与语言的新鲜详情。
	if req.Component == "base" {
		err = r.db.WithContext(ctx).Raw(`SELECT p.body FROM tmdb_refresh_jobs j JOIN tmdb_payloads p ON p.id=j.payload_id WHERE j.path=? AND j.language=? AND j.config_key=? AND j.scope='detail' AND j.last_success_at>? ORDER BY j.last_success_at DESC LIMIT 1`, req.Path, req.Language, req.ConfigKey, time.Now().Add(-24*time.Hour)).Scan(&row).Error
	}
	return []byte(row.Body), err
}
func (r *TMDbCatalogRepository) Job(ctx context.Context, key string) (*model.TMDbRefreshJob, error) {
	var row model.TMDbRefreshJob
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	return &row, err
}

var ErrTMDbQueueLimit = errors.New("TMDb pending request limit reached")

func (r *TMDbCatalogRepository) Enqueue(ctx context.Context, req TMDbSourceRequest, user string, force bool) (*model.TMDbRefreshJob, error) {
	if user == "" {
		return r.enqueue(ctx, req, user, force)
	}
	var result *model.TMDbRefreshJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "tmdb_ensure:"+user).Error; e != nil {
			return e
		}
		repo := &TMDbCatalogRepository{db: tx}
		old, e := repo.Job(ctx, req.Key)
		if e == nil && (old.Status == "pending" || old.Status == "running") {
			result = old
			return nil
		}
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		n, e := repo.PendingForUser(ctx, user)
		if e != nil {
			return e
		}
		if n >= 50 {
			return ErrTMDbQueueLimit
		}
		result, e = repo.enqueue(ctx, req, user, force)
		return e
	})
	return result, err
}
func (r *TMDbCatalogRepository) enqueue(ctx context.Context, req TMDbSourceRequest, user string, force bool) (*model.TMDbRefreshJob, error) {
	j := model.TMDbRefreshJob{Key: req.Key, ConfigKey: req.ConfigKey, Path: req.Path, Query: req.Query, Scope: req.Component, Language: req.Language, Status: "pending", RequestedBy: user, ExplicitPending: !req.Automatic}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).Create(&j).Error
	if err != nil {
		return nil, err
	}
	q := r.db.WithContext(ctx).Model(&model.TMDbRefreshJob{}).Where("key = ? AND status <> 'running' AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", req.Key, time.Now())
	if !force {
		q = q.Where("next_refresh_at IS NULL OR next_refresh_at <= ?", time.Now())
	}
	if err = q.Updates(map[string]any{"status": "pending", "explicit_pending": gorm.Expr("explicit_pending OR ?", !req.Automatic), "requested_by": gorm.Expr("CASE WHEN requested_by = '' THEN ? ELSE requested_by END", user)}).Error; err != nil {
		return nil, err
	}
	return r.Job(ctx, req.Key)
}
func (r *TMDbCatalogRepository) Claim(ctx context.Context, key string) (*model.TMDbRefreshJob, error) {
	var row model.TMDbRefreshJob
	now := time.Now()
	until := now.Add(5 * time.Minute)
	token := uuid.NewString()
	res := r.db.WithContext(ctx).Model(&row).Clauses(clause.Returning{}).Where("key = ? AND (lease_until IS NULL OR lease_until < ?) AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", key, now, now).Updates(map[string]any{"token": token, "status": "running", "lease_until": until})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &row, nil
}
func (r *TMDbCatalogRepository) Fail(ctx context.Context, j *model.TMDbRefreshJob, errText string, delay time.Duration, cooldown bool) error {
	at := time.Now().Add(delay)
	updates := map[string]any{"status": "error", "token": "", "lease_until": nil, "last_error": errText, "attempts": gorm.Expr("attempts + 1"), "next_attempt_at": at}
	if cooldown {
		updates["cooldown_until"] = at
	}
	return r.db.WithContext(ctx).Model(&model.TMDbRefreshJob{}).Where("id = ? AND token = ?", j.ID, j.Token).Updates(updates).Error
}
func (r *TMDbCatalogRepository) Release(ctx context.Context, j *model.TMDbRefreshJob) error {
	return r.db.WithContext(ctx).Model(&model.TMDbRefreshJob{}).Where("id = ? AND token = ?", j.ID, j.Token).Updates(map[string]any{"status": "pending", "token": "", "lease_until": nil}).Error
}
func (r *TMDbCatalogRepository) Cooldown(ctx context.Context, key string) (*time.Time, error) {
	var row struct{ Until *time.Time }
	err := r.db.WithContext(ctx).Model(&model.TMDbRefreshJob{}).Select("MAX(cooldown_until) AS until").Where("config_key = ? AND cooldown_until > ?", key, time.Now()).Scan(&row).Error
	return row.Until, err
}
func (r *TMDbCatalogRepository) Due(ctx context.Context, periodic bool, limit int) ([]model.TMDbRefreshJob, error) {
	q := r.db.WithContext(ctx).Where("(lease_until IS NULL OR lease_until < ?) AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", time.Now(), time.Now())
	if periodic {
		q = q.Where("explicit_pending OR (path NOT LIKE '%/episode/%' AND (status IN ('pending','error','running') OR next_refresh_at <= ?))", time.Now())
	} else {
		q = q.Where("explicit_pending AND status IN ('pending','error','running')")
	}
	rows := []model.TMDbRefreshJob{}
	err := q.Order("next_attempt_at NULLS FIRST,next_refresh_at NULLS FIRST,id").Limit(limit).Find(&rows).Error
	return rows, err
}
func (r *TMDbCatalogRepository) PendingForUser(ctx context.Context, id string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.TMDbRefreshJob{}).Where("requested_by = ? AND status IN ('pending','running')", id).Count(&n).Error
	return n, err
}
func (r *TMDbCatalogRepository) Artwork(ctx context.Context, id, typ string) (*model.TMDbArtwork, error) {
	var row model.TMDbArtwork
	err := r.db.WithContext(ctx).Preload("Asset").Where("subject_id = ? AND type = ?", id, typ).First(&row).Error
	return &row, err
}
func (r *TMDbCatalogRepository) SaveArtwork(ctx context.Context, row *model.TMDbArtwork, assetID *string, delay bool) error {
	u := map[string]any{"asset_id": assetID, "downloaded_path": row.SourcePath, "status": "ready", "retry_at": nil}
	if delay {
		u = map[string]any{"status": "error", "retry_at": time.Now().Add(time.Hour)}
	}
	return r.db.WithContext(ctx).Model(&model.TMDbArtwork{}).Where("id = ? AND source_path = ?", row.ID, row.SourcePath).Updates(u).Error
}
func (r *TMDbCatalogRepository) SavePersonImage(ctx context.Context, row *model.TMDbPersonProfile, key string, failed bool) error {
	u := map[string]any{"image_key": key, "image_source_path": row.ProfilePath, "image_status": "ready", "image_retry_at": nil}
	if failed {
		u = map[string]any{"image_status": "error", "image_retry_at": time.Now().Add(time.Hour)}
	}
	return r.db.WithContext(ctx).Model(&model.TMDbPersonProfile{}).Where("id = ? AND profile_path = ?", row.ID, row.ProfilePath).Updates(u).Error
}

func (r *TMDbCatalogRepository) AttachJob(ctx context.Context, id, owner string, person bool) error {
	col := "subject_id"
	if person {
		col = "person_profile_id"
	}
	return r.db.WithContext(ctx).Model(&model.TMDbRefreshJob{}).Where("id=?", id).Update(col, owner).Error
}

func (r *TMDbCatalogRepository) ArtworkSource(ctx context.Context, path string) (*model.TMDbArtwork, error) {
	var a model.TMDbArtwork
	err := r.db.WithContext(ctx).Where("source_path=?", path).Order("asset_id NULLS LAST,id").First(&a).Error
	return &a, err
}

// PersonWorks 先按作品分页，再读取该页全部角色，避免多角色跨页或丢失。
func (r *TMDbCatalogRepository) PersonWorks(ctx context.Context, id, kind, category, order string, page, limit int) ([]model.TMDbCredit, int64, error) {
	query := func() *gorm.DB {
		q := r.db.WithContext(ctx).Table("tmdb_subjects s").Where("EXISTS (SELECT 1 FROM tmdb_credits c WHERE c.subject_id=s.id AND c.person_profile_id=? AND (?='' OR c.category=?))", id, category, category)
		if kind != "" {
			q = q.Where("s.kind=?", kind)
		}
		return q
	}
	var total int64
	if e := query().Count(&total).Error; e != nil {
		return nil, 0, e
	}
	var ids []string
	sort := "s.release_date DESC NULLS LAST,s.id"
	if order == "oldest" {
		sort = "s.release_date ASC NULLS LAST,s.id"
	}
	if e := query().Order(sort).Offset((page-1)*limit).Limit(limit).Pluck("s.id", &ids).Error; e != nil {
		return nil, 0, e
	}
	rows := []model.TMDbCredit{}
	if len(ids) == 0 {
		return rows, total, nil
	}
	e := r.db.WithContext(ctx).Preload("PersonProfile").Preload("Subject").Joins("JOIN tmdb_subjects s ON s.id=tmdb_credits.subject_id").Where("person_profile_id=? AND subject_id IN ? AND (?='' OR category=?)", id, ids, category, category).Order(sort + ",origin DESC,sort_order,tmdb_credits.id").Find(&rows).Error
	return rows, total, e
}

func tmdbStateFilter(q *gorm.DB, owner, column, state string) *gorm.DB {
	switch state {
	case "pending":
		return q.Where("EXISTS(SELECT 1 FROM tmdb_refresh_jobs j WHERE j." + column + "=" + owner + " AND j.status IN ('pending','running'))")
	case "error":
		return q.Where("EXISTS(SELECT 1 FROM tmdb_refresh_jobs j WHERE j." + column + "=" + owner + " AND j.status='error')")
	}
	return q
}

// RehomeJob 将旧 API 配置的到期待办交接给当前配置，保留原始来源而停止旧槽反复调度。
func (r *TMDbCatalogRepository) RehomeJob(ctx context.Context, old *model.TMDbRefreshJob, req TMDbSourceRequest) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TMDbRefreshJob{}).Where("id=? AND (lease_until IS NULL OR lease_until<?)", old.ID, time.Now()).Updates(map[string]any{"status": "retired", "explicit_pending": false, "requested_by": "", "next_refresh_at": nil, "next_attempt_at": nil, "token": "", "lease_until": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("TMDb job claimed during configuration change")
		}
		repo := &TMDbCatalogRepository{db: tx}
		j, e := repo.enqueue(ctx, req, old.RequestedBy, false)
		if e != nil {
			return e
		}
		return tx.Model(&model.TMDbRefreshJob{}).Where("id=? AND subject_id IS NULL AND person_profile_id IS NULL", j.ID).Updates(map[string]any{"subject_id": old.SubjectID, "person_profile_id": old.PersonProfileID}).Error
	})
}

func (r *TMDbCatalogRepository) PersonImageSource(ctx context.Context, sourcePath string) (string, error) {
	var row struct{ ImageKey string }
	err := r.db.WithContext(ctx).Model(&model.TMDbPersonProfile{}).Select("image_key").Where("image_source_path=? AND image_key<>''", sourcePath).Order("updated_at DESC,id").Limit(1).Scan(&row).Error
	return row.ImageKey, err
}
