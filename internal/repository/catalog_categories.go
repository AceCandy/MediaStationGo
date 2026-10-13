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

func (r *DoubanRepository) Categories(ctx context.Context, kind string) ([]CatalogCategory, error) {
	rows := []CatalogCategory{}
	err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT genre.value, genre.value AS label FROM douban_subjects
		CROSS JOIN LATERAL unnest(list_genres) AS genre(value)
		WHERE entity_kind = ? ORDER BY label`, kind).Scan(&rows).Error
	return rows, err
}
