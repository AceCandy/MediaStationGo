package service

import (
	"fmt"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestStorageBreakdownCountsLibraryMetadata(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{})
	// 此测试需要保留未匹配文件，不启用旧夹具的自动 metadata 补齐。
	if err := db.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	svc := NewStorageService(repository.New(db))
	empty, err := svc.Compute(t.Context())
	if err != nil || empty == nil || empty.ByLibrary == nil || len(empty.ByLibrary) != 0 || empty.TotalBytes != 0 {
		t.Fatalf("empty breakdown = %#v, err = %v", empty, err)
	}
	libs := []model.Library{
		{Name: "记录电影", Path: "/media/纪录片", Type: "movie", Enabled: true},
		{Name: "欧美动漫", Path: "/media/欧美动漫", Type: "tv", Enabled: true},
		{Name: "空库", Path: "/media/empty", Type: "movie", Enabled: false},
		{Name: "已删除", Path: "/media/deleted", Type: "movie", Enabled: true},
	}
	for i := range libs {
		if err := db.Create(&libs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	movie := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "movie", Title: "电影", Source: "local"})
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "剧", Source: "local"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1, Title: "第一季", Source: "local"})
	specials := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 0, Title: "特别篇", Source: "local"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1, Title: "第一集", Source: "local"})
	special := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &specials.ID, EpisodeNum: 1, Title: "特别篇第一集", Source: "local"})
	// 目录中的未入库电影、季和集不应进入计数。
	createServiceTestMetadata(t, db, model.MetadataItem{Kind: "movie", Title: "目录电影", Source: "local"})
	createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 2, Title: "目录季", Source: "local"})
	createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 2, Title: "目录集", Source: "local"})
	files := []struct {
		library  int
		metadata string
	}{
		{0, movie.ID}, {0, movie.ID}, {0, ""},
		{1, episode.ID}, {1, episode.ID}, {1, special.ID},
		{1, season.ID}, {1, series.ID}, {1, movie.ID},
		{3, movie.ID},
	}
	for i, file := range files {
		media := model.Media{LibraryID: libs[file.library].ID, MetadataID: file.metadata, Path: fmt.Sprintf("/media/file-%d.mkv", i)}
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: "{}", SchemaVersion: 1, SizeBytes: 100}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&libs[3]).Error; err != nil {
		t.Fatal(err)
	}
	got, err := svc.Compute(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ByLibrary) != 3 || got.TotalBytes != 900 {
		t.Fatalf("breakdown = %#v", got)
	}
	want := []LibraryUsage{
		{LibraryID: libs[0].ID, Name: libs[0].Name, Type: libs[0].Type, Path: libs[0].Path, MovieCount: 1, TotalBytes: 300},
		{LibraryID: libs[1].ID, Name: libs[1].Name, Type: libs[1].Type, Path: libs[1].Path, MovieCount: 1, SeriesCount: 1, SeasonCount: 2, EpisodeCount: 2, TotalBytes: 600},
		{LibraryID: libs[2].ID, Name: libs[2].Name, Type: libs[2].Type, Path: libs[2].Path},
	}
	for i := range want {
		if got.ByLibrary[i] != want[i] {
			t.Fatalf("library %d = %#v, want %#v", i, got.ByLibrary[i], want[i])
		}
	}
}
