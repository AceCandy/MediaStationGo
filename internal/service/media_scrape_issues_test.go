package service

import (
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestListScrapeIssuesFiltersAndSanitizesReasons(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	service := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	normal := model.Library{Name: "电影", Path: "/media/movies", Type: "movie", Enabled: true}
	nfo := model.Library{Name: "个人短片", Path: "/media/clips", Type: model.LibraryTypeNFOMovie, Enabled: true}
	if err := db.Create(&[]*model.Library{&normal, &nfo}).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: normal.ID, Title: "失败电影", Path: "/media/movies/error.mkv", ScrapeStatus: "error", ScrapeError: "request https://example.test/path?token=secret failed"},
		{LibraryID: normal.ID, Title: "未匹配电影", Path: "/media/movies/no-match.mkv", ScrapeStatus: "no_match"},
		{LibraryID: nfo.ID, Title: "无 NFO 短片", Path: "/media/clips/no-nfo.mkv", ScrapeStatus: "no_match"},
		{LibraryID: normal.ID, Title: "成功电影", Path: "/media/movies/matched.mkv", ScrapeStatus: "matched"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	page, err := service.ListScrapeIssues(t.Context(), "", "", nil, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("page = %#v, want 3 unresolved rows", page)
	}
	if emptyFilterPage, err := service.ListScrapeIssues(t.Context(), "", "", []string{""}, 1, 10); err != nil || emptyFilterPage.Total != page.Total {
		t.Fatalf("empty status filter page = %#v, err = %v", emptyFilterPage, err)
	}
	byID := make(map[string]MediaScrapeIssue, len(page.Items))
	for _, item := range page.Items {
		byID[item.ID] = item
	}
	if got := byID[rows[0].ID].Path; got != rows[0].Path {
		t.Fatalf("media path = %q, want %q", got, rows[0].Path)
	}
	if got := byID[rows[0].ID].Reason; strings.Contains(got, "token=secret") || !strings.Contains(got, "[redacted-url]") {
		t.Fatalf("unsanitized reason = %q", got)
	}
	if got := byID[rows[1].ID].Reason; got != "未找到匹配元数据" {
		t.Fatalf("normal no-match reason = %q", got)
	}
	if got := byID[rows[2].ID].Reason; got != "未找到本地 NFO" {
		t.Fatalf("NFO no-match reason = %q", got)
	}

	page, err = service.ListScrapeIssues(t.Context(), normal.ID, "", []string{"error"}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != rows[0].ID || page.PageSize != 1 {
		t.Fatalf("filtered page = %#v", page)
	}
	if page, err = service.ListScrapeIssues(t.Context(), "", "个人短片", nil, 1, 10); err != nil || page.Total != 1 || page.Items[0].ID != rows[2].ID {
		t.Fatalf("library search page = %#v, err = %v", page, err)
	}
	if page, err = service.ListScrapeIssues(t.Context(), "", "no-match.mkv", nil, 1, 10); err != nil || page.Total != 1 || page.Items[0].ID != rows[1].ID {
		t.Fatalf("path search page = %#v, err = %v", page, err)
	}
	if page, err = service.ListScrapeIssues(t.Context(), "", "missing", nil, 1, 10); err != nil || page.Total != 0 {
		t.Fatalf("empty search page = %#v, err = %v", page, err)
	}
	if _, err := service.ListScrapeIssues(t.Context(), "", "", []string{"matched"}, 1, 10); !errors.Is(err, ErrInvalidScrapeIssueStatus) {
		t.Fatalf("invalid status error = %v", err)
	}
}
