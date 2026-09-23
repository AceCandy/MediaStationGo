package service

import (
	"fmt"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestHongGuoLibrarySeriesPresentation(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	ctx := t.Context()
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	library := model.Library{Name: "红果", Path: "/fixture/hongguo", Type: model.LibraryTypeHongGuo}
	hidden := model.Library{Name: "隐藏库", Path: "/fixture/hidden", Type: model.LibraryTypeHongGuo}
	for _, lib := range []*model.Library{&library, &hidden} {
		if err := repos.Library.Create(ctx, lib); err != nil {
			t.Fatal(err)
		}
	}
	works := make([]*model.HongGuoWork, 4)
	for i := range works {
		work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{
			SourceID: fmt.Sprint(91001 + i), Title: fmt.Sprintf("主体%d", i+1), Overview: fmt.Sprintf("简介%d", i+1),
			Tags: []string{fmt.Sprintf("标签%d", i+1)}, Rating: float32(8 + i), EpisodeCount: 2,
			People: []hongguo.Person{{SourceID: fmt.Sprint(92001 + i), Name: fmt.Sprintf("演员%d", i+1)}}, Snapshot: []byte(`{}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		season := i + 1
		if i == 3 {
			season = 3 // 相同季号仍是两个来源作品，不能合并分集身份。
		}
		if err := repos.HongGuo.SaveAlbum(ctx, work.SourceID, hongguo.Album{ID: "93001", Season: season}); err != nil {
			t.Fatal(err)
		}
		works[i] = work
	}
	poster := model.HongGuoArtwork{WorkID: &works[0].ID, LocalKey: "fixture.jpg"}
	if err := db.Create(&poster).Error; err != nil {
		t.Fatal(err)
	}
	addFile := func(lib model.Library, work *model.HongGuoWork, episode int, suffix string) model.Media {
		t.Helper()
		media := model.Media{LibraryID: lib.ID, Path: fmt.Sprintf("%s/%s/S01E%02d-%s.mkv", lib.Path, work.SourceID, episode, suffix),
			CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: work.SourceID, SeasonNum: 1, EpisodeNum: episode}
		if err := repos.Media.Upsert(ctx, &media); err != nil {
			t.Fatal(err)
		}
		return media
	}
	second := addFile(library, works[1], 1, "a")
	alternate := addFile(library, works[1], 1, "b")
	third := addFile(library, works[2], 1, "a")
	addFile(library, works[3], 1, "a")
	addFile(hidden, works[1], 2, "hidden")
	visibility := MediaVisibility{HiddenLibraryIDs: []string{hidden.ID}}
	filter := repository.MediaQueryFilter{HiddenLibraryIDs: []string{hidden.ID}}
	const seriesID = "hg-group-93001"
	cards, total, err := svc.ListLibrarySeriesCards(ctx, library.ID, 1, 50, seriesID, "", visibility)
	if err != nil || total != 1 || len(cards) != 1 {
		t.Fatalf("cards=%+v total=%d err=%v", cards, total, err)
	}
	card := cards[0]
	if card.Count != 3 || card.Rep.Title != works[0].Title || card.Rep.Overview != works[0].Overview || card.Rep.Rating != works[0].Rating || card.Rep.Genres != "标签1" || card.Rep.PosterURL != "/api/catalogs/hongguo/artwork/"+poster.ID {
		t.Fatalf("wrong first-season presentation or duplicate count: %+v", card)
	}
	if len(card.Seasons) != 2 || card.Seasons[0] != 2 || card.Seasons[1] != 3 || card.Rep.MetadataID != "" || card.Rep.Year != 0 || card.Rep.ReleaseDate != "" {
		t.Fatalf("invented metadata or unavailable season: %+v", card)
	}
	series, err := svc.GetMediaSeriesVisible(ctx, second.ID, visibility)
	if err != nil || series == nil || series.ID != seriesID || series.LookupCatalogID != works[0].SourceID || series.Title != works[0].Title {
		t.Fatalf("series=%+v err=%v", series, err)
	}
	credits, err := svc.ListMetadataCredits(ctx, "hongguo:"+series.LookupCatalogID)
	if err != nil || len(credits) != 1 || credits[0].Name != "演员1" {
		t.Fatalf("credits=%+v err=%v", credits, err)
	}
	season, err := svc.GetMediaSeasonVisible(ctx, second.ID, visibility)
	if err != nil || season == nil || season.SeasonNum != 2 || season.Title != works[1].Title || season.Overview != works[1].Overview || season.PosterURL != "" {
		t.Fatalf("season borrowed series metadata: %+v err=%v", season, err)
	}
	seasonNumber := 3
	episodes, err := svc.ListLibrarySeriesEpisodes(ctx, library.ID, "metadata:"+seriesID, &seasonNumber, visibility)
	if err != nil || len(episodes) != 2 || episodes[0].CatalogItemID == episodes[1].CatalogItemID {
		t.Fatalf("same-season sources merged: %+v err=%v", episodes, err)
	}
	for _, ep := range episodes {
		if ep.SeasonNum != 3 || ep.EpisodeNum != 1 || ep.Overview != "" || ep.BackdropURL != "" || ep.ReleaseDate != "" || ep.MetadataID != "" {
			t.Fatalf("incorrect episode projection: %+v", ep)
		}
	}
	view := serviceTestMediaView(t, repos, alternate.ID)
	if err := repos.HongGuo.RecordProgress(ctx, "viewer", "", *view, 30000, 120000, false); err != nil {
		t.Fatal(err)
	}
	versions, err := svc.ListMediaVersions(ctx, second.ID, "viewer", visibility)
	if err != nil || len(versions) != 2 || versions[0].ID != alternate.ID {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	history, err := repos.History.ListByUserMetadataIDs(ctx, "viewer", []string{view.CatalogItemID}, filter)
	if err != nil || len(history) != 1 || history[0].MetadataID != view.CatalogItemID || history[0].MediaID != alternate.ID || history[0].PositionMs != 30000 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	otherHistory, err := repos.History.ListByUserMetadataIDs(ctx, "other", []string{view.CatalogItemID}, filter)
	if err != nil || len(otherHistory) != 0 {
		t.Fatalf("other user history leaked: %+v err=%v", otherHistory, err)
	}
	if err := repos.HongGuo.MarkPlayed(ctx, "viewer", *view, true); err != nil {
		t.Fatal(err)
	}
	resume, err := NewPlaybackService(zap.NewNop(), repos).ContinueSeriesHistory(ctx, "viewer", library.ID, seriesID, visibility)
	if err != nil || resume == nil || !resume.IsNext || resume.Media == nil || resume.Media.ID != third.ID {
		t.Fatalf("cross-season resume=%+v err=%v", resume, err)
	}
	for _, favorite := range []bool{true, false} {
		if _, err := repos.MediaView.HongGuoSeriesFavorite(ctx, "viewer", seriesID, filter, &favorite); err != nil {
			t.Fatal(err)
		}
		got, err := repos.MediaView.HongGuoSeriesFavorite(ctx, "viewer", seriesID, filter, nil)
		if err != nil || got != favorite {
			t.Fatalf("favorite=%v err=%v", got, err)
		}
		other, err := repos.MediaView.HongGuoSeriesFavorite(ctx, "other", seriesID, filter, nil)
		if err != nil || other {
			t.Fatalf("other user favorite changed: %v %v", other, err)
		}
	}
	legacy, _, err := svc.ListLibrarySeriesCards(ctx, library.ID, 1, 50, "", "hongguo:"+works[1].SourceID, visibility)
	if err != nil || len(legacy) != 1 || legacy[0].Rep.SeriesID != seriesID {
		t.Fatalf("legacy link=%+v err=%v", legacy, err)
	}
	for _, denied := range []MediaVisibility{{HiddenLibraryIDs: []string{library.ID}}, {AllowedLibraryIDs: []string{hidden.ID}}, {LibraryRestricted: true}} {
		cards, n, err := svc.ListLibrarySeriesCards(ctx, library.ID, 1, 50, "", "", denied)
		if err != nil || n != 0 || len(cards) != 0 {
			t.Fatalf("library visibility leaked: %+v %d %v", cards, n, err)
		}
		got, err := svc.GetMediaSeriesVisible(ctx, third.ID, denied)
		if err != nil || got != nil {
			t.Fatalf("detail visibility leaked: %+v %v", got, err)
		}
	}
	for _, missing := range []MediaVisibility{{MissingPoster: true}, {MissingChineseTitle: true}} {
		cards, n, err := svc.ListLibrarySeriesCards(ctx, library.ID, 1, 50, "", "", missing)
		if err != nil || n != 0 || len(cards) != 0 {
			t.Fatalf("filter used episode instead of first-season: %+v %d %v", cards, n, err)
		}
	}
	movie, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "94001", Title: "独立电影", Completed: true, EpisodeCount: 1, TotalEpisodes: 1, Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	movieFile := addFile(library, movie, 0, "movie")
	movieCards, _, err := svc.ListLibrarySeriesCards(ctx, library.ID, 1, 50, "", "hongguo:"+movie.SourceID, visibility)
	if err != nil || len(movieCards) != 1 || movieCards[0].Rep.SeriesID != "" || movieCards[0].Rep.ID != movieFile.ID {
		t.Fatalf("movie routing=%+v err=%v", movieCards, err)
	}
	page, n, err := svc.ListLibrarySeriesCards(ctx, library.ID, 2, 1, "", "", visibility)
	if err != nil || n != 2 || len(page) != 1 {
		t.Fatalf("pagination=%+v %d %v", page, n, err)
	}
	if err := repos.HongGuo.SaveAlbum(ctx, works[0].SourceID, hongguo.Album{}); err != nil {
		t.Fatal(err)
	}
	fallback, err := svc.GetMediaSeriesVisible(ctx, second.ID, visibility)
	if err != nil || fallback == nil || fallback.Title != works[1].Title || fallback.PosterURL != "" {
		t.Fatalf("earliest available season fallback=%+v err=%v", fallback, err)
	}
}
