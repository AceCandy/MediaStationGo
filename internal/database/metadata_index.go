package database

import "gorm.io/gorm"

// EnsureMetadataIndexTriggers 在来源和关联事务内登记待办，不访问网络或写展示字段。
func EnsureMetadataIndexTriggers(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range []string{
			`CREATE OR REPLACE FUNCTION metadata_index_enqueue(target text) RETURNS void LANGUAGE plpgsql AS $$ BEGIN
 IF target IS NULL OR target='' THEN RETURN; END IF;
 INSERT INTO metadata_index_jobs(metadata_id,revision,next_at) VALUES(target,1,now())
 ON CONFLICT(metadata_id) DO UPDATE SET revision=metadata_index_jobs.revision+1,next_at=now(); END $$`,
			`CREATE OR REPLACE FUNCTION metadata_index_source(provider_name text, source_kind text, external text) RETURNS void LANGUAGE plpgsql AS $$ BEGIN
 INSERT INTO metadata_index_jobs(metadata_id,revision,next_at)
 SELECT DISTINCT metadata_id,1,now() FROM metadata_identifiers
 WHERE provider=provider_name AND external_id=external
 AND CASE WHEN provider_name='douban' THEN COALESCE(NULLIF(douban_entity_kind,''),entity_kind) ELSE entity_kind END=source_kind
 ORDER BY metadata_id
 ON CONFLICT(metadata_id) DO UPDATE SET revision=metadata_index_jobs.revision+1,next_at=now(); END $$`,
			`CREATE OR REPLACE FUNCTION metadata_index_source_change() RETURNS trigger LANGUAGE plpgsql AS $$
 DECLARE subject record; changed record; BEGIN
 IF TG_OP='DELETE' THEN changed:=OLD; ELSE changed:=NEW; END IF;
 IF TG_TABLE_NAME='tmdb_subjects' THEN
 IF TG_OP='UPDATE' AND NEW.title IS NOT DISTINCT FROM OLD.title AND NEW.original_name IS NOT DISTINCT FROM OLD.original_name AND NEW.overview IS NOT DISTINCT FROM OLD.overview AND NEW.fields IS NOT DISTINCT FROM OLD.fields AND NEW.release_date IS NOT DISTINCT FROM OLD.release_date AND NEW.rating IS NOT DISTINCT FROM OLD.rating THEN RETURN NEW; END IF;
 PERFORM metadata_index_source('tmdb',changed.kind,changed.tmdb_id::text);
 ELSIF TG_TABLE_NAME IN ('tmdb_artworks','tmdb_credits') THEN
 IF TG_TABLE_NAME='tmdb_artworks' AND TG_OP='UPDATE' THEN
 IF NEW.asset_id IS NOT DISTINCT FROM OLD.asset_id THEN RETURN NEW; END IF; END IF;
 SELECT kind,tmdb_id INTO subject FROM tmdb_subjects WHERE id=changed.subject_id;
 PERFORM metadata_index_source('tmdb',subject.kind,subject.tmdb_id::text);
 ELSE
 IF TG_OP='UPDATE' AND NEW.payload IS NOT DISTINCT FROM OLD.payload AND NEW.summary IS NOT DISTINCT FROM OLD.summary AND NEW.rating IS NOT DISTINCT FROM OLD.rating AND NEW.poster_asset_id IS NOT DISTINCT FROM OLD.poster_asset_id THEN RETURN NEW; END IF;
 PERFORM metadata_index_source('douban',changed.entity_kind,changed.douban_id); END IF;
 RETURN NEW; END $$`,
			`CREATE OR REPLACE FUNCTION metadata_index_person_change() RETURNS trigger LANGUAGE plpgsql AS $$
 DECLARE changed record; subject record; BEGIN
 IF TG_OP='DELETE' THEN changed:=OLD; ELSE changed:=NEW; END IF;
 IF TG_TABLE_NAME='tmdb_person_profiles' THEN
 IF TG_OP='UPDATE' AND NEW.name IS NOT DISTINCT FROM OLD.name AND NEW.original_name IS NOT DISTINCT FROM OLD.original_name AND NEW.biography IS NOT DISTINCT FROM OLD.biography THEN RETURN NEW; END IF;
 FOR subject IN SELECT DISTINCT s.kind,s.tmdb_id FROM tmdb_credits c JOIN tmdb_subjects s ON s.id=c.subject_id
 WHERE c.person_profile_id=changed.id AND c.origin='subject' ORDER BY s.kind,s.tmdb_id LOOP
 PERFORM metadata_index_source('tmdb',subject.kind,subject.tmdb_id::text); END LOOP;
 ELSIF changed.kind='person_name' THEN
 FOR subject IN SELECT DISTINCT s.kind,s.tmdb_id FROM tmdb_person_profiles p
 JOIN tmdb_credits c ON c.person_profile_id=p.id AND c.origin='subject'
 JOIN tmdb_subjects s ON s.id=c.subject_id WHERE p.person_id=changed.context_key ORDER BY s.kind,s.tmdb_id LOOP
 PERFORM metadata_index_source('tmdb',subject.kind,subject.tmdb_id::text); END LOOP;
 ELSIF changed.kind='role' THEN
 SELECT kind,tmdb_id INTO subject FROM tmdb_subjects WHERE id=changed.context_key;
 PERFORM metadata_index_source('tmdb',subject.kind,subject.tmdb_id::text);
 END IF; RETURN NULL; END $$`,
			`DROP TRIGGER IF EXISTS metadata_index_person ON tmdb_person_profiles`,
			`CREATE TRIGGER metadata_index_person AFTER INSERT OR UPDATE OR DELETE ON tmdb_person_profiles FOR EACH ROW EXECUTE FUNCTION metadata_index_person_change()`,
			`DROP TRIGGER IF EXISTS metadata_index_translation ON translation_caches`,
			`CREATE TRIGGER metadata_index_translation AFTER INSERT OR UPDATE OR DELETE ON translation_caches FOR EACH ROW EXECUTE FUNCTION metadata_index_person_change()`,
			`CREATE OR REPLACE FUNCTION metadata_index_binding_change() RETURNS trigger LANGUAGE plpgsql AS $$
 DECLARE parent text; grandparent text; target text; targets text[]; BEGIN
 IF TG_TABLE_NAME='media' AND TG_OP='UPDATE' AND NEW.metadata_id IS NOT DISTINCT FROM OLD.metadata_id THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' THEN targets:=ARRAY[NEW.metadata_id];
 ELSIF TG_OP='DELETE' THEN targets:=ARRAY[OLD.metadata_id];
 ELSE targets:=ARRAY[OLD.metadata_id,NEW.metadata_id]; END IF;
 FOR target IN SELECT DISTINCT unnest(targets) ORDER BY 1 LOOP
 PERFORM metadata_index_enqueue(target);
 SELECT parent_id INTO parent FROM metadata_items WHERE id=target;
 PERFORM metadata_index_enqueue(parent);
 SELECT parent_id INTO grandparent FROM metadata_items WHERE id=parent;
 PERFORM metadata_index_enqueue(grandparent);
 END LOOP; RETURN NULL; END $$`,
			`CREATE OR REPLACE FUNCTION douban_image_pending_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.payload IS DISTINCT FROM OLD.payload OR NEW.summary IS DISTINCT FROM OLD.summary THEN NEW.poster_checked_at=NULL; NEW.poster_retry_at=NULL; END IF;
 RETURN NEW; END $$`,
			`DROP TRIGGER IF EXISTS douban_image_pending ON douban_subjects`,
			`CREATE TRIGGER douban_image_pending BEFORE UPDATE ON douban_subjects FOR EACH ROW EXECUTE FUNCTION douban_image_pending_change()`,
			`DROP TRIGGER IF EXISTS metadata_index_subject ON tmdb_subjects`,
			`CREATE TRIGGER metadata_index_subject AFTER INSERT OR UPDATE OR DELETE ON tmdb_subjects FOR EACH ROW EXECUTE FUNCTION metadata_index_source_change()`,
			`DROP TRIGGER IF EXISTS metadata_index_artwork ON tmdb_artworks`,
			`CREATE TRIGGER metadata_index_artwork AFTER INSERT OR UPDATE OR DELETE ON tmdb_artworks FOR EACH ROW EXECUTE FUNCTION metadata_index_source_change()`,
			`DROP TRIGGER IF EXISTS metadata_index_credit ON tmdb_credits`,
			`CREATE TRIGGER metadata_index_credit AFTER INSERT OR UPDATE OR DELETE ON tmdb_credits FOR EACH ROW EXECUTE FUNCTION metadata_index_source_change()`,
			`DROP TRIGGER IF EXISTS metadata_index_douban ON douban_subjects`,
			`CREATE TRIGGER metadata_index_douban AFTER INSERT OR UPDATE OR DELETE ON douban_subjects FOR EACH ROW EXECUTE FUNCTION metadata_index_source_change()`,
			`DROP TRIGGER IF EXISTS metadata_index_identifier ON metadata_identifiers`,
			`CREATE TRIGGER metadata_index_identifier AFTER INSERT OR UPDATE OR DELETE ON metadata_identifiers FOR EACH ROW EXECUTE FUNCTION metadata_index_binding_change()`,
			`DROP TRIGGER IF EXISTS metadata_index_media ON media`,
			`CREATE TRIGGER metadata_index_media AFTER INSERT OR UPDATE OR DELETE ON media FOR EACH ROW EXECUTE FUNCTION metadata_index_binding_change()`,
		} {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
