package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyItemPayloadHierarchyFields(t *testing.T) {
	svc := newTestEmbyService(t)
	for _, tc := range []struct {
		name       string
		source     string
		episode    bool
		season     int
		seasonName string
	}{
		{name: "movie"},
		{name: "nfo_movie", source: model.CatalogSourceNFO},
		{name: "episode", episode: true, season: 1, seasonName: "第 1 季"},
		{name: "special", episode: true, seasonName: "特别篇"},
		{name: "nfo_episode", source: model.CatalogSourceNFO, episode: true, season: 1, seasonName: "第 1 季"},
		{name: "nfo_special", source: model.CatalogSourceNFO, episode: true, seasonName: "特别篇"},
		{name: "hongguo_episode", source: model.TaskSystemHongGuo, episode: true, season: 1, seasonName: "第 1 季"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := model.MediaView{
				Media: model.Media{
					PermanentBase: model.PermanentBase{ID: "payload-media"},
					MetadataID:    "payload-metadata", LibraryID: "payload-library",
					CatalogSource: tc.source, Path: "/media/movies/movie.mkv",
				},
				CatalogItemID: "payload-catalog", Title: "示例影片", MetadataKind: model.MetadataKindMovie,
			}
			wantType, wantParent := "Movie", view.LibraryID
			if tc.episode {
				view.MetadataKind = model.MetadataKindEpisode
				view.SeriesID, view.SeriesTitle, view.SeasonID = "payload-series", "示例剧集", "payload-season"
				view.SeasonNum, view.EpisodeNum = tc.season, 1
				view.Media.SeasonNum, view.Media.EpisodeNum = tc.season, 1
				view.Path = "/media/shows/Season 1/E01.mkv"
				wantType, wantParent = "Episode", view.SeasonID
			}
			wantHierarchy := map[string]any{
				"SeriesId": view.SeriesID, "SeriesName": view.SeriesTitle,
				"SeasonId": view.SeasonID, "SeasonName": tc.seasonName,
				"ParentIndexNumber": tc.season, "IndexNumber": 1,
			}
			for _, mode := range []string{"detail", "list"} {
				t.Run(mode, func(t *testing.T) {
					var relations *embyItemRelations
					if mode == "list" {
						relations = &embyItemRelations{episodeByMediaID: map[string]bool{view.ID: tc.episode}}
					}
					item := svc.itemPayloadWithRelations(t.Context(), &view, "", false, 0, false, mode == "detail", relations)
					if item["Type"] != wantType || item["ParentId"] != wantParent {
						t.Fatalf("Type/ParentId = %v/%v, want %s/%s", item["Type"], item["ParentId"], wantType, wantParent)
					}
					for key, want := range wantHierarchy {
						got, exists := item[key]
						if !tc.episode && exists {
							t.Errorf("Movie must omit %s, got %#v", key, got)
						} else if tc.episode && (!exists || got != want) {
							t.Errorf("Episode %s = %#v (exists=%v), want %#v", key, got, exists, want)
						}
					}
				})
			}
		})
	}
}
