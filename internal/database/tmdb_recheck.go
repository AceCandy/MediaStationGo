package database

import "gorm.io/gorm"

// EnsureTMDbRecheckTriggers 只登记变更，不在业务写事务中展开后代或请求上游。
func EnsureTMDbRecheckTriggers(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, sql := range tmdbRecheckTriggerSQL {
			if err := tx.Exec(sql).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

var tmdbRecheckTriggerSQL = []string{
	// 领取按 ID 排序，部分索引必须同序，避免反复跳过已处理的主键前缀。
	`CREATE INDEX IF NOT EXISTS idx_tmdb_recheck_changes_pending_id ON tm_db_recheck_changes(metadata_id) WHERE pending`,
	`CREATE OR REPLACE FUNCTION tmdb_recheck_mark(target text, descendants boolean) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
 IF target IS NULL OR target = '' THEN RETURN; END IF;
 INSERT INTO tm_db_recheck_changes(metadata_id, revision, pending, expand, cursor)
 VALUES(target, 1, true, descendants, '')
 ON CONFLICT(metadata_id) DO UPDATE SET revision = tm_db_recheck_changes.revision + 1,
 pending = true, expand = tm_db_recheck_changes.expand OR EXCLUDED.expand,
 cursor = CASE WHEN EXCLUDED.expand THEN '' ELSE tm_db_recheck_changes.cursor END;
END $$`,
	`CREATE OR REPLACE FUNCTION tmdb_recheck_related(target text, descendants boolean) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE k text;
BEGIN
 SELECT kind INTO k FROM metadata_items WHERE id = target;
 IF k = 'movie' OR target IS NULL OR target = '' THEN RETURN; END IF;
 PERFORM tmdb_recheck_mark(target, COALESCE(descendants AND k IN ('series','season'),false));
END $$`,
	`CREATE OR REPLACE FUNCTION tmdb_recheck_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE before_row jsonb; after_row jsonb; target text; changed_subject_id text;
BEGIN
 IF TG_TABLE_NAME = 'media' THEN
  IF TG_OP = 'UPDATE' AND OLD.metadata_id IS NOT DISTINCT FROM NEW.metadata_id THEN RETURN NULL; END IF;
  IF TG_OP <> 'INSERT' THEN
   PERFORM tmdb_recheck_mark((SELECT parent_id FROM metadata_items WHERE id=OLD.metadata_id AND kind='episode'),false);
   PERFORM tmdb_recheck_related(OLD.metadata_id,false);
  END IF;
  IF TG_OP <> 'DELETE' THEN
   PERFORM tmdb_recheck_mark((SELECT parent_id FROM metadata_items WHERE id=NEW.metadata_id AND kind='episode'),false);
   PERFORM tmdb_recheck_related(NEW.metadata_id,false);
  END IF;
  RETURN NULL;
 END IF;
 IF TG_OP <> 'INSERT' THEN before_row := to_jsonb(OLD); END IF;
 IF TG_OP <> 'DELETE' THEN after_row := to_jsonb(NEW); END IF;
 IF TG_TABLE_NAME = 'metadata_items' THEN
  IF TG_OP = 'INSERT' THEN RETURN NULL; END IF;
  IF TG_OP = 'UPDATE' AND
   (before_row->'parent_id', before_row->'kind', before_row->'season_num', before_row->'episode_num')
   IS NOT DISTINCT FROM
   (after_row->'parent_id', after_row->'kind', after_row->'season_num', after_row->'episode_num') THEN RETURN NULL; END IF;
  PERFORM tmdb_recheck_related(COALESCE(after_row->>'id', before_row->>'id'),
    TG_OP='DELETE' OR (before_row->'parent_id',before_row->'kind',before_row->'season_num',before_row->'episode_num')
    IS DISTINCT FROM (after_row->'parent_id',after_row->'kind',after_row->'season_num',after_row->'episode_num'));
  IF (before_row->'parent_id',before_row->'kind') IS DISTINCT FROM (after_row->'parent_id',after_row->'kind') THEN
   IF before_row->>'kind' = 'episode' THEN PERFORM tmdb_recheck_mark(before_row->>'parent_id', false); END IF;
   IF after_row->>'kind' = 'episode' THEN PERFORM tmdb_recheck_mark(after_row->>'parent_id', false); END IF;
  END IF;
 ELSIF TG_TABLE_NAME = 'metadata_identifiers' THEN
  IF TG_OP = 'UPDATE' AND (before_row->'metadata_id',before_row->'provider',before_row->'entity_kind',before_row->'external_id')
   IS NOT DISTINCT FROM (after_row->'metadata_id',after_row->'provider',after_row->'entity_kind',after_row->'external_id') THEN RETURN NULL; END IF;
  IF before_row->>'provider' = 'tmdb' THEN PERFORM tmdb_recheck_related(before_row->>'metadata_id', true); END IF;
  IF after_row->>'provider' = 'tmdb' THEN PERFORM tmdb_recheck_related(after_row->>'metadata_id', true); END IF;
 ELSIF TG_TABLE_NAME IN ('tmdb_subjects','tmdb_artworks') THEN
  IF TG_TABLE_NAME='tmdb_subjects' THEN
   IF TG_OP='UPDATE' AND (before_row->'overview',before_row->'release_date') IS NOT DISTINCT FROM (after_row->'overview',after_row->'release_date') THEN RETURN NULL; END IF;
   changed_subject_id := COALESCE(after_row->>'id',before_row->>'id');
  ELSE
   IF TG_OP='UPDATE' AND (before_row->'asset_id',before_row->'source_path') IS NOT DISTINCT FROM (after_row->'asset_id',after_row->'source_path') THEN RETURN NULL; END IF;
   changed_subject_id := COALESCE(after_row->>'subject_id',before_row->>'subject_id');
  END IF;
  FOR target IN SELECT binding.metadata_id FROM metadata_identifiers binding
   LEFT JOIN tmdb_subjects subject ON subject.kind=binding.entity_kind AND subject.tmdb_id::text=binding.external_id
   WHERE binding.provider='tmdb' AND (subject.id=changed_subject_id OR
    (TG_TABLE_NAME='tmdb_subjects' AND binding.entity_kind=before_row->>'kind' AND binding.external_id=before_row->>'tmdb_id')) ORDER BY binding.metadata_id LOOP
   PERFORM tmdb_recheck_related(target,false);
  END LOOP;
 ELSIF TG_TABLE_NAME = 'artwork_assets' THEN
  INSERT INTO tm_db_recheck_asset_changes(asset_id,cursor) VALUES(COALESCE(after_row->>'id',before_row->>'id'),'')
  ON CONFLICT(asset_id) DO UPDATE SET cursor='';
 END IF;
 RETURN NULL;
END $$`,
	`DROP TRIGGER IF EXISTS tmdb_recheck_media ON media`,
	`CREATE TRIGGER tmdb_recheck_media AFTER INSERT OR UPDATE OR DELETE ON media FOR EACH ROW EXECUTE FUNCTION tmdb_recheck_changed()`,
	`DROP TRIGGER IF EXISTS tmdb_recheck_metadata ON metadata_items`,
	`CREATE TRIGGER tmdb_recheck_metadata AFTER UPDATE OR DELETE ON metadata_items FOR EACH ROW EXECUTE FUNCTION tmdb_recheck_changed()`,
	`DROP TRIGGER IF EXISTS tmdb_recheck_identifiers ON metadata_identifiers`,
	`CREATE TRIGGER tmdb_recheck_identifiers AFTER INSERT OR UPDATE OR DELETE ON metadata_identifiers FOR EACH ROW EXECUTE FUNCTION tmdb_recheck_changed()`,
	`DROP TRIGGER IF EXISTS tmdb_recheck_artworks ON tmdb_artworks`,
	`CREATE TRIGGER tmdb_recheck_artworks AFTER INSERT OR UPDATE OR DELETE ON tmdb_artworks FOR EACH ROW EXECUTE FUNCTION tmdb_recheck_changed()`,
	`DROP TRIGGER IF EXISTS tmdb_recheck_subject ON tmdb_subjects`,
	`CREATE TRIGGER tmdb_recheck_subject AFTER UPDATE OR DELETE ON tmdb_subjects FOR EACH ROW EXECUTE FUNCTION tmdb_recheck_changed()`,
	`DROP TRIGGER IF EXISTS tmdb_recheck_assets ON artwork_assets`,
	`CREATE TRIGGER tmdb_recheck_assets AFTER INSERT OR DELETE ON artwork_assets FOR EACH ROW EXECUTE FUNCTION tmdb_recheck_changed()`,
}
