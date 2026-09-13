package repository

import (
	"context"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoBindingGroupingAndStableProgress(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := New(db)
	library := model.Library{Name: "红果", Path: "/test/hongguo", Type: model.LibraryTypeHongGuo}
	if err := db.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	input := hongguo.Work{SourceID: "900000000000000001", Title: "待完结剧", EpisodeCount: 1, TotalEpisodes: 1, Snapshot: []byte(`{}`)}
	m := model.Media{LibraryID: library.ID, Path: "/test/hongguo/a.strm", CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: input.SourceID, SeasonNum: 1, EpisodeNum: 1}
	if err := r.Media.Upsert(ctx, &m); err != nil {
		t.Fatal(err)
	}
	if m.ScrapeStatus != "source_pending" {
		t.Fatalf("status=%s", m.ScrapeStatus)
	}
	pending, pendingTotal, err := r.HongGuo.PendingMedia(ctx, 1)
	if err != nil || pendingTotal != 1 || len(pending) != 1 || pending[0].SourceID != input.SourceID {
		t.Fatalf("pending diagnostics: %+v %d %v", pending, pendingTotal, err)
	}
	if _, err := r.HongGuo.SaveDetail(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := r.HongGuo.RebindWork(ctx, input.SourceID); err != nil {
		t.Fatal(err)
	}
	if _, total, err := r.HongGuo.PendingMedia(ctx, 1); err != nil || total != 0 {
		t.Fatalf("bound file retained in pending diagnostics: %d %v", total, err)
	}
	view, err := r.MediaView.FindByID(ctx, m.ID)
	if err != nil || view == nil {
		t.Fatalf("view=%v err=%v", view, err)
	}
	if view.MetadataID != "" || view.MetadataKind != "episode" {
		t.Fatalf("wrong source view: %+v", view)
	}
	itemID := view.CatalogItemID
	if err := r.Setting.Set(ctx, "hongguo.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	rescan := m
	rescan.EpisodeNum = 999
	if err := r.Media.Upsert(ctx, &rescan); err == nil {
		t.Fatal("disabled source changed a binding")
	}
	unchanged, err := r.MediaView.FindByID(ctx, m.ID)
	if err != nil || unchanged == nil || unchanged.CatalogItemID != itemID || unchanged.EpisodeNum != 1 {
		t.Fatalf("disabled scan damaged existing media: %+v %v", unchanged, err)
	}
	if err := r.Setting.Set(ctx, "hongguo.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := r.HongGuo.RecordProgress(ctx, "user-a", "session-a", *view, 30000, 120000, false); err != nil {
		t.Fatal(err)
	}
	if err := r.HongGuo.RecordProgress(ctx, "user-a", "session-a", *view, 40000, 120000, false); err != nil {
		t.Fatal(err)
	}
	cards, total, err := r.HongGuo.UserCards(ctx, "user-a", "history", 1, 50, MediaQueryFilter{})
	if err != nil || total != 1 || len(cards) != 1 || cards[0].PositionMs != 40000 {
		t.Fatalf("history cards: %v %d %v", cards, total, err)
	}
	cards, total, err = r.HongGuo.UserCards(ctx, "user-a", "history", 1, 50, MediaQueryFilter{HiddenLibraryIDs: []string{library.ID}})
	if err != nil || total != 0 || len(cards) != 0 {
		t.Fatal("hidden history leaked")
	}
	if err := r.HongGuo.SetFavorite(ctx, "user-a", input.SourceID, true); err != nil {
		t.Fatal(err)
	}
	cards, total, err = r.HongGuo.UserCards(ctx, "user-a", "favourites", 1, 50, MediaQueryFilter{})
	if err != nil || total != 1 || len(cards) != 1 {
		t.Fatalf("favorite cards: %v %d %v", cards, total, err)
	}
	group, err := r.HongGuo.SaveGroup(ctx, "", "合并剧", []HongGuoGroupInput{{SourceID: input.SourceID, SeasonNumber: 3}})
	if err != nil {
		t.Fatal(err)
	}
	view, err = r.MediaView.FindByID(ctx, m.ID)
	if err != nil || view.SeasonNum != 3 || view.Media.SeasonNum != 1 || view.CatalogItemID != itemID {
		t.Fatalf("group changed source identity: %+v %v", view, err)
	}
	state, err := r.HongGuo.UserState(ctx, "user-a", input.SourceID, 1)
	if err != nil || state.PositionMs != 40000 {
		t.Fatalf("lost progress: %+v %v", state, err)
	}
	other, err := r.HongGuo.UserState(ctx, "user-b", input.SourceID, 1)
	if err != nil || other.PositionMs != 0 {
		t.Fatal("user state leaked")
	}
	var events int64
	if err := db.Model(&model.HongGuoPlaybackEvent{}).Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	if err := r.HongGuo.DeleteGroup(ctx, group.ID); err != nil {
		t.Fatal(err)
	}
	input.Completed = true
	if _, err := r.HongGuo.SaveDetail(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := r.HongGuo.RebindWork(ctx, input.SourceID); err != nil {
		t.Fatal(err)
	}
	view, err = r.MediaView.FindByID(ctx, m.ID)
	if err != nil || view.MetadataKind != "movie" {
		t.Fatalf("movie transition: %+v %v", view, err)
	}
	if err := r.HongGuo.RecordProgress(ctx, "user-a", "session-a", *view, 50000, 120000, false); err != nil {
		t.Fatal(err)
	}
	state, err = r.HongGuo.UserState(ctx, "user-a", input.SourceID, 1)
	if err != nil || state.PositionMs != 50000 {
		t.Fatal("movie transition lost progress")
	}
	// 重扫无效坐标必须解除旧绑定，不能继续呈现成已匹配电影。
	m.EpisodeNum = 2
	if err := r.Media.Upsert(ctx, &m); err != nil {
		t.Fatal(err)
	}
	var bindings int64
	if err := db.Model(&model.HongGuoMediaBinding{}).Where("media_id = ?", m.ID).Count(&bindings).Error; err != nil || bindings != 0 {
		t.Fatalf("stale binding=%d err=%v", bindings, err)
	}
	if m.ScrapeStatus != "source_pending" {
		t.Fatal("invalid coordinate matched")
	}
	legacy := model.Media{LibraryID: library.ID, Path: m.Path}
	if err := r.Media.Upsert(ctx, &legacy); err == nil {
		t.Fatal("legacy rescan accepted source file")
	}
	rows, err := r.MediaView.FindByIDs(ctx, []string{m.ID}, MediaQueryFilter{HiddenLibraryIDs: []string{library.ID}})
	if err != nil || len(rows) != 0 {
		t.Fatal("hidden media exposed")
	}
}
