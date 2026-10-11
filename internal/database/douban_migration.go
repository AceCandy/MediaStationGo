package database

import (
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// migrateDoubanSubjects 按快照自身身份幂等导入豆瓣资料，完整资料优先；旧行保留供回滚。
func migrateDoubanSubjects(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.MetadataProviderSnapshot{}) || !db.Migrator().HasTable(&model.MetadataIdentifier{}) {
		return nil
	}
	afterID, afterQuality := "", 0
	var afterTime time.Time
	for {
		var rows []struct {
			model.MetadataProviderSnapshot
			DoubanID     string
			EntityKind   string
			QualityOrder int
		}
		// 关联仅补足缺失身份；先校验每个候选，再由唯一约束选择质量/时间最优的有效资料。
		err := db.Raw(`WITH candidates AS (
SELECT mps.*,
COALESCE(NULLIF(mps.payload->>'id', ''), NULLIF(mps.payload#>>'{subject,id}', ''),
         NULLIF(mps.payload#>>'{data,id}', ''), binding.external_id) AS raw_id,
COALESCE(
 CASE mps.payload->>'type' WHEN 'tv' THEN 'series' WHEN 'movie' THEN 'movie' END,
 CASE mps.payload#>>'{subject,type}' WHEN 'tv' THEN 'series' WHEN 'movie' THEN 'movie' END,
 CASE mps.payload#>>'{data,type}' WHEN 'tv' THEN 'series' WHEN 'movie' THEN 'movie' END,
 CASE mps.payload->'is_tv' WHEN 'true'::jsonb THEN 'series' WHEN 'false'::jsonb THEN 'movie' END,
 CASE mps.payload#>'{subject,is_tv}' WHEN 'true'::jsonb THEN 'series' WHEN 'false'::jsonb THEN 'movie' END,
 CASE mps.payload#>'{data,is_tv}' WHEN 'true'::jsonb THEN 'series' WHEN 'false'::jsonb THEN 'movie' END) AS payload_kind,
binding.external_id AS binding_id, binding.entity_kind AS binding_kind,
CASE WHEN mps.degraded THEN 1
     WHEN jsonb_exists_any(mps.payload, ARRAY['subject','data']) THEN 2 ELSE 0 END AS quality_order
FROM metadata_provider_snapshots mps
LEFT JOIN LATERAL (
 SELECT MIN(mid.external_id) AS external_id,
        MIN(COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind)) AS entity_kind
 FROM metadata_identifiers mid
 JOIN metadata_items mi ON mi.id = mid.metadata_id AND mi.kind = mid.entity_kind
 WHERE mid.metadata_id = mps.metadata_id AND mid.provider = 'douban' AND mid.external_id <> ''
 AND COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind) IN ('movie', 'series', 'season', 'episode')
 HAVING COUNT(*) = 1
) binding ON true
WHERE mps.provider = 'douban'
), identified AS (
 SELECT candidates.*, LTRIM(raw_id, '0') AS douban_id,
 COALESCE(payload_kind, CASE WHEN LTRIM(raw_id, '0') = LTRIM(binding_id, '0') THEN binding_kind END) AS entity_kind
 FROM candidates WHERE raw_id ~ '^[0-9]+$'
)
SELECT * FROM identified
WHERE douban_id <> '' AND LENGTH(douban_id) <= 32
AND entity_kind IN ('movie', 'series', 'season', 'episode')
AND NOT EXISTS (SELECT 1 FROM douban_subjects ds WHERE ds.douban_id = identified.douban_id AND ds.entity_kind = identified.entity_kind)
AND (? = '' OR quality_order > ? OR (quality_order = ? AND fetched_at < ?)
     OR (quality_order = ? AND fetched_at = ? AND id > ?))
ORDER BY quality_order, fetched_at DESC, id
LIMIT 1000`, afterID, afterQuality, afterQuality, afterTime, afterQuality, afterTime, afterID).Scan(&rows).Error
		if err != nil {
			return err
		}
		for _, row := range rows {
			afterID, afterQuality, afterTime = row.ID, row.QualityOrder, row.FetchedAt
			if !model.DoubanPayloadMatchesIdentity([]byte(row.Payload), row.DoubanID, row.EntityKind) {
				continue
			}
			payload := row.Payload
			subject := model.DoubanSubject{DoubanID: row.DoubanID, EntityKind: row.EntityKind, Payload: &payload,
				Quality: model.DoubanPayloadQuality([]byte(payload), row.Degraded), FetchedAt: &row.FetchedAt,
				Rating: model.DoubanRating(payload)}
			if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "douban_id"}, {Name: "entity_kind"}}, DoNothing: true}).Create(&subject).Error; err != nil {
				return err
			}
		}
		if len(rows) < 1000 {
			return nil
		}
	}
}

// migrateDoubanRefreshTasks 保留执行记录与日志键，仅将旧资料刷新归入公共任务。
func migrateDoubanRefreshTasks(db *gorm.DB) error {
	return db.Model(&model.TaskExecution{}).
		Where("kind = ? AND name = ?", "artwork", "豆瓣资料刷新").
		UpdateColumns(map[string]any{"kind": "douban_data_refresh", "system": model.TaskSystemCommon}).Error
}
