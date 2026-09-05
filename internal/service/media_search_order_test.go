package service

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestSearchPreservesMetadataRankAcrossWebAndEmby(t *testing.T) {
	emby := newTestEmbyService(t)
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := emby.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	titles := []string{"世外", "世外逃劫", "其他电影"}
	for i, title := range titles {
		metadata := createServiceTestMetadata(t, emby.repo.DB, model.MetadataItem{
			Kind: model.MetadataKindMovie, Title: title, Overview: "世外的故事", Source: "local",
		})
		// 相关性越低的条目越晚入库，以识别按文件时间覆盖搜索排序的问题。
		created := time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC)
		if err := emby.repo.DB.Create(&model.Media{
			PermanentBase: model.PermanentBase{CreatedAt: created, UpdatedAt: created},
			LibraryID:     lib.ID, MetadataID: metadata.ID, Path: lib.Path + "/" + metadata.ID + ".mkv",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	web := NewMediaService(emby.cfg, emby.log, emby.repo)
	visibility := MediaVisibility{IncludeNSFW: true}
	for _, size := range []int{2, 3} {
		page, total, err := web.SearchMediaVisiblePageGrouped(t.Context(), "世外", 1, size, visibility)
		if err != nil || total != 3 || len(page) != size {
			t.Fatalf("Web page: len=%d total=%d err=%v", len(page), total, err)
		}
		for i, item := range page {
			if item.Title != titles[i] {
				t.Fatalf("Web result %d = %q, want %q", i, item.Title, titles[i])
			}
		}
	}
	page, total, err := web.SearchMediaVisiblePageGrouped(t.Context(), "世外", 2, 2, visibility)
	if err != nil || total != 3 || len(page) != 1 || page[0].Title != titles[2] {
		t.Fatalf("Web second page = %#v, total=%d err=%v", page, total, err)
	}
	suggestions, err := web.SearchMediaVisibleGrouped(t.Context(), "世外", 3, visibility)
	if err != nil || len(suggestions) != 3 {
		t.Fatalf("Web suggestions: len=%d err=%v", len(suggestions), err)
	}
	for i, item := range suggestions {
		if item.Title != titles[i] {
			t.Fatalf("Web suggestion %d = %q, want %q", i, item.Title, titles[i])
		}
	}
	params := ItemsParams{SearchTerm: "世外", IncludeItemTypes: []string{"Movie"}, Limit: 3}
	items, err := emby.Items(t.Context(), params)
	if err != nil {
		t.Fatal(err)
	}
	hints, err := emby.SearchHints(t.Context(), params)
	if err != nil {
		t.Fatal(err)
	}
	for name, results := range map[string][]map[string]any{
		"Items":       items["Items"].([]map[string]any),
		"SearchHints": hints["SearchHints"].([]map[string]any),
	} {
		if len(results) != 2 {
			t.Fatalf("Emby %s = %#v, want two title matches", name, results)
		}
		for i, item := range results {
			if item["Name"] != titles[i] {
				t.Fatalf("Emby %s result %d = %v, want %q", name, i, item["Name"], titles[i])
			}
		}
	}
}
