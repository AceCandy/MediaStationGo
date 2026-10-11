package database

import "gorm.io/gorm"

// ensureTMDbCatalogSchema 保证来源层级约束及本地复查队列的来源变更通知。
func ensureTMDbCatalogSchema(db *gorm.DB) error {
	statements := []string{
		`DO $$ BEGIN IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='tmdb_subject_hierarchy' AND conrelid='tmdb_subjects'::regclass) THEN ALTER TABLE tmdb_subjects ADD CONSTRAINT tmdb_subject_hierarchy CHECK ((kind IN ('movie','series') AND parent_id IS NULL AND number IS NULL) OR (kind='season' AND parent_id IS NOT NULL AND number IS NOT NULL AND number>=0) OR (kind='episode' AND parent_id IS NOT NULL AND number IS NOT NULL AND number>0)); END IF; END $$`,
		`CREATE INDEX IF NOT EXISTS idx_tmdb_person_image_source ON tmdb_person_profiles(image_source_path) WHERE image_key<>''`,
		`CREATE INDEX IF NOT EXISTS idx_tmdb_artwork_source ON tmdb_artworks(source_path)`,
		`CREATE INDEX IF NOT EXISTS idx_tmdb_subject_list ON tmdb_subjects(kind,updated_at DESC,id)`,
		`CREATE OR REPLACE FUNCTION tmdb_catalog_source_changed() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE local_id text; BEGIN IF NEW.subject_id IS NULL THEN RETURN NULL; END IF; FOR local_id IN SELECT mid.metadata_id FROM tmdb_subjects s JOIN metadata_identifiers mid ON mid.provider='tmdb' AND mid.entity_kind=s.kind AND s.tmdb_id=CASE WHEN mid.external_id ~ '^[1-9][0-9]{0,9}$' THEN mid.external_id::bigint END WHERE s.id=NEW.subject_id LOOP PERFORM tmdb_recheck_related(local_id,false); END LOOP; RETURN NULL; END $$`,
		`DROP TRIGGER IF EXISTS tmdb_catalog_source_changed ON tmdb_payloads`,
		`CREATE TRIGGER tmdb_catalog_source_changed AFTER INSERT ON tmdb_payloads FOR EACH ROW EXECUTE FUNCTION tmdb_catalog_source_changed()`,
	}
	for _, sql := range statements {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	return nil
}
