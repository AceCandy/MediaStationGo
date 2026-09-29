package service

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestEmbyItemsCountModes(t *testing.T) {
	e := nfoBrowseFixture(t, 3, 2)
	db := e.repo.DB
	for _, query := range []string{
		`INSERT INTO libraries(id,name,type,path) VALUES ('movies','Movies','movie','/fixture/movies'),('tv','TV','tv','/fixture/tv'),('hg','Source','hongguo','/fixture/source')`,
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'movie-'||n,'movie','Movie '||n,'local' FROM generate_series(1,3) n`,
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('show','series','Show','local')`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num) VALUES ('season','season','Season','local','show',1)`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,episode_num) SELECT 'ep-'||n,'episode','Episode '||n,'local','season',n FROM generate_series(1,3) n`,
		`INSERT INTO media(id,metadata_id,library_id,path,season_num,episode_num) SELECT 'file-'||id,id,'movies','/fixture/movies/'||id,0,0 FROM metadata_items WHERE kind='movie'`,
		`INSERT INTO media(id,metadata_id,library_id,path,season_num,episode_num) SELECT lib||'-'||id,id,lib,'/fixture/'||lib||'/Season 01/'||id,1,episode_num FROM metadata_items CROSS JOIN (VALUES ('tv'),('movies')) l(lib) WHERE kind='episode'`,
		`INSERT INTO hongguo_works(id,source_id,kind,title,refreshed_at) SELECT 'work-'||n,n::text,'series','Source '||n,now() FROM generate_series(1,3) n`,
		`INSERT INTO hongguo_episodes(id,work_id,number) SELECT 'hg-ep-'||n,'work-'||n,1 FROM generate_series(1,3) n`,
		`INSERT INTO media(id,library_id,catalog_source,path) SELECT 'hg-file-'||n,'hg','hongguo','/fixture/source/'||n FROM generate_series(1,3) n`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) SELECT 'hg-file-'||n,'work-'||n,'hg-ep-'||n FROM generate_series(1,3) n`,
		`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,position_ms) VALUES ('progress','viewer','movie-1','file-movie-1',30000)`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Person{Base: model.Base{ID: "person"}, Name: "Actor", OriginalName: "Actor", NormalizedName: "actor", Source: "local"}).Error; err != nil {
		t.Fatal(err)
	}
	var counts atomic.Int64
	capture := func(tx *gorm.DB) {
		sql := strings.ToLower(strings.TrimSpace(tx.Statement.SQL.String()))
		if strings.HasPrefix(sql, "select count(*)") || strings.Contains(sql, ") select count(*) from (") || strings.Contains(sql, "select count(*) as total from ") {
			counts.Add(1)
		}
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:count-modes", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:count-modes", capture); err != nil {
		t.Fatal(err)
	}
	for _, p := range []ItemsParams{
		{ParentID: "movies", IncludeItemTypes: []string{"Movie"}},
		{ParentID: "movies"},
		{ParentID: "movies", SortBy: "Random"},
		{ParentID: "tv", IncludeItemTypes: []string{"Series"}},
		{ParentID: "show"}, {ParentID: "season"},
		{ParentID: "library-nfo", IncludeItemTypes: []string{"Series"}},
		{ParentID: "library-nfo", IncludeItemTypes: []string{"Series"}, SortBy: "DateLastContentAdded", SortOrder: "Descending"},
		{ParentID: "nfo-show-1"}, {ParentID: "nfo-season-1"},
		{ParentID: "hg", IncludeItemTypes: []string{"Series"}},
		{ParentID: "hg-work-work-1"}, {ParentID: "hg-season-work-1"},
		{Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}},
		{Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, SortBy: "Random", Filters: []string{"IsUnplayed"}},
		{Recursive: true, Filters: []string{"IsResumable"}},
		{ParentID: "movies", Filters: []string{"IsResumable"}, IncludeItemTypes: []string{"Movie"}},
		{IncludeItemTypes: []string{"Person"}},
	} {
		p.UserID, p.Limit, p.Fields, p.randomSeed = "viewer", 2, []string{"BasicSyncInfo"}, 42
		for _, start := range []int{0, 2, 1000} {
			p.StartIndex = start
			counted, err := e.Items(t.Context(), p)
			if err != nil {
				t.Fatalf("params=%+v: %v", p, err)
			}
			counts.Store(0)
			p.SkipTotalRecordCount = true
			uncounted, err := e.Items(t.Context(), p)
			if err != nil || counts.Load() != 0 {
				t.Fatalf("params=%+v count queries=%d err=%v", p, counts.Load(), err)
			}
			delete(counted, "TotalRecordCount")
			delete(uncounted, "TotalRecordCount")
			want, _ := json.Marshal(counted)
			got, _ := json.Marshal(uncounted)
			if string(got) != string(want) {
				t.Fatalf("params=%+v: count changed the page\ngot=%s\nwant=%s", p, got, want)
			}
			p.SkipTotalRecordCount = false
		}
	}
	for _, count := range []bool{false, true} {
		counts.Store(0)
		people, total, err := e.repo.Person.List(t.Context(), "Actor", nil, 0, 2, count)
		wantTotal := int64(0)
		if count {
			wantTotal = 1
		}
		if err != nil || len(people) != 1 || total != wantTotal || counts.Load() != wantTotal {
			t.Fatalf("ordinary people count=%v: rows=%d total=%d queries=%d err=%v", count, len(people), total, counts.Load(), err)
		}
	}
}
