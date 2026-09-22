package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/logger"
)

type containerDetailReadLog struct {
	logger.Interface
	viewRows int64
}

func (l *containerDetailReadLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.Contains(sql, "AS view_series_id") && rows > 0 {
		l.viewRows += rows
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func TestEmbyContainerDetailBoundsReadsAndPreservesPayload(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	for _, id := range []string{"a-library", "z-library"} {
		if err := db.Create(&model.Library{Base: model.Base{ID: id}, Name: id, Path: "/fixture/" + id, Type: "tv"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, sql := range []string{
		`INSERT INTO metadata_items (id, kind, title, source) VALUES ('detail-series', 'series', 'Series', 'local')`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, season_num)
		 SELECT 'detail-season-' || n, 'season', 'detail-series', 'Season ' || n, 'local', n FROM generate_series(0,3) n`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, episode_num)
		 SELECT 'detail-episode-' || s || '-' || n, 'episode', 'detail-season-' || s, 'Episode', 'local', n
		 FROM generate_series(0,2) s CROSS JOIN generate_series(1,200) n`,
		`INSERT INTO media (id, library_id, metadata_id, path, season_num, episode_num, strm_url, created_at, updated_at)
		 SELECT 'detail-file-' || s || '-' || n || '-' || v, CASE WHEN v = 1 THEN 'z-library' ELSE 'a-library' END,
		 'detail-episode-' || s || '-' || n, '/fixture/' || s || '/' || n || '-' || v || '.mkv', s, n,
		 CASE WHEN v = 1 THEN '' ELSE 'https://fixture.invalid/video' END,
		 '2025-01-01'::timestamp + (n + v * 1000) * interval '1 second', NOW()
		 FROM generate_series(0,2) s CROSS JOIN generate_series(1,200) n CROSS JOIN generate_series(1,2) v`,
		`UPDATE media SET part_group_key = 'detail-parts', part_index = 1 WHERE id = 'detail-file-0-1-1'`,
		`INSERT INTO media (id, library_id, metadata_id, path, season_num, episode_num, part_group_key, part_index, created_at, updated_at)
		 VALUES ('detail-part-2', 'z-library', 'detail-episode-0-1', '/fixture/part2.mkv', 0, 1, 'detail-parts', 2, NOW(), NOW())`,
		`UPDATE metadata_items SET nsfw = TRUE WHERE id = 'detail-episode-1-200'`,
		`UPDATE media SET strm_url = '' WHERE id IN ('detail-file-0-1-2', 'detail-file-0-2-2')`,
		`INSERT INTO media_probe_metadata (media_id, probe_json, schema_version, width, size_bytes, probed_at)
		 SELECT id, '{}', 1, CASE WHEN id LIKE '%-1' THEN 1920 ELSE 1280 END, 1000, NOW()
		 FROM media WHERE metadata_id = 'detail-episode-0-1'`,
		`INSERT INTO media_probe_metadata (media_id, probe_json, schema_version, width, size_bytes, probed_at)
		 SELECT id, '{}', 1, 1920, CASE WHEN id LIKE '%-1' THEN 2000 ELSE 1000 END, NOW()
		 FROM media WHERE metadata_id = 'detail-episode-0-2'`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"detail-series", "detail-season-0"} {
		if err := db.Create(&model.Favorite{UserID: "viewer", MetadataID: id}).Error; err != nil {
			t.Fatal(err)
		}
		createServiceTestArtwork(t, db, id, model.ArtworkTypePoster, id+"-poster")
	}
	if err := db.Exec(`INSERT INTO playback_histories (id, user_id, metadata_id, media_id, completed, watched_at)
		SELECT 'history-' || id, 'viewer', metadata_id, id, TRUE, NOW() FROM media WHERE id LIKE 'detail-file-0-%-1'`).Error; err != nil {
		t.Fatal(err)
	}
	for _, visibility := range []MediaVisibility{
		{IncludeNSFW: true},
		{AllowedLibraryIDs: []string{"z-library"}},
		{IncludeNSFW: true, HiddenLibraryIDs: []string{"z-library"}},
		{AllowedLibraryIDs: []string{"missing-library"}},
	} {
		svc.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: visibility, expiresAt: time.Now().Add(time.Hour)}}
		for _, id := range []string{"detail-series", "detail-season-0", "detail-season-1", "detail-season-3", "missing"} {
			var want map[string]any
			if id == "detail-series" {
				group, ok, err := svc.findSeriesGroup(t.Context(), id, "viewer")
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					want = svc.seriesPayload(t.Context(), group, "viewer")
				}
			} else {
				season, ok, err := svc.findSeasonGroup(t.Context(), id, "viewer")
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					want = svc.seasonPayload(t.Context(), season, "viewer")
				}
			}
			reads := &containerDetailReadLog{Interface: db.Logger}
			db.Logger = reads
			got, err := svc.Item(t.Context(), id, "viewer")
			db.Logger = reads.Interface
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s visibility=%+v\ngot=%#v\nwant=%#v", id, visibility, got, want)
			}
			if reads.viewRows > 1 {
				t.Fatalf("%s hydrated %d file views, want at most one", id, reads.viewRows)
			}
			if cached, ok := svc.cachedSeriesGroup("detail-series"); ok && cached.Summary != nil {
				t.Fatal("detail summary replaced the full playback cache")
			}
		}
	}
}
