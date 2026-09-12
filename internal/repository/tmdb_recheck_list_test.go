package repository

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTMDbRecheckListOrdersEpisodesBeforePagination(t *testing.T) {
	db := recheckQueueDB(t)
	repo := New(db).Metadata
	var want []string
	for _, title := range []string{"Alpha", "Beta"} {
		series := model.MetadataItem{Kind: "series", Title: title}
		if err := db.Create(&series).Error; err != nil {
			t.Fatal(err)
		}
		for _, seasonNum := range []int{2, 10} {
			season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: seasonNum}
			if err := db.Create(&season).Error; err != nil {
				t.Fatal(err)
			}
			for _, episodeNum := range []int{2, 10, 110} {
				episode := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: episodeNum, Title: "Episode"}
				if err := db.Create(&episode).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.Media{MetadataID: episode.ID, Path: "/recheck/" + episode.ID + ".strm"}).Error; err != nil {
					t.Fatal(err)
				}
				// 到期时间与展示顺序相反，确保列表不再按调度时间排序。
				due := time.Now().UTC().Add(-time.Duration(len(want)) * time.Hour)
				if err := db.Create(&model.TMDbRecheckJob{MetadataID: episode.ID, Status: "not_found", DueAt: &due}).Error; err != nil {
					t.Fatal(err)
				}
				want = append(want, episode.ID)
			}
		}
	}
	for _, keyword := range []string{"", "Episode", "Alpha"} {
		expected := want
		if keyword == "Alpha" {
			expected = want[:6]
		}
		var got []string
		for page := 1; page <= (len(expected)+1)/2+1; page++ {
			out, err := repo.ListTMDbRechecks(t.Context(), "not_found", keyword, page, 2)
			if err != nil || out.Total != int64(len(expected)) {
				t.Fatalf("keyword=%q page=%d: total=%d err=%v", keyword, page, out.Total, err)
			}
			for _, row := range out.Items {
				got = append(got, row.MetadataID)
			}
		}
		if !slices.Equal(got, expected) {
			t.Fatalf("keyword=%q: got=%v want=%v", keyword, got, expected)
		}
	}
}

