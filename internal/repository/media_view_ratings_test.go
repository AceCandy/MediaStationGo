package repository

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestDoubanSnapshotRating(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    float32
	}{
		{`{"rating":{"value":8.6}}`, 8.6},
		{`{"subject":{"rating":"7.5"}}`, 7.5},
		{`{"data":{"rate":9.1}}`, 9.1},
		{`{"rating":{"score":8}}`, 8},
		{`{"rating":null}`, 0},
		{`{"rating":0}`, 0},
		{`{"rating":-1}`, 0},
		{`{"rating":11}`, 0},
		{`{"rating":"NaN"}`, 0},
		{`{"rating":"暂无评分"}`, 0},
		{`{}`, 0},
		{`invalid`, 0},
	} {
		if got := doubanSnapshotRating(tc.payload); got != tc.want {
			t.Errorf("rating(%s) = %v, want %v", tc.payload, got, tc.want)
		}
	}
}

func TestMediaViewDoubanRatings(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	lib := model.Library{Name: "Ratings", Path: "/ratings", Type: "movie"}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"rated", "unrated", "unlinked"} {
		metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindMovie, Title: id, Rating: 6, Source: "tmdb"}
		if err := repos.DB.Create(&metadata).Error; err != nil {
			t.Fatal(err)
		}
		media := model.Media{PermanentBase: model.PermanentBase{ID: "media-" + id}, LibraryID: lib.ID, MetadataID: id, Path: "/ratings/" + id}
		if err := repos.DB.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		if id != "unlinked" {
			identifier := model.MetadataIdentifier{MetadataID: id, Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: id}
			if err := repos.DB.Create(&identifier).Error; err != nil {
				t.Fatal(err)
			}
		}
		if id != "unrated" {
			if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), id, "douban", []byte(`{"rating":{"value":8.6}}`), time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, total, err := repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{lib.ID}, 0, 10, MediaQueryFilter{})
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("list: total=%d rows=%d err=%v", total, len(rows), err)
	}
	for _, row := range rows {
		want := float32(0)
		if row.MetadataID == "rated" {
			want = 8.6
		}
		if row.DoubanRating != want || row.Rating != 6 {
			t.Errorf("%s: douban=%v rating=%v", row.MetadataID, row.DoubanRating, row.Rating)
		}
	}
	rows, err = repos.MediaView.FindMetadataSearchRepresentatives(t.Context(), []string{"rated"}, MediaQueryFilter{})
	if err != nil || len(rows) != 1 || rows[0].DoubanRating != 8.6 {
		t.Fatalf("search rating missing: rows=%v err=%v", rows, err)
	}
	if got := mediaViewsToMedia(rows)[0].DoubanRating; got != 8.6 {
		t.Fatalf("media conversion lost rating: %v", got)
	}
	applyMetadataSearchPresentation(&rows[0], metadataSearchPresentation{ID: "unrated", DoubanExternalID: "unrated", Kind: model.MetadataKindSeries})
	if err := attachMediaViewDoubanRatings(repos.DB, rows); err != nil || rows[0].DoubanRating != 0 {
		t.Fatalf("metadata switch retained previous rating: %v err=%v", rows[0].DoubanRating, err)
	}
}
