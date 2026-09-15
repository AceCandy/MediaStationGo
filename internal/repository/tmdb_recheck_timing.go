package repository

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// TMDbRecheckTiming 使用所属季的播出安排决定复查频率，不受同剧其他季影响。
type TMDbRecheckTiming struct {
	SeasonReleaseDate  string
	SeasonEpisodeDates string
}

// Cooldown 以 UTC 播出日分档；非法或未知日期保留三天冷却。
func (t TMDbRecheckTiming) Cooldown(now time.Time) time.Duration {
	today := now.UTC().Truncate(24 * time.Hour)
	var dates []string
	_ = json.Unmarshal([]byte(t.SeasonEpisodeDates), &dates)
	latest := time.Time{}
	for _, value := range dates {
		date, err := time.Parse(time.DateOnly, value)
		if err != nil {
			continue
		}
		if !date.Before(today.AddDate(0, 0, -30)) && !date.After(today.AddDate(0, 0, 30)) {
			return 24 * time.Hour
		}
		if !date.After(today) && date.After(latest) {
			latest = date
		}
	}
	if latest.IsZero() {
		latest, _ = time.Parse(time.DateOnly, t.SeasonReleaseDate)
	}
	switch {
	case latest.IsZero(), latest.After(today.AddDate(0, 0, 30)):
		return 3 * 24 * time.Hour
	case !latest.Before(today.AddDate(0, 0, -30)):
		return 24 * time.Hour
	case !latest.Before(today.AddDate(0, 0, -365)):
		return 10 * 24 * time.Hour
	default:
		return 20 * 24 * time.Hour
	}
}

// 投影只展开目标季；快照编号必须与该季一致，畸形清单不参与日期判断。
const tmdbRecheckTimingColumns = `COALESCE(timing_season.release_date,'') AS season_release_date,
COALESCE((SELECT jsonb_agg(d.air_date) FROM (
 SELECT e.release_date AS air_date FROM metadata_items e WHERE e.parent_id=timing_season.id AND e.kind='episode'
 UNION ALL
 SELECT e->>'air_date' FROM metadata_provider_snapshots p,
 jsonb_array_elements(CASE WHEN jsonb_typeof(p.payload->'episodes')='array' THEN p.payload->'episodes' ELSE '[]'::jsonb END) e
 WHERE p.metadata_id=timing_season.id AND p.provider='tmdb'
 AND p.payload->>'season_number'=timing_season.season_num::text
 AND p.payload->>'id' ~ '^[1-9][0-9]*$'
) d),'[]'::jsonb)::text AS season_episode_dates`

type tmdbRecheckSeasonOrder struct {
	TMDbRecheckTiming
	MetadataID string
	DueAt      time.Time
	cooldown   time.Duration
}

const tmdbRecheckSeasonOrderSQL = `WITH due AS MATERIALIZED (
 SELECT COALESCE(CASE WHEN m.kind='episode' THEN m.parent_id END,j.metadata_id) AS metadata_id, min(j.due_at) AS due_at
 FROM tm_db_recheck_jobs j LEFT JOIN metadata_items m ON m.id=j.metadata_id
 WHERE j.due_at<=? GROUP BY 1)
SELECT due.metadata_id,due.due_at, ` + tmdbRecheckTimingColumns + `
FROM due LEFT JOIN metadata_items timing_season ON timing_season.id=due.metadata_id AND timing_season.kind='season'`

// ListTMDbRecheckSeasonOrder 每轮仅聚合排序一次，避免每次领取都扫描全部待办。
func (r *MetadataRepository) ListTMDbRecheckSeasonOrder(ctx context.Context, cutoff time.Time) ([]string, error) {
	var seasons []tmdbRecheckSeasonOrder
	if err := r.db.WithContext(ctx).Raw(tmdbRecheckSeasonOrderSQL, cutoff).Scan(&seasons).Error; err != nil {
		return nil, err
	}
	for i := range seasons {
		seasons[i].cooldown = seasons[i].Cooldown(cutoff)
		seasons[i].TMDbRecheckTiming = TMDbRecheckTiming{}
	}
	sort.Slice(seasons, func(i, j int) bool {
		a, b := seasons[i], seasons[j]
		if a.cooldown != b.cooldown {
			return a.cooldown < b.cooldown
		}
		if !a.DueAt.Equal(b.DueAt) {
			return a.DueAt.Before(b.DueAt)
		}
		return a.MetadataID < b.MetadataID
	})
	ids := make([]string, len(seasons))
	for i := range seasons {
		ids[i] = seasons[i].MetadataID
	}
	return ids, nil
}
