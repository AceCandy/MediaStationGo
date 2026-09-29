package service

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestEmbyRandomGlobalWithoutExternalCatalogs(t *testing.T) {
	e := nfoBrowseFixture(t, 0, 0)
	for _, query := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('movie','movie','Movie','local'),('show','series','Show','local')`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num) VALUES ('season','season','Season','local','show',1)`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,episode_num) VALUES ('episode','episode','Episode','local','season',1)`,
		`INSERT INTO media(id,metadata_id,library_id,path,season_num,episode_num) VALUES ('movie-file','movie','library-nfo','/fixture/movie.mkv',0,0),('episode-file','episode','library-nfo','/fixture/show/Season 01/episode.mkv',1,1)`,
		`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,completed) VALUES ('watched','viewer','movie','movie-file',true)`,
	} {
		if err := e.repo.DB.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	p := ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, SortBy: "Random", randomSeed: 42, SkipTotalRecordCount: true, Limit: 20, Fields: []string{"BasicSyncInfo"}}
	for _, filters := range [][]string{nil, {"IsUnplayed"}, {"IsPlayed"}} {
		p.Filters = filters
		page, err := e.Items(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, item := range page["Items"].([]map[string]any) {
			ids = append(ids, item["Id"].(string))
		}
		sort.Strings(ids)
		want := []string{"movie", "show"}
		if len(filters) > 0 {
			want = []string{"show"}
			if filters[0] == "IsPlayed" {
				want = []string{"movie"}
			}
		}
		if !reflect.DeepEqual(ids, want) || page["TotalRecordCount"] != int64(len(want)) {
			t.Fatalf("filters=%v ids=%v total=%v", filters, ids, page["TotalRecordCount"])
		}
	}
	// Video 沿用普通文件列表身份，不能被作品级全局查询当作不存在的 kind。
	p.IncludeItemTypes, p.Filters = []string{"Video"}, nil
	videoPage, err := e.Items(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	var videoIDs []string
	for _, item := range videoPage["Items"].([]map[string]any) {
		videoIDs = append(videoIDs, item["Id"].(string))
	}
	sort.Strings(videoIDs)
	if !reflect.DeepEqual(videoIDs, []string{"episode", "movie"}) {
		t.Fatalf("random Video changed file identities: %v", videoIDs)
	}
	p.IncludeItemTypes = []string{"Movie", "Series"}
	e.visibilityCache[e.repo.ReadCacheKey()+"viewer"] = embyVisibilityCacheEntry{visibility: MediaVisibility{LibraryRestricted: true}, expiresAt: time.Now().Add(time.Hour)}
	page, err := e.Items(t.Context(), p)
	if err != nil || len(page["Items"].([]map[string]any)) != 0 {
		t.Fatalf("restricted random: %v %v", page, err)
	}
}

func TestEmbyRandomSortUsesSeed(t *testing.T) {
	p := ItemsParams{SortBy: "Random,SortName", randomSeed: 42}
	for _, order := range []string{globalItemsOrder(p), metadataOrderSQL(p, false), seriesOrderSQL(p)} {
		if !strings.HasPrefix(order, "md5(") || !strings.Contains(order, "'42'") || strings.Contains(order, "created_at") {
			t.Fatalf("random fell back to dates: %s", order)
		}
	}
	if globalWorkDateAggregate(p) != "" {
		t.Fatal("random requests file dates")
	}
	other := p
	other.randomSeed = 43
	if globalItemsOrder(p) == globalItemsOrder(other) {
		t.Fatal("random seed has no effect")
	}
}
