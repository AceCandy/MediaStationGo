package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/logger"
)

type hongGuoBatchQueries struct {
	logger.Interface
	works, episodes, settings int
}

func (l *hongGuoBatchQueries) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	if strings.Contains(sql, `FROM "hongguo_works"`) {
		l.works++
	}
	if strings.Contains(sql, `FROM "hongguo_episodes"`) {
		l.episodes++
	}
	if strings.Contains(sql, `FROM "settings"`) {
		l.settings++
	}
}

func TestHongGuoScanBatchLookupAndRollback(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	for _, row := range []any{
		&model.Library{Base: model.Base{ID: "library"}, Name: "batch", Path: "/batch", Type: model.LibraryTypeHongGuo},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "12345678901", Kind: "series", Title: "Target"},
		&model.HongGuoEpisode{WorkID: "work", Number: 1},
	} {
		if err := repos.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	file := func(i int) *model.Media {
		return &model.Media{LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "12345678901", Path: fmt.Sprintf("/batch/%d.mkv", i), SeasonNum: 1, EpisodeNum: 1}
	}
	counter := &hongGuoBatchQueries{Interface: logger.Default}
	repos.DB.Logger = counter
	files := []*model.Media{file(1), file(2), file(3)}
	files[2].EpisodeNum = 99
	if _, err := repos.Media.UpsertHongGuoScanBatch(ctx, files); err != nil {
		t.Fatal(err)
	}
	if counter.works != 1 || counter.episodes != 1 || counter.settings != 1 {
		t.Fatalf("lookup reads=%+v", counter)
	}
	if files[0].ScrapeStatus != "matched" || files[1].ScrapeStatus != "matched" || files[2].ScrapeStatus != "source_pending" {
		t.Fatal("batch binding semantics changed")
	}
	// 第二行失败时第一行及其生成的 ID 都回滚，搜索后端不能看到未提交结果。
	backend := &hongGuoSearchTestBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	if _, err := repos.HongGuo.BackfillSearchIndex(ctx, 10, 0); err != nil {
		t.Fatal(err)
	}
	before := len(backend.upserts)
	bad := []*model.Media{file(4), file(5)}
	bad[1].MetadataID = "forbidden"
	if _, err := repos.Media.UpsertHongGuoScanBatch(ctx, bad); err == nil {
		t.Fatal("invalid metadata was accepted")
	}
	if bad[0].ID != "" || bad[0].ScrapeStatus != "" || len(backend.upserts) != before {
		t.Fatal("rollback leaked generated identity or search publication")
	}
	var count int64
	if err := repos.DB.Model(&model.Media{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	if err := repos.Media.Upsert(ctx, bad[0]); err != nil {
		t.Fatalf("individual retry: %v", err)
	}
	if err := repos.Setting.Set(ctx, "hongguo.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Media.UpsertHongGuoScanBatch(ctx, []*model.Media{file(6)}); err == nil {
		t.Fatal("disabled source accepted batch")
	}
}
