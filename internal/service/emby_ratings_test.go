package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyItemPayloadDoubanRating(t *testing.T) {
	svc := &EmbyService{}
	for _, rating := range []float32{0, 8.6} {
		view := model.MediaView{Media: model.Media{DoubanRating: rating}, Rating: 6}
		item := svc.itemPayloadWithRelations(t.Context(), &view, "", false, 0, false, false, &embyItemRelations{})
		if item["CommunityRating"] != float32(6) {
			t.Fatalf("CommunityRating changed: %v", item["CommunityRating"])
		}
		got, exists := item["DoubanRating"]
		if exists != (rating > 0) || exists && got != rating {
			t.Fatalf("DoubanRating=%v exists=%v want=%v", got, exists, rating)
		}
	}
}

func TestEmbyDoubanRatingsDetailAndList(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	if err := db.AutoMigrate(&model.MetadataProviderSnapshot{}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Ratings", Path: "/fixture/ratings", Type: "tv", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	seriesID, seasonID := "ratings-series", "ratings-season"
	items := []model.MetadataItem{
		{PermanentBase: model.PermanentBase{ID: "ratings-movie"}, Kind: model.MetadataKindMovie},
		{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: model.MetadataKindSeries},
		{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: model.MetadataKindSeason, ParentID: &seriesID, SeasonNum: 1},
		{PermanentBase: model.PermanentBase{ID: "ratings-episode"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 1},
	}
	for i := range items {
		items[i].Title, items[i].Source, items[i].Rating = items[i].ID, "tmdb", 6
		createServiceTestMetadata(t, db, items[i], model.MetadataIdentifier{Provider: "douban", EntityKind: items[i].Kind, ExternalID: items[i].ID})
		if err := svc.repo.Metadata.UpsertProviderSnapshot(t.Context(), items[i].ID, "douban", []byte(fmt.Sprintf(`{"rating":{"value":%d}}`, 7+i)), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []int{0, 3} {
		media := model.Media{LibraryID: lib.ID, MetadataID: items[index].ID, Path: lib.Path + "/" + items[index].ID + ".mkv"}
		if index == 3 {
			media.SeasonNum, media.EpisodeNum = 1, 1
		}
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
	}
	group := embySeriesGroup{ID: seriesID, LibraryID: lib.ID, Summary: &embySeriesSummary{EpisodeCount: 1, SeasonCount: 1}}
	season := embySeasonGroup{ID: seasonID, SeriesID: seriesID, LibraryID: lib.ID, SeasonNum: 1, Series: group, EpisodeCount: 1}
	for _, phase := range []string{"rated", "missing"} {
		if phase == "missing" {
			// 孤立快照、无效评分和无快照均应省略，季/集不能借用仍有评分的整剧。
			if err := db.Where("metadata_id = ?", items[0].ID).Delete(&model.MetadataIdentifier{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.repo.Metadata.UpsertProviderSnapshot(t.Context(), seasonID, "douban", []byte(`{"rating":{"value":0}}`), time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := db.Where("metadata_id = ?", items[3].ID).Delete(&model.MetadataProviderSnapshot{}).Error; err != nil {
				t.Fatal(err)
			}
		}
		views, err := svc.repo.MediaView.FindByMetadataIDs(t.Context(), []string{items[0].ID, items[3].ID}, svc.mediaQueryFilter(t.Context(), ""))
		if err != nil {
			t.Fatal(err)
		}
		// 显式精简字段，不请求 ProviderIds；评分仍需返回。
		list := svc.payloadsForViewsWithFields(t.Context(), views, "", []string{"Overview"})
		list = append(list, svc.seriesPayloadsWithFields(t.Context(), []embySeriesGroup{group}, "", []string{"Overview"})...)
		list = append(list, svc.seasonPayloadsWithFields(t.Context(), []embySeasonGroup{season}, "", []string{"Overview"})...)
		if len(list) != 4 {
			t.Fatalf("%s: list length=%d", phase, len(list))
		}
		for _, row := range list {
			if _, exists := row["ProviderIds"]; exists {
				t.Fatal("list unexpectedly returned ProviderIds")
			}
			id := row["Id"].(string)
			detail, err := svc.Item(t.Context(), id, "")
			if err != nil || detail == nil {
				t.Fatalf("%s detail: %v", id, err)
			}
			for i, metadata := range items {
				if metadata.ID != id {
					continue
				}
				for _, payload := range []map[string]any{row, detail} {
					want := float32(7 + i)
					if phase == "missing" && id != seriesID {
						want = 0
					}
					got, exists := payload["DoubanRating"]
					if exists != (want > 0) || exists && got != want || payload["CommunityRating"] != float32(6) {
						t.Fatalf("%s/%s ratings: %v want douban=%v community=6", phase, id, payload, want)
					}
				}
			}
		}
	}
}
