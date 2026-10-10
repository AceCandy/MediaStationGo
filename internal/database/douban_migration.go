package database

import (
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// migrateDoubanSubjects 幂等导入旧豆瓣快照，完整资料优先；旧行保留供回滚。
func migrateDoubanSubjects(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.MetadataProviderSnapshot{}) || !db.Migrator().HasTable(&model.MetadataIdentifier{}) {
		return nil
	}
	afterID, afterKind := "", ""
	for {
		var rows []struct {
			model.MetadataProviderSnapshot
			DoubanID   string
			EntityKind string
		}
		err := db.Raw(`SELECT DISTINCT ON (mid.external_id, COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind))
mps.*, mid.external_id AS douban_id,
COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind) AS entity_kind
FROM metadata_provider_snapshots mps
JOIN metadata_identifiers mid ON mid.metadata_id = mps.metadata_id AND mid.provider = 'douban'
JOIN metadata_items mi ON mi.id = mid.metadata_id AND mi.kind = mid.entity_kind
WHERE mps.provider = 'douban' AND mid.external_id <> ''
AND NOT EXISTS (SELECT 1 FROM douban_subjects ds WHERE ds.douban_id = mid.external_id AND ds.entity_kind = COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind))
AND COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind) IN ('movie', 'series', 'season', 'episode')
AND (mid.external_id, COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind)) > (?, ?)
ORDER BY mid.external_id, COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind),
CASE WHEN NOT mps.degraded AND NOT (mps.payload ?| ARRAY['subject','data']) THEN 0 WHEN mps.degraded THEN 1 ELSE 2 END,
mps.fetched_at DESC, mps.id
LIMIT 1000`, afterID, afterKind).Scan(&rows).Error
		if err != nil {
			return err
		}
		for _, row := range rows {
			afterID, afterKind = row.DoubanID, row.EntityKind
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
