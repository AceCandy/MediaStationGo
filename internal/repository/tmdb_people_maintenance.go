package repository

import (
	"context"
	"time"
)

// TMDbPeopleCandidate 将作品演职员与人物详情合并分页，失败对象不能阻塞后续来源。
type TMDbPeopleCandidate struct {
	ID           string
	Kind         string
	TMDbID       int `gorm:"column:tmdb_id"`
	ParentID     *string
	Number       *int
	NextPeopleAt *time.Time
}

// 已获取空 cast/crew 也是完整证据；只有一侧数组或 null 不能视为完成。
const tmdbCreditsLoadedSQL = `EXISTS (SELECT 1 FROM tmdb_payloads p WHERE p.subject_id=s.id
 AND p.language IN ('zh-CN','und') AND (
 (p.component='credits' AND jsonb_typeof(p.body->'cast')='array' AND jsonb_typeof(p.body->'crew')='array')
 OR (p.component IN ('base','detail') AND jsonb_typeof(p.body->'credits'->'cast')='array' AND jsonb_typeof(p.body->'credits'->'crew')='array')))`

func (r *TMDbCatalogRepository) PeopleMaintenanceCandidates(ctx context.Context, limit int) ([]TMDbPeopleCandidate, error) {
	rows := []TMDbPeopleCandidate{}
	err := r.db.WithContext(ctx).Raw(`SELECT * FROM (
 SELECT s.id,s.kind,s.tmdb_id,s.parent_id,s.number,s.next_people_at FROM tmdb_subjects s
 WHERE s.kind IN ('movie','series','season') AND (s.next_people_at IS NULL OR s.next_people_at<=?) AND NOT `+tmdbCreditsLoadedSQL+`
 UNION ALL
 SELECT p.id,'person',p.tmdb_id,NULL,NULL,p.next_people_at FROM tmdb_person_profiles p
 WHERE (p.next_people_at IS NULL OR p.next_people_at<=?)
 AND NOT EXISTS (SELECT 1 FROM tmdb_payloads payload WHERE payload.person_profile_id=p.id
 AND payload.component IN ('base','detail') AND payload.quality>=2 AND payload.language IN ('zh-CN','und'))
 ) candidates ORDER BY next_people_at NULLS FIRST,id LIMIT ?`, time.Now(), time.Now(), limit).Scan(&rows).Error
	return rows, err
}

func (r *TMDbCatalogRepository) FinishPeopleMaintenance(ctx context.Context, row TMDbPeopleCandidate, next time.Time) error {
	table := "tmdb_subjects"
	if row.Kind == "person" {
		table = "tmdb_person_profiles"
	}
	return r.db.WithContext(ctx).Table(table).Where("id=? AND next_people_at IS NOT DISTINCT FROM ?", row.ID, row.NextPeopleAt).UpdateColumn("next_people_at", next).Error
}

// CreditsLoaded 查询来源组件证据，不能用关系行数判定空列表是否已获取。
func (r *TMDbCatalogRepository) CreditsLoaded(ctx context.Context, id string) (bool, error) {
	var loaded bool
	err := r.db.WithContext(ctx).Raw(`SELECT `+tmdbCreditsLoadedSQL+` FROM tmdb_subjects s WHERE s.id=?`, id).Scan(&loaded).Error
	return loaded, err
}
