package database

import "gorm.io/gorm"

// EnsureMetadataPresentationSchema 以本地来源生成只读展示，不回退到人工或旧刮削副本。
func EnsureMetadataPresentationSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range []string{
			`CREATE OR REPLACE FUNCTION metadata_source_text(doc jsonb, VARIADIC keys text[]) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT COALESCE((SELECT btrim(doc->>key) FROM unnest(keys) WITH ORDINALITY k(key,n)
 WHERE jsonb_typeof(doc->key) IN ('string','number') AND btrim(doc->>key)<>'' ORDER BY n LIMIT 1),'') $$`,
			`CREATE OR REPLACE FUNCTION metadata_source_list(raw jsonb) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT COALESCE(string_agg(value, ', ' ORDER BY n),'') FROM (
 SELECT n, CASE jsonb_typeof(item) WHEN 'string' THEN btrim(item#>>'{}')
 WHEN 'object' THEN metadata_source_text(item,'name','title') ELSE '' END value
 FROM jsonb_array_elements(CASE jsonb_typeof(raw) WHEN 'array' THEN raw
 WHEN 'string' THEN jsonb_build_array(raw) ELSE '[]'::jsonb END) WITH ORDINALITY a(item,n)
 ) parsed WHERE value<>'' $$`,
			`CREATE OR REPLACE VIEW metadata_presentations AS
SELECT m.id,m.created_at,m.updated_at,m.kind,m.parent_id,m.season_num,m.episode_num,
 m.latest_media_added_at,m.library_ids,
 m.tmdb_episode_checked_at,m.tmdb_season_checked_at,
 COALESCE(NULLIF(btrim(t.title),''),NULLIF(metadata_source_text(d.doc,'title','name'),''),metadata_source_text(ds.summary,'title','name')) AS title,
 COALESCE(NULLIF(btrim(t.original_name),''),metadata_source_text(d.doc,'original_title','original_name')) AS original_name,
 COALESCE(NULLIF(btrim(t.overview),''),metadata_source_text(d.doc,'intro','summary','short_comment','abstract')) AS overview,
 COALESCE(t.rating,0)::real AS rating,
 CASE WHEN date.value ~ '^[0-9]{4}' THEN left(date.value,4)::integer
 WHEN metadata_source_text(d.doc,'year') ~ '^[0-9]{4}' THEN left(metadata_source_text(d.doc,'year'),4)::integer ELSE 0 END AS year,
 date.value AS release_date,
 CASE WHEN metadata_source_text(t.fields,'runtime') ~ '^[0-9]{1,6}$'
 THEN (t.fields->>'runtime')::integer*60
 WHEN t.fields->'episode_run_time'->>0 ~ '^[0-9]{1,6}$'
 THEN (t.fields->'episode_run_time'->>0)::integer*60 ELSE 0 END AS runtime_sec,
 COALESCE(NULLIF(metadata_source_list(t.fields->'spoken_languages'),''),NULLIF(metadata_source_text(t.fields,'original_language'),''),
 NULLIF(metadata_source_list(d.doc->'languages'),''),metadata_source_list(d.doc->'language')) AS languages,
 COALESCE(NULLIF(metadata_source_list(t.fields->'production_countries'),''),NULLIF(metadata_source_list(t.fields->'origin_country'),''),
 NULLIF(metadata_source_list(d.doc->'countries'),''),NULLIF(metadata_source_list(d.doc->'country'),''),metadata_source_list(d.doc->'regions')) AS countries,
 COALESCE(NULLIF(metadata_source_list(t.fields->'genres'),''),NULLIF(metadata_source_list(d.doc->'genres'),''),metadata_source_list(d.doc->'genre')) AS genres,
 CASE WHEN t.id IS NOT NULL THEN 'tmdb' WHEN ds.id IS NOT NULL THEN 'douban' ELSE m.source END::text AS source,
 COALESCE(poster.asset_id,ds.poster_asset_id)::varchar(36) AS poster_asset_id,
 backdrop.asset_id AS backdrop_asset_id,still.asset_id AS still_asset_id
FROM metadata_items m
LEFT JOIN LATERAL (
 SELECT MIN(external_id) AS external_id FROM metadata_identifiers
 WHERE metadata_id=m.id AND entity_kind=m.kind AND provider='tmdb' HAVING COUNT(*)=1
) ti ON TRUE
LEFT JOIN tmdb_subjects t ON t.kind=m.kind AND t.tmdb_id=CASE WHEN ti.external_id ~ '^[0-9]{1,18}$' THEN ti.external_id::bigint END
LEFT JOIN LATERAL (
 SELECT MIN(external_id) external_id,MIN(COALESCE(NULLIF(douban_entity_kind,''),entity_kind)) entity_kind
 FROM metadata_identifiers WHERE metadata_id=m.id AND entity_kind=m.kind AND provider='douban' HAVING COUNT(*)=1
) di ON TRUE
LEFT JOIN douban_subjects ds ON ds.douban_id=di.external_id AND ds.entity_kind=di.entity_kind
LEFT JOIN LATERAL (SELECT CASE WHEN jsonb_typeof(ds.payload->'subject')='object' THEN ds.payload->'subject'
 WHEN jsonb_typeof(ds.payload->'data')='object' THEN ds.payload->'data' ELSE COALESCE(ds.payload,ds.summary) END doc) d ON TRUE
LEFT JOIN LATERAL (SELECT COALESCE(NULLIF(btrim(t.release_date),''),substring(metadata_source_text(d.doc,'release_date','pubdate') FROM '[0-9]{4}-[0-9]{2}-[0-9]{2}'),'') value) date ON TRUE
LEFT JOIN tmdb_artworks poster ON poster.subject_id=t.id AND poster.type='poster'
LEFT JOIN tmdb_artworks backdrop ON backdrop.subject_id=t.id AND backdrop.type='backdrop'
LEFT JOIN tmdb_artworks still ON still.subject_id=t.id AND still.type='still'`,
			`CREATE OR REPLACE VIEW metadata_artwork_presentations AS
SELECT m.id AS metadata_id, a.artwork_type, a.asset_id FROM metadata_presentations m
CROSS JOIN LATERAL (VALUES ('poster',m.poster_asset_id),('backdrop',m.backdrop_asset_id),('still',m.still_asset_id)) a(artwork_type,asset_id)
WHERE a.asset_id IS NOT NULL`,
		} {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		if err := EnsurePersonPresentationSchema(tx); err != nil {
			return err
		}
		return EnsureMetadataCreditPresentationSchema(tx)
	})
}

