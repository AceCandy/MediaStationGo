package database

import "gorm.io/gorm"

// EnsureDoubanCatalogSchema 将目录字段存为数据库生成列，所有资料写入都原子更新投影。
// 首次建立会计算已有资料；原始响应不变，后续启动只确认现行结构。
func EnsureDoubanCatalogSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('douban_catalog_schema'))`).Error; err != nil {
			return err
		}
		for _, stmt := range []string{
			`CREATE OR REPLACE FUNCTION douban_catalog_genres(payload jsonb, summary jsonb) RETURNS text[] LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
SELECT COALESCE(array_agg(DISTINCT value ORDER BY value), ARRAY[]::text[]) FROM (
SELECT value FROM (
	SELECT value, dense_rank() OVER (ORDER BY priority) AS preference FROM (
		SELECT priority, CASE jsonb_typeof(item)
			WHEN 'string' THEN btrim(item #>> '{}', E' \t\r\n')
			WHEN 'object' THEN COALESCE(NULLIF(btrim(CASE WHEN jsonb_typeof(item->'name') = 'string' THEN item->>'name' ELSE '' END, E' \t\r\n'), ''),
				btrim(CASE WHEN jsonb_typeof(item->'title') = 'string' THEN item->>'title' ELSE '' END, E' \t\r\n'))
			ELSE '' END AS value
		FROM (SELECT CASE WHEN jsonb_typeof(payload->'subject') = 'object' THEN payload->'subject'
			WHEN jsonb_typeof(payload->'data') = 'object' THEN payload->'data' ELSE COALESCE(payload, summary) END AS doc) source
		CROSS JOIN LATERAL (VALUES (1, doc->'genres'), (2, doc->'genre')) candidates(priority, raw)
		CROSS JOIN LATERAL jsonb_array_elements(CASE jsonb_typeof(raw)
			WHEN 'array' THEN raw WHEN 'string' THEN to_jsonb(regexp_split_to_array(raw #>> '{}', '[,，/]')) ELSE '[]'::jsonb END) item
	) parsed WHERE value <> ''
) preferred WHERE preference = 1
) genres $fn$`,
			`ALTER TABLE douban_subjects
    ADD COLUMN IF NOT EXISTS list_title text GENERATED ALWAYS AS (COALESCE(NULLIF(payload #>> '{subject,title}', ''), NULLIF(payload #>> '{subject,name}', ''), NULLIF(payload #>> '{data,title}', ''), NULLIF(payload #>> '{data,name}', ''), NULLIF(payload->>'title', ''), NULLIF(payload->>'name', ''), summary->>'title', '')) STORED,
    ADD COLUMN IF NOT EXISTS list_year text GENERATED ALWAYS AS (COALESCE(payload #>> '{subject,year}', payload #>> '{data,year}', payload->>'year', summary->>'year', '')) STORED,
    ADD COLUMN IF NOT EXISTS list_genres text[] GENERATED ALWAYS AS (douban_catalog_genres(payload, summary)) STORED`,
			`CREATE INDEX IF NOT EXISTS idx_douban_catalog_list ON douban_subjects(entity_kind, fetched_at DESC NULLS LAST, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_douban_catalog_genres ON douban_subjects USING gin(list_genres)`,
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
