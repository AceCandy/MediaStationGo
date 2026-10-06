package service

import (
	"fmt"
	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestHuangGuoAIEmbyPlaybackHierarchyAndPermissions(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	ctx := t.Context()
	lib := model.Library{Name: "Synthetic", Type: model.LibraryTypeHuangGuoAI, Path: "/synthetic/hga"}
	if err = db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{repos.ReadCacheKey() + "viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}, repos.ReadCacheKey() + "locked": {visibility: MediaVisibility{IncludeNSFW: false, HiddenLibraryIDs: []string{lib.ID}}, expiresAt: time.Now().Add(time.Hour)}}
	var files []model.Media
	var workID string
	for _, input := range []huangguoai.Work{{Summary: huangguoai.Summary{SourceID: "71", Category: "ai-duanju", Title: "Synthetic Series"}, Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/71/"}, {Number: 3, PagePath: "/video/71/ep-3/"}}}, {Summary: huangguoai.Summary{SourceID: "72", Category: "ai-mogai", Title: "Synthetic Movie"}, Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/72/"}}}} {
		if err = repos.HuangGuoAI.RegisterSummaries(ctx, []huangguoai.Summary{input.Summary}); err != nil {
			t.Fatal(err)
		}
		work, _, err := repos.HuangGuoAI.SaveDetail(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if input.SourceID == "71" {
			workID = work.ID
		}
		for _, ep := range input.Episodes {
			m := model.Media{LibraryID: lib.ID, Path: fmt.Sprintf("/synthetic/hga/%s-%d.mp4", input.SourceID, ep.Number), CatalogSource: "huangguoai", LookupCatalogID: input.SourceID, SeasonNum: 1, EpisodeNum: ep.Number}
			if err = repos.Media.Upsert(ctx, &m); err != nil {
				t.Fatal(err)
			}
			if err = db.Create(&model.MediaProbeMetadata{MediaID: m.ID, DurationMS: 100000}).Error; err != nil {
				t.Fatal(err)
			}
			files = append(files, m)
		}
	}
	for _, p := range []ItemsParams{{UserID: "viewer", ParentID: lib.ID, Limit: 20}, {UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, Limit: 20, SortBy: "SortName"}, {UserID: "viewer", SearchTerm: "Synthetic", IncludeItemTypes: []string{"Movie", "Series"}, Limit: 20}} {
		page, err := e.Items(ctx, p)
		if err != nil {
			t.Fatal("items", err)
		}
		if len(page["Items"].([]map[string]any)) != 2 {
			t.Fatal("work count", page)
		}
	}
	series, err := e.Item(ctx, "hga-group-71", "viewer")
	if err != nil || series == nil || series["Type"] != "Series" {
		t.Fatal("series", err)
	}
	seasonID := "hga-season-" + workID
	season, err := e.Item(ctx, seasonID, "viewer")
	if err != nil || season == nil || season["ParentId"] != "hga-group-71" || season["Name"] != "第 1 季" || season["SeriesName"] != "Synthetic Series" {
		t.Fatal("season", err)
	}
	page, err := e.Items(ctx, ItemsParams{UserID: "viewer", ParentID: seasonID, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page["Items"].([]map[string]any)) != 2 {
		t.Fatal("episodes", page)
	}
	view, err := repos.MediaView.FindByID(ctx, files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.RecordProgress(ctx, "viewer", view.CatalogItemID, view.ID, "synthetic-session", 95000*10000, 100000*10000); err != nil {
		t.Fatal("record", err)
	}
	var original model.HuangGuoAIUserState
	if err = db.Where("user_id=? AND source_id=? AND episode_number=1", "viewer", "71").Take(&original).Error; err != nil {
		t.Fatal(err)
	}
	if err = repos.HuangGuoAI.MarkPreviousEpisodes(ctx, "viewer", "71", 3, repository.MediaQueryFilter{}); err != nil {
		t.Fatal(err)
	}
	var preserved model.HuangGuoAIUserState
	if err = db.Where("user_id=? AND source_id=? AND episode_number=1", "viewer", "71").Take(&preserved).Error; err != nil || preserved.PositionMs != original.PositionMs || !preserved.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatal("auto-mark overwrote completed history", err)
	}
	next, err := e.NextUpItems(ctx, ItemsParams{UserID: "viewer", Limit: 20})
	if err != nil {
		t.Fatal("next", err)
	}
	items := next["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["IndexNumber"] != 3 {
		t.Fatal("actual successor", next)
	}
	if err = e.SetFavorite(ctx, "viewer", "hga-group-71", true); err != nil {
		t.Fatal(err)
	}
	favorites, err := e.Items(ctx, ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, Filters: []string{"IsFavorite"}, Limit: 20})
	if err != nil {
		t.Fatal("favorite page", err)
	}
	favoriteItems := favorites["Items"].([]map[string]any)
	if len(favoriteItems) != 1 || len(favoriteItems[0]["LibraryIds"].([]string)) != 1 {
		t.Fatal("favorite memberships", favorites)
	}
	counts, err := e.ItemCounts(ctx, "viewer")
	if err != nil || counts["MovieCount"] != int64(1) || counts["EpisodeCount"] != int64(2) || counts["SeriesCount"] != 1 {
		t.Fatal("item counts", counts, err)
	}
	movie, err := repos.MediaView.FindByID(ctx, files[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.RecordProgress(ctx, "viewer", movie.CatalogItemID, movie.ID, "movie-session", 30000*10000, 100000*10000); err != nil {
		t.Fatal(err)
	}
	resume, err := e.Items(ctx, ItemsParams{UserID: "viewer", Recursive: true, Filters: []string{"IsResumable"}, Limit: 20})
	if err != nil || len(resume["Items"].([]map[string]any)) != 1 {
		t.Fatal("global resumable", resume, err)
	}
	mediaSvc := &MediaService{repo: repos}
	seriesView, err := mediaSvc.GetMediaSeriesVisible(ctx, files[0].ID, MediaVisibility{IncludeNSFW: true})
	if err != nil || seriesView == nil || seriesView.LookupCatalogID != "71" {
		t.Fatal("web series presentation", err)
	}
	seasonView, err := mediaSvc.GetMediaSeasonVisible(ctx, files[0].ID, MediaVisibility{IncludeNSFW: true})
	if err != nil || seasonView == nil || seasonView.SeasonNum != 1 {
		t.Fatal("web season presentation", err)
	}
	if err = e.MarkPlayed(ctx, "viewer", "hga-group-71", true); err != nil {
		t.Fatal(err)
	}
	if item, err := e.Item(ctx, "hga-group-71", "locked"); err != nil || item != nil {
		t.Fatal("hidden item", err)
	}
	latest, err := e.LatestItems(ctx, "viewer", "", 20, false)
	if err != nil {
		t.Fatal("latest", err)
	}
	if len(latest) != 1 || latest[0]["Type"] != "Movie" {
		t.Fatal("latest played filter", latest)
	}
	if _, err = repos.PlaybackStats(ctx, "huangguoai", repository.PlaybackStatsFilter{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), RankFrom: time.Now().Add(-time.Hour), RankTo: time.Now().Add(time.Hour), Grain: "day", TimeZone: "UTC", Page: 1, PageSize: 20}); err != nil {
		t.Fatal("stats", err)
	}
}
