package service

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

func TestEmbyGlobalBatchMixedSourcesRefill(t *testing.T) {
	e := nfoBrowseFixture(t, 0, 0)
	db, ctx := e.repo.DB, t.Context()
	for _, query := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'ordinary-'||n,'movie','Title '||n,'local' FROM generate_series(1,155) n`,
		`INSERT INTO nfo_items(id,library_id,local_key,kind,title) SELECT 'local-'||n,'library-nfo',n::text,'movie','Title '||n FROM generate_series(1,155) n`,
		`INSERT INTO hongguo_works(id,source_id,kind,title,refreshed_at) SELECT 'source-'||n,n::text,'movie','Title '||n,now() FROM generate_series(1,155) n`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'ordinary-file-'||n,'ordinary-'||n,'library-nfo','/ordinary/'||n,TIMESTAMP '2026-01-01'-n*INTERVAL '1 minute' FROM generate_series(1,155) n`,
		`INSERT INTO media(id,library_id,catalog_source,path,created_at) SELECT source||'-file-'||n,'library-nfo',source,'/'||source||'/'||n,TIMESTAMP '2026-01-01'-n*INTERVAL '1 minute' FROM generate_series(1,155) n CROSS JOIN (VALUES ('nfo'),('hongguo')) s(source)`,
		`INSERT INTO nfo_media_bindings(media_id,item_id,title,fingerprint) SELECT 'nfo-file-'||n,'local-'||n,'Title','fixture' FROM generate_series(1,155) n`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id) SELECT 'hongguo-file-'||n,'source-'||n FROM generate_series(1,155) n`,
		`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,completed) SELECT 'state-'||n,'viewer','ordinary-'||n,'ordinary-file-'||n,true FROM generate_series(1,155) n WHERE n NOT IN (51,103,155)`,
		`INSERT INTO nfo_user_states(user_id,item_id,media_id,completed) SELECT 'viewer','local-'||n,'nfo-file-'||n,true FROM generate_series(1,155) n WHERE n NOT IN (51,103,155)`,
		`INSERT INTO hongguo_user_states(user_id,source_id,episode_number,completed) SELECT 'viewer',n::text,1,true FROM generate_series(1,155) n WHERE n NOT IN (51,103,155)`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	p := ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Movie"}, SortBy: "DateLastContentAdded", SortOrder: "Descending", Filters: []string{"IsUnplayed"}, Limit: 2, Fields: []string{"BasicSyncInfo"}}
	var expected []string
	if err := originalGlobalBrowseCandidates(t, e, p).Order(globalItemsOrder(p)).Pluck("id", &expected).Error; err != nil || len(expected) != 9 {
		t.Fatalf("oracle=%v err=%v", expected, err)
	}
	batches := 0
	if err := db.Callback().Row().After("gorm:row").Register("test:mixed-refill", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") && strings.Contains(tx.Statement.SQL.String(), "LEFT JOIN qualified") {
			batches++
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, skip := range []bool{false, true} {
		p.SkipTotalRecordCount = skip
		for _, start := range []int{0, 1, 3, 8, 9, 1000} {
			p.StartIndex, batches = start, 0
			page, err := e.Items(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range page["Items"].([]map[string]any) {
				got = append(got, item["Id"].(string))
			}
			want := pageSlice(expected, start, p.Limit)
			wantTotal := int64(9)
			if skip {
				wantTotal = 0
				if len(want) > 0 {
					wantTotal = int64(start + len(pageSlice(expected, start, p.Limit+1)))
				}
			}
			if page["TotalRecordCount"] != wantTotal || strings.Join(got, ",") != strings.Join(want, ",") || start == 0 && batches < 4 {
				t.Fatalf("start=%d ids=%v want=%v total=%v batches=%d", start, got, want, page["TotalRecordCount"], batches)
			}
		}
	}
	latest, err := e.LatestItems(ctx, p.UserID, "", 2, false, p.Fields...)
	if err != nil || len(latest) != 2 || latest[0]["Id"] != expected[0] || latest[1]["Id"] != expected[1] {
		t.Fatalf("latest=%v err=%v", latest, err)
	}
	// 无 NFO 时也按同一个全局作品排序合并。
	if err := db.Exec("DELETE FROM media WHERE catalog_source='nfo'").Error; err != nil {
		t.Fatal(err)
	}
	latest, err = e.LatestItems(ctx, p.UserID, "", 2, false, p.Fields...)
	if err != nil || len(latest) != 2 || latest[0]["Id"] != "ordinary-51" || latest[1]["Id"] != "hg-work-source-51" {
		t.Fatalf("mixed latest=%v err=%v", latest, err)
	}
}

func TestWebNFOFilteredBatchRefill(t *testing.T) {
	e := nfoBrowseFixture(t, 155, 1)
	for _, query := range []string{
		`UPDATE nfo_items SET title=CASE WHEN id IN ('show-51','show-103','show-155') THEN 'Missing' ELSE '中文标题' END WHERE kind='series'`,
		`UPDATE media SET created_at=TIMESTAMP '2026-01-01'-split_part(id,'-',3)::int*INTERVAL '1 minute'`,
	} {
		if err := e.repo.DB.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	filter := repository.MediaQueryFilter{MissingChineseTitle: true}
	for _, start := range []int{0, 1, 2, 3} {
		_, rows, total, err := e.repo.MediaView.ListLibraryMetadataPage(t.Context(), "library-nfo", "series", "", start, 2, filter)
		got := []string{}
		for _, row := range rows {
			got = append(got, row.MetadataID)
		}
		want := pageSlice([]string{"nfo-show-51", "nfo-show-103", "nfo-show-155"}, start, 2)
		if err != nil || total != 3 || !reflect.DeepEqual(got, want) {
			t.Fatalf("start=%d ids=%v total=%d want=%v err=%v", start, got, total, want, err)
		}
	}
	views, err := e.repo.MediaView.ListRecentLogicalWorks(t.Context(), 2, filter)
	ids := []string{}
	for _, view := range views {
		if len(ids) == 0 || ids[len(ids)-1] != view.SeriesID {
			ids = append(ids, view.SeriesID)
		}
	}
	if err != nil || !reflect.DeepEqual(ids, []string{"nfo-show-51", "nfo-show-103"}) {
		t.Fatalf("recent=%v err=%v", ids, err)
	}
}

func TestGlobalLatestUsesWorkTieOrderWithoutNFO(t *testing.T) {
	e := nfoBrowseFixture(t, 0, 0)
	for _, query := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('a','movie','A','local'),('b','movie','B','local'),('c','movie','C','local'),('z-series','series','Direct series','local')`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'file-'||id,id,'library-nfo','/fixture/'||id,TIMESTAMP '2026-01-01' FROM metadata_items`,
		`INSERT INTO hongguo_works(id,source_id,kind,title,refreshed_at) VALUES ('source','source','movie','Older',now())`,
		`INSERT INTO media(id,library_id,catalog_source,path,created_at) VALUES ('source-file','library-nfo','hongguo','/source','2020-01-01')`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id) VALUES ('source-file','source')`,
	} {
		if err := e.repo.DB.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 无分集的直接绑定 Series 没有作品卡片资格；全局并列统一按 ID 倒序。
	got, err := e.LatestItems(t.Context(), "viewer", "", 2, false, "BasicSyncInfo")
	if err != nil || len(got) != 2 || got[0]["Id"] != "c" || got[1]["Id"] != "b" {
		t.Fatalf("work latest tie order differs: items=%v err=%v", got, err)
	}
}
