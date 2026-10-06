package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHuangGuoAIStableIdentityAndPartialSummaries(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err = database.AutoMigrate(db); err != nil {
		t.Fatal("repeat migration:", err)
	}
	r := New(db).HuangGuoAI
	ctx := context.Background()
	finished := true
	total := 4
	summary := huangguoai.Summary{SourceID: "794", Category: "ai-mogai", Title: "Synthetic", Overview: "Full", EpisodeCount: 4, TotalEpisodes: &total, Finished: &finished, Tags: []string{"tag"}}
	if err = r.RegisterSummaries(ctx, []huangguoai.Summary{summary}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ReplaceRank(ctx, "hot", []huangguoai.Summary{{SourceID: "794", Category: "ai-mogai", Title: "Synthetic"}}); err != nil {
		t.Fatal(err)
	}
	rows, count, err := r.List(ctx, "", "ai-mogai", "tag", "", 1, 24)
	if err != nil || count != 1 || len(rows) != 1 || rows[0].Overview != "Full" || rows[0].EpisodeCount != 4 {
		t.Fatalf("partial overwrite/count: %d %v", count, err)
	}
	input := huangguoai.Work{Summary: summary, Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/794/"}}}
	first, _, err := r.SaveDetail(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	episodes, _, err := r.Episodes(ctx, "794", 1, 100)
	if err != nil || len(episodes) != 1 {
		t.Fatal(err)
	}
	epID := episodes[0].ID
	second, change, err := r.SaveDetail(ctx, input)
	if err != nil || second.ID != first.ID || second.Kind != "movie" || change != "unchanged" {
		t.Fatalf("identity/change: %s %v", change, err)
	}
	episodes, _, err = r.Episodes(ctx, "794", 1, 100)
	if err != nil || episodes[0].ID != epID {
		t.Fatal("episode identity changed")
	}
	ids, err := r.Pending(ctx, "", time.Now())
	if err != nil || len(ids) != 0 {
		t.Fatal("completed work scheduled")
	}
	var all int64
	db.Table("metadata_items").Count(&all)
	if all != 0 {
		t.Fatal("legacy metadata created")
	}
}

func TestHuangGuoAIArtworkHandoff(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.HuangGuoAIModels()...); err != nil {
		t.Fatal(err)
	}
	r := New(db).HuangGuoAI
	ctx := context.Background()
	save := func(url string) {
		t.Helper()
		if err := r.RegisterSummaries(ctx, []huangguoai.Summary{{SourceID: "1", Title: "Synthetic", Category: "ai-duanju", CoverURL: url}}); err != nil {
			t.Fatal(err)
		}
	}
	save("https://example.com/a")
	rows, err := r.DueArtwork(ctx, "", time.Now())
	if err != nil || len(rows) != 1 {
		t.Fatal("new image not scheduled", err)
	}
	old := rows[0]
	if err = r.FinishArtwork(ctx, old, "stored.jpg", nil); err != nil {
		t.Fatal(err)
	}
	save("https://example.com/b")
	rows, err = r.DueArtwork(ctx, "", time.Now())
	if err != nil || len(rows) != 1 || rows[0].LocalKey != "stored.jpg" {
		t.Fatal("changed image not scheduled", err)
	}
	if err = r.FinishArtwork(ctx, old, "stale.jpg", nil); err != nil {
		t.Fatal(err)
	}
	row, err := r.Artwork(ctx, old.ID)
	if err != nil || row.LocalKey != "stored.jpg" {
		t.Fatal("stale attempt committed")
	}
}

func TestHuangGuoAIBindingProjectionAndDerivedLibraries(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := New(db)
	ctx := context.Background()
	lib := model.Library{Name: "Synthetic", Type: model.LibraryTypeHuangGuoAI, Path: "/test/hga"}
	if err = db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	input := huangguoai.Summary{SourceID: "33", Title: "Synthetic", Category: "ai-duanju", EpisodeCount: 1}
	if err = repos.HuangGuoAI.RegisterSummaries(ctx, []huangguoai.Summary{input}); err != nil {
		t.Fatal(err)
	}
	work, _, err := repos.HuangGuoAI.SaveDetail(ctx, huangguoai.Work{Summary: input, Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/33/"}}})
	if err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Path: "/test/hga/[huangguoai-33] S01E001.mp4", CatalogSource: model.TaskSystemHuangGuoAI, LookupCatalogID: "33", SeasonNum: 1, EpisodeNum: 1}
	if err = repos.Media.Upsert(ctx, &media); err != nil {
		t.Fatal(err)
	}
	view, err := repos.MediaView.FindByID(ctx, media.ID)
	if err != nil || view == nil || view.SeriesID != "hga-group-33" || view.CatalogItemID == "" || view.MetadataKind != "episode" {
		t.Fatalf("source projection missing: %+v %v", view, err)
	}
	reloaded, err := repos.HuangGuoAI.FindBySourceID(ctx, "33")
	if err != nil || reloaded.LatestMediaAddedAt == nil || reloaded.LibraryIDs == nil || !strings.Contains(*reloaded.LibraryIDs, lib.ID) {
		t.Fatal("derived fields not refreshed", err)
	}
	page, stats, count, err := repos.MediaView.ListLibraryMetadataPage(ctx, lib.ID, "series", "", 0, 24, MediaQueryFilter{})
	if err != nil || count != 1 || len(page) != 1 || len(stats) != 1 || stats[0].Count != 1 {
		t.Fatalf("library page %d %v", count, err)
	}
	hidden, err := repos.MediaView.HuangGuoAIItemsViews(ctx, []string{view.CatalogItemID}, MediaQueryFilter{HiddenLibraryIDs: []string{lib.ID}})
	if err != nil || len(hidden) != 0 {
		t.Fatal("hidden files visible")
	}
	other := model.HuangGuoAIWork{SourceID: "44", SourceCategory: "ai-duanju", Kind: "series", Title: "Other", RefreshedAt: time.Now()}
	if err = db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	var binding model.HuangGuoAIMediaBinding
	db.Where("media_id=?", media.ID).Take(&binding)
	if err = db.Model(&binding).Update("work_id", other.ID).Error; err == nil {
		t.Fatal("cross-work episode binding accepted")
	}
	if err = db.Where("media_id=?", media.ID).Take(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if err = repos.Setting.Set(ctx, "huangguoai.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	newFile := model.Media{LibraryID: lib.ID, Path: "/test/hga/new [huangguoai-33] S01E001.mp4", CatalogSource: model.TaskSystemHuangGuoAI, LookupCatalogID: "33", SeasonNum: 1, EpisodeNum: 1}
	if err = repos.Media.Upsert(ctx, &newFile); err != nil {
		t.Fatal(err)
	}
	if err = repos.HuangGuoAI.RebindWork(ctx, "33"); err != nil {
		t.Fatal(err)
	}
	var countBindings int64
	if err = db.Model(&model.HuangGuoAIMediaBinding{}).Count(&countBindings).Error; err != nil || countBindings != 1 {
		t.Fatal("disabled source changed bindings", countBindings, err)
	}
	if preserved, err := repos.MediaView.FindByID(ctx, media.ID); err != nil || preserved == nil || preserved.CatalogItemID != view.CatalogItemID {
		t.Fatal("disabled source lost existing file", err)
	}
	if err = repos.Setting.Set(ctx, "huangguoai.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err = repos.HuangGuoAI.RebindWork(ctx, "33"); err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.HuangGuoAIMediaBinding{}).Count(&countBindings).Error; err != nil || countBindings != 2 {
		t.Fatal("reenabled source did not bind pending file", countBindings, err)
	}
	if work.ID != binding.WorkID {
		t.Fatal("unexpected binding work")
	}
}

func TestHuangGuoAIListDownloadedBadge(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.HuangGuoAIModels()...); err != nil {
		t.Fatal(err)
	}
	r := New(db).HuangGuoAI
	ctx := context.Background()
	summaries := []huangguoai.Summary{{SourceID: "1", Category: "ai-duanju", Title: "Synthetic One"}, {SourceID: "2", Category: "ai-duanju", Title: "Synthetic Two"}}
	if err = r.RegisterSummaries(ctx, summaries); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.SaveDetail(ctx, huangguoai.Work{Summary: summaries[0], Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/1/"}}}); err != nil {
		t.Fatal(err)
	}
	tasks := []model.HuangGuoAIDownload{{SourceID: "1", Episode: 1, Status: "completed"}, {SourceID: "1", Episode: 2, Status: "failed"}, {SourceID: "2", Episode: 1, Status: "failed"}, {SourceID: "999", Episode: 1, Status: "completed"}}
	if err = db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	rows, total, err := r.List(ctx, "", "", "", "", 1, 50)
	if err != nil || total != 2 {
		t.Fatalf("list: %d %v", total, err)
	}
	for _, row := range rows {
		if row.Downloaded != (row.SourceID == "1") {
			t.Fatalf("badge: %s %v", row.SourceID, row.Downloaded)
		}
	}
	remote, err := r.SearchResults(ctx, summaries)
	if err != nil || len(remote) != 2 || !remote[0].Downloaded || remote[1].Downloaded {
		t.Fatal("search badge", err)
	}
	detail, err := r.Detail(ctx, "1")
	if err != nil || !detail.Downloaded {
		t.Fatal("detail badge", err)
	}
	if err = db.Model(&tasks[0]).Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	detail, err = r.Detail(ctx, "1")
	if err != nil || detail.Downloaded {
		t.Fatal("stale badge", err)
	}
}