func TestTMDbRecheckListTracksMediaDeletion(t *testing.T) {
	db := recheckQueueDB(t)
	repo := New(db).Metadata
	series := model.MetadataItem{Kind: "series", Title: "Series"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episodes := []model.MetadataItem{
		{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1},
		{Kind: "episode", ParentID: &season.ID, EpisodeNum: 2},
	}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	files := []model.Media{
		{MetadataID: episodes[0].ID, Path: "/recheck/episode-v1.strm"},
		{MetadataID: episodes[0].ID, Path: "/recheck/episode-v2.strm"},
		{MetadataID: episodes[1].ID, Path: "/recheck/episode-2.strm"},
		{MetadataID: season.ID, Path: "/recheck/season.strm"},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Microsecond)
	jobs := []model.TMDbRecheckJob{
		{MetadataID: season.ID, Status: "not_found"},
		{MetadataID: episodes[0].ID, Status: "not_found", DueAt: &due, Attempts: 2, NotFoundIdentity: "unchanged"},
		{MetadataID: episodes[1].ID, Status: "retry"},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	check := func(want map[string]string) {
		t.Helper()
		counts := map[string]int64{}
		for _, status := range want {
			counts[status]++
		}
		summary, err := repo.SummarizeTMDbRechecks(t.Context())
		if err != nil || !maps.Equal(summary.Counts, counts) {
			t.Fatalf("summary=%+v err=%v, want=%v", summary, err, counts)
		}
		for _, id := range []string{season.ID, episodes[0].ID} {
			files, err := repo.ListTMDbRecheckFiles(t.Context(), id, 1, 100)
			if err != nil || (len(files.Items) > 0) != (want[id] == "not_found") {
				t.Fatalf("file scope differs for %s: %+v err=%v", id, files, err)
			}
		}
		for _, keyword := range []string{"", "Series"} {
			for _, status := range []string{"", "not_found", "retry"} {
				expected := map[string]string{}
				for id, value := range want {
					if status == "" || status == value {
						expected[id] = value
					}
				}
				seen := map[string]string{}
				// 一条一页，覆盖过滤发生在分页前和末页为空时的总数。
				for page := 1; page <= len(expected)+1; page++ {
					out, err := repo.ListTMDbRechecks(t.Context(), status, keyword, page, 1)
					items, itemsErr := repo.ListTMDbRecheckItems(t.Context(), status, keyword, page, 1)
					if itemsErr != nil || items.Total != out.Total || !slices.EqualFunc(items.Items, out.Items, func(a, b TMDbRecheckRow) bool { return a.MetadataID == b.MetadataID }) || len(items.Counts) != 0 || items.Changes != 0 {
						t.Fatalf("items-only differs: %+v err=%v, full=%+v", items, itemsErr, out)
					}
					if err != nil || out.Total != int64(len(expected)) || !maps.Equal(out.Counts, counts) {
						t.Fatalf("status=%q keyword=%q page=%d: %+v err=%v, want=%v counts=%v", status, keyword, page, out, err, expected, counts)
					}
					if page <= len(expected) && len(out.Items) != 1 || page > len(expected) && len(out.Items) != 0 {
						t.Fatalf("unexpected page: %+v", out)
					}
					for _, item := range out.Items {
						seen[item.MetadataID] = item.Status
					}
				}
				if !maps.Equal(seen, expected) {
					t.Fatalf("items=%v want=%v", seen, expected)
				}
			}
		}
	}
	want := map[string]string{season.ID: "not_found", episodes[0].ID: "not_found", episodes[1].ID: "retry"}
	check(want)
	// 不归并变更或运行复查任务，媒体删除后的下一次查询就应生效。
	for i := range files {
		if err := db.Delete(&files[i]).Error; err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			delete(want, episodes[0].ID)
		}
		if i == 2 {
			delete(want, episodes[1].ID)
		}
		if i == 3 {
			delete(want, season.ID)
		}
		check(want)
	}
	// 重建媒体关联后恢复展示，查询不得删除待办或修改冷却状态。
	if err := db.Create(&model.Media{MetadataID: episodes[0].ID, Path: "/recheck/restored.strm"}).Error; err != nil {
		t.Fatal(err)
	}
	check(map[string]string{season.ID: "not_found", episodes[0].ID: "not_found"})
	var saved model.TMDbRecheckJob
	if err := db.First(&saved, "metadata_id=?", episodes[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Status != "not_found" || saved.DueAt == nil || !saved.DueAt.Equal(due) || saved.Attempts != 2 || saved.NotFoundIdentity != "unchanged" {
		t.Fatalf("query changed recheck state: %+v", saved)
	}
}

func TestTMDbRecheckListCountsAvoidPerEpisodeProbes(t *testing.T) {
	db := recheckQueueDB(t)
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source,season_num,episode_num) VALUES ('series','series','Series','tmdb',0,0)`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num) SELECT 's'||i,'season','Season','tmdb','series',i,0 FROM generate_series(1,3000) i`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num) SELECT 'e'||i,'episode','Episode','tmdb','s'||((i-1)/100+1),0,i FROM generate_series(1,30000) i`,
		`INSERT INTO media(id,path,metadata_id) SELECT 'm'||i,'/test/'||i,'e'||i FROM generate_series(1,30000) i`,
		`INSERT INTO tm_db_recheck_jobs(metadata_id,status) SELECT 'e'||i,'pending' FROM generate_series(1,30000) i`,
		`INSERT INTO tm_db_recheck_jobs(metadata_id,status) SELECT 's'||i,'pending' FROM generate_series(1,30) i`,
		`INSERT INTO tm_db_recheck_jobs(metadata_id,status) VALUES ('series','done'),('missing-target','done')`,
		`ANALYZE metadata_items`, `ANALYZE media`, `ANALYZE tm_db_recheck_jobs`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	summary, err := New(db).Metadata.SummarizeTMDbRechecks(t.Context())
	if err != nil || summary.Counts["pending"] != 30030 || len(summary.Counts) != 1 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	jobs := db.Table("tm_db_recheck_jobs").Select("metadata_id, status")
	var raw string
	if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+tmdbRecheckCountsSQL, jobs, jobs).Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	type node struct {
		Relation string `json:"Relation Name"`
		Loops    int    `json:"Actual Loops"`
		Plans    []node `json:"Plans"`
	}
	var plans []struct{ Plan node }
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("invalid plan: %s, err=%v", raw, err)
	}
	var check func(node)
	check = func(n node) {
		if (n.Relation == "media" || n.Relation == "metadata_items") && n.Loops > 1000 {
			t.Fatalf("per-episode/catalog probes returned: %s", raw)
		}
		for _, child := range n.Plans {
			check(child)
		}
	}
	check(plans[0].Plan)
	// 分页不依赖全状态统计或变更表；即使统计不可用，列表仍独立返回精确总数。
	if err := db.Exec(`DROP TABLE tm_db_recheck_asset_changes`).Error; err != nil {
		t.Fatal(err)
	}
	items, err := New(db).Metadata.ListTMDbRecheckItems(t.Context(), "pending", "", 1502, 20)
	if err != nil || items.Total != 30030 || len(items.Items) != 10 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
