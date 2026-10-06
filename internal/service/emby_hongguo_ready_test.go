package service

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestHongGuoListsWaitForAlbumSupplement(t *testing.T) {
	e := newTestEmbyService(t)
	db, ctx := e.repo.DB, t.Context()
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Base: model.Base{ID: "ready-library"}, Name: "红果", Type: model.LibraryTypeHongGuo, Path: "/fixture/ready"}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	for _, sql := range []string{
		`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at) VALUES
('ready','1001','series','Ready','1001',1,now()),('pending','1002','series','Pending','',0,now()),
('season-pending','1003','series','Season pending','1003',0,now()),('short','1004','series','Short','1004',1,now())`,
		`INSERT INTO hongguo_episodes(id,work_id,number) SELECT 'ep-'||id,id,1 FROM hongguo_works WHERE kind='series'`,
		`INSERT INTO media(id,path,library_id,catalog_source,created_at) SELECT 'file-'||id,'/fixture/ready/'||id,'ready-library','hongguo',now() FROM hongguo_works`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) SELECT 'file-'||id,id,CASE WHEN kind='series' THEN 'ep-'||id END FROM hongguo_works`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, supplemented := range []bool{false, true} {
		want := map[string]bool{"hg-group-1001": true, "hg-group-1004": true}
		if supplemented {
			for _, source := range []string{"1002", "1003"} {
				if err := e.repo.HongGuo.SaveAlbum(ctx, source, hongguo.Album{ID: source, Season: 1}); err != nil {
					t.Fatal(err)
				}
				want["hg-group-"+source] = true
			}
		}
		check := func(name string, ids []string, total int64, err error) {
			t.Helper()
			seen := map[string]bool{}
			for _, id := range ids {
				seen[id] = true
			}
			if err != nil || total != int64(len(want)) || len(seen) != len(want) {
				t.Fatalf("%s supplemented=%v ids=%v total=%d err=%v", name, supplemented, ids, total, err)
			}
			for id := range seen {
				if !want[id] {
					t.Fatalf("%s unexpected identity %s", name, id)
				}
			}
		}
		for _, parent := range []string{lib.ID, ""} {
			page, err := e.Items(ctx, ItemsParams{UserID: "viewer", ParentID: parent, Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, SortBy: "SortName", Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, item := range page["Items"].([]map[string]any) {
				ids = append(ids, item["Id"].(string))
			}
			check("emby "+parent, ids, page["TotalRecordCount"].(int64), nil)
		}
		_, summaries, total, err := e.repo.MediaView.ListLibraryMetadataPage(ctx, lib.ID, "series", "", 0, 10, repository.MediaQueryFilter{})
		ids := []string{}
		for _, row := range summaries {
			ids = append(ids, row.MetadataID)
		}
		check("web library", ids, total, err)
		views, err := e.repo.MediaView.ListRecentLogicalWorks(ctx, 10, repository.MediaQueryFilter{})
		ids = nil
		for _, view := range views {
			id := view.SeriesID
			if id == "" {
				id = view.CatalogItemID
			}
			ids = append(ids, id)
		}
		check("web recent", ids, int64(len(views)), err)
		latest, err := e.LatestItems(ctx, "viewer", lib.ID, 10, false)
		ids = nil
		for _, item := range latest {
			ids = append(ids, item["Id"].(string))
		}
		check("emby latest", ids, int64(len(latest)), err)
	}
}