// EnsureMetadataCreditPresentationSchema 从 TMDb 当前关系读取演职员，译文按来源作品隔离。
func EnsureMetadataCreditPresentationSchema(db *gorm.DB) error {
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_tmdb_credits_pending_roles ON tmdb_credits(subject_id,id)
 WHERE origin='subject' AND category='cast' AND character<>'' AND character !~ '[一-鿿]'`).Error; err != nil {
		return err
	}
	return db.Exec(`CREATE OR REPLACE VIEW metadata_credit_presentations AS
SELECT c.id,c.created_at,c.updated_at,m.id AS metadata_id,p.person_id,
 c.subject_id AS source_subject_id,
 CASE WHEN c.category='cast' THEN 'Actor' WHEN c.job='Director' THEN 'Director' ELSE 'Writer' END::text AS type,
 c.character AS original_role,COALESCE(NULLIF(translation.translated_text,''),c.character) AS role,c.sort_order
FROM metadata_items m
JOIN metadata_identifiers binding ON binding.metadata_id=m.id AND binding.entity_kind=m.kind AND binding.provider='tmdb'
JOIN tmdb_subjects subject ON subject.kind=binding.entity_kind AND subject.tmdb_id::text=binding.external_id
JOIN tmdb_credits c ON c.subject_id=subject.id AND c.origin='subject'
JOIN tmdb_person_profiles p ON p.id=c.person_profile_id
LEFT JOIN LATERAL (SELECT translated_text FROM translation_caches cache
 WHERE cache.kind='role' AND cache.context_key=c.subject_id AND cache.source_text=c.character
 AND cache.target_language='zh-CN' AND cache.deleted_at IS NULL AND btrim(cache.translated_text)<>''
 ORDER BY cache.updated_at DESC,cache.id DESC LIMIT 1) translation ON TRUE
WHERE m.kind IN ('movie','series','season')
 AND (c.category='cast' OR c.job='Director' OR c.department='Writing')
 AND NOT EXISTS (SELECT 1 FROM metadata_identifiers other WHERE other.metadata_id=m.id
 AND other.provider='tmdb' AND other.entity_kind=m.kind AND other.id<>binding.id)`).Error
}

// EnsurePersonPresentationSchema 保留公共人物身份和本地头像，TMDb 人物正文及译名直接读取当前来源。
func EnsurePersonPresentationSchema(db *gorm.DB) error {
	return db.Exec(`CREATE OR REPLACE VIEW person_presentations AS
SELECT p.id,p.created_at,p.updated_at,p.deleted_at,
 CASE WHEN source.id IS NULL THEN p.name ELSE COALESCE(NULLIF(translation.translated_text,''),NULLIF(source.name,''),source.original_name,'') END AS name,
 CASE WHEN source.id IS NULL THEN p.original_name ELSE COALESCE(NULLIF(source.original_name,''),source.name,'') END AS original_name,
 p.normalized_name,
 CASE WHEN source.id IS NULL THEN p.overview ELSE COALESCE(source.biography,'') END AS overview,
 p.profile_url,p.profile_image_key,p.profile_image_source_url,p.source
FROM people p
LEFT JOIN tmdb_person_profiles source ON source.person_id=p.id
LEFT JOIN LATERAL (SELECT translated_text FROM translation_caches cache
 WHERE cache.kind='person_name' AND cache.context_key=p.id
 AND cache.source_text=COALESCE(NULLIF(source.original_name,''),source.name,'')
 AND cache.target_language='zh-CN' AND cache.deleted_at IS NULL AND btrim(cache.translated_text)<>''
 ORDER BY cache.updated_at DESC,cache.id DESC LIMIT 1) translation ON TRUE`).Error
}
