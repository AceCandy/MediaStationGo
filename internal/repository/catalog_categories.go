package repository

import "context"

// CatalogCategory 是来源资料中已有的分类选项；值用于筛选，名称用于展示。
type CatalogCategory struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func (r *TMDbCatalogRepository) Categories(ctx context.Context, kind string) ([]CatalogCategory, error) {
	rows := []CatalogCategory{}
	if kind == "person" {
		err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT department AS value, department AS label FROM tmdb_person_profiles WHERE department <> '' ORDER BY value`).Scan(&rows).Error
		return rows, err
	}
	err := r.db.WithContext(ctx).Raw(`SELECT genre->>'id' AS value, MIN(genre->>'name') AS label
		FROM tmdb_subjects CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(fields->'genres') = 'array' THEN fields->'genres' ELSE '[]'::jsonb END) genre
		WHERE kind = ? AND genre->>'id' ~ '^[1-9][0-9]*$' AND COALESCE(genre->>'name', '') <> ''
		GROUP BY genre->>'id' ORDER BY label, value`, kind).Scan(&rows).Error
	return rows, err
}

// 与豆瓣详情解析一致：优先 subject/data 包装及非空 genres，兼容字符串和对象数组。
const doubanGenresSQL = `SELECT value FROM (
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
) preferred WHERE preference = 1`

func (r *DoubanRepository) Categories(ctx context.Context, kind string) ([]CatalogCategory, error) {
	rows := []CatalogCategory{}
	err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT genre.value, genre.value AS label FROM douban_subjects
		CROSS JOIN LATERAL (`+doubanGenresSQL+`) genre
		WHERE entity_kind = ? ORDER BY label`, kind).Scan(&rows).Error
	return rows, err
}
