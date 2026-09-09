package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/logger"
)

type seriesPageReadLog struct {
	logger.Interface
	queries       []string
	playedQueries []string
}

func (l *seriesPageReadLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.Contains(sql, "BOOL_AND") {
		l.playedQueries = append(l.playedQueries, sql)
	}
	if strings.Contains(sql, "AS scoped_series") || strings.Contains(sql, "AS series_id") || strings.Contains(sql, "ARRAY_AGG(m.id") {
		l.queries = append(l.queries, sql)
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func TestEmbySeriesPaginationDoesNotProbeFilesForWholeCatalog(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	lib := model.Library{Name: "Series", Path: "/fixture/series", Type: "tv"}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	// 目录包含 2 万集，只有其中 20 部剧的前 100 集各有两个文件版本。
	for _, sql := range []string{
		`INSERT INTO metadata_items (id, kind, title, source, season_num, episode_num)
		 SELECT 'series-' || n, 'series', LPAD(n::text, 3, '0'), 'local', 0, 0 FROM generate_series(1,100) n`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, season_num, episode_num)
		 SELECT 'season-' || n, 'season', 'series-' || n, 'Season', 'local', 1, 0 FROM generate_series(1,100) n`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, season_num, episode_num)
		 SELECT 'episode-' || s || '-' || n, 'episode', 'season-' || s, 'Episode', 'local', 0, n
		 FROM generate_series(1,100) s CROSS JOIN generate_series(1,200) n`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, metadata_id, path, season_num, episode_num, created_at, updated_at)
		SELECT 'file-' || s || '-' || n || '-' || v, ?, 'episode-' || s || '-' || n,
		'/fixture/' || s || '/' || n || '-' || v || '.mkv', 1, n, NOW(), NOW()
		FROM generate_series(1,20) s CROSS JOIN generate_series(1,100) n CROSS JOIN generate_series(1,2) v`, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"media", "metadata_items"} {
		if err := db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	reads := &seriesPageReadLog{Interface: db.Logger}
	db.Logger = reads
	p := ItemsParams{UserID: "page-user", ParentID: lib.ID, IncludeItemTypes: []string{"Series"}, Recursive: true,
		SortBy: "DateLastContentAdded,SortName", SortOrder: "Descending", Limit: 3, Fields: []string{"BasicSyncInfo"}}
	result, err := svc.Items(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	items := result["Items"].([]map[string]any)
	if result["TotalRecordCount"] != 20 || len(items) != 3 || items[0]["Id"] != "series-20" || items[2]["Id"] != "series-18" || items[0]["RecursiveItemCount"] != 100 || items[0]["ChildCount"] != 1 {
		t.Fatalf("unexpected series page: %#v", result)
	}
	queries := append([]string(nil), reads.queries...)
	if len(queries) != 2 {
		t.Fatalf("count/page queries = %d", len(queries))
	}
	for _, query := range queries {
		if !strings.Contains(query, "WITH scoped_media AS MATERIALIZED") || strings.Contains(query, "JOIN LATERAL") {
			t.Fatalf("series pagination must materialize visible files before parent joins: %s", query)
		}
	}
	if len(reads.playedQueries) != 1 {
		t.Fatalf("played summary queries = %d, want one per page", len(reads.playedQueries))
	}
	queries = append(queries, reads.playedQueries...)
	reads.queries = nil
	web := NewMediaService(svc.cfg, svc.log, svc.repo)
	cards, total, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 3, "", "", MediaVisibility{})
	if err != nil || total != 20 || len(cards) != 3 || cards[0].Count != 100 {
		t.Fatalf("unexpected web page: cards=%d total=%d err=%v", len(cards), total, err)
	}
	if len(reads.queries) != 1 {
		t.Fatalf("web page queries = %d", len(reads.queries))
	}
	queries = append(queries, reads.queries...)
	// 验证实际执行计划的文件探测次数，不用受机器负载影响的毫秒阈值。
	type planNode struct {
		Relation string     `json:"Relation Name"`
		Loops    float64    `json:"Actual Loops"`
		Plans    []planNode `json:"Plans"`
	}
	var check func(planNode)
	check = func(node planNode) {
		if node.Relation == "media" && node.Loops > 4000 {
			t.Errorf("file probes expanded to catalog size: %.0f", node.Loops)
		}
		for _, child := range node.Plans {
			check(child)
		}
	}
	for _, query := range queries {
		var raw string
		if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON) " + query).Row().Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plans []struct{ Plan planNode }
		if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid execution plan: %v", err)
		}
		check(plans[0].Plan)
	}
	for _, sortBy := range []string{"SortName", "DateCreated", "PremiereDate"} {
		p.SortBy, p.SortOrder, p.StartIndex = sortBy, "Ascending", 1
		result, err = svc.Items(t.Context(), p)
		if err != nil || result["TotalRecordCount"] != 20 || len(result["Items"].([]map[string]any)) != 3 {
			t.Fatalf("sort %s page changed: %#v, %v", sortBy, result, err)
		}
		if sortBy == "SortName" && result["Items"].([]map[string]any)[0]["Id"] != "series-2" {
			t.Fatalf("name sorting or offset changed: %#v", result)
		}
	}
	p.StartIndex = 20
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 20 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("empty page changed: %#v, %v", result, err)
	}
	// 同一个过滤 scope 必须同时约束计数、分页和当前页摘要。
	for _, row := range []any{
		&model.Person{Base: model.Base{ID: "page-person"}, Name: "Actor", Source: "local"},
		&model.MetadataCredit{MetadataID: "series-20", PersonID: "page-person", Type: model.CreditTypeActor},
		&model.Favorite{UserID: "page-user", MetadataID: "series-20"},
		&model.PlaybackHistory{UserID: "page-user", MetadataID: "episode-20-1", MediaID: "file-20-1-1", Completed: true},
		&model.Media{LibraryID: "hidden", MetadataID: "episode-20-101", Path: "/fixture/hidden.mkv", SeasonNum: 1, EpisodeNum: 101},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.MetadataItem{}).Where("id = ?", "episode-20-100").Update("nsfw", true).Error; err != nil {
		t.Fatal(err)
	}
	svc.visibilityCache["page-user"] = embyVisibilityCacheEntry{
		visibility: MediaVisibility{AllowedLibraryIDs: []string{lib.ID}, HiddenLibraryIDs: []string{"hidden"}},
		expiresAt:  time.Now().Add(time.Minute),
	}
	p.UserID, p.ParentID, p.StartIndex = "page-user", "", 0
	p.Filters = []string{"IsFavorite"}
	reads.queries = nil
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 1 || len(result["Items"].([]map[string]any)) != 1 || result["Items"].([]map[string]any)[0]["RecursiveItemCount"] != 99 {
		t.Fatalf("favorite-only page changed: %#v, %v", result, err)
	}
	favoriteQueries := append([]string(nil), reads.queries...)
	if len(favoriteQueries) != 2 {
		t.Fatalf("favorite count/page queries = %d", len(favoriteQueries))
	}
	for _, query := range favoriteQueries {
		if !strings.Contains(query, "WITH scoped_media AS NOT MATERIALIZED") {
			t.Fatalf("favorite filter cannot be pushed into file scope: %s", query)
		}
	}
	p.StartIndex = 1
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 1 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("favorite empty page changed: %#v, %v", result, err)
	}
	p.StartIndex, p.UserID = 0, "other-user"
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 0 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("favorites leaked between users: %#v, %v", result, err)
	}
	p.UserID = "page-user"
	p.PersonIDs, p.Filters = []string{"page-person"}, []string{"IsFavorite"}
	result, err = svc.Items(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	items = result["Items"].([]map[string]any)
	if result["TotalRecordCount"] != 1 || len(items) != 1 || items[0]["Id"] != "series-20" || items[0]["RecursiveItemCount"] != 99 {
		t.Fatalf("filtered page changed: %#v", result)
	}
	p.PersonIDs = []string{"missing-person"}
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 0 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("person filter ignored: %#v, %v", result, err)
	}
	p.PersonIDs = []string{"page-person"}
	if err := db.Where("user_id = ?", "page-user").Delete(&model.Favorite{}).Error; err != nil {
		t.Fatal(err)
	}
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 0 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("deleted favorite included: %#v, %v", result, err)
	}
	items, err = svc.LatestItems(t.Context(), "page-user", lib.ID, 3, true)
	if err != nil || len(items) != 1 || items[0]["Id"] != "series-20" || items[0]["RecursiveItemCount"] != 1 {
		t.Fatalf("played series changed: %#v, %v", items, err)
	}
}
