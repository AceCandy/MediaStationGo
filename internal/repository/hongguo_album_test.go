package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoOfficialAlbumsAndBackfill(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.HongGuoModels()...); err != nil {
		t.Fatal(err)
	}
	r, ctx := New(db).HongGuo, t.Context()
	var works []model.HongGuoWork
	for i := 0; i < 103; i++ {
		works = append(works, model.HongGuoWork{SourceID: fmt.Sprintf("900000000000000%04d", i), Kind: "series", Title: "同名剧", Tags: "[]", RefreshedAt: time.Now()})
	}
	if err = db.Create(&works).Error; err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now()
	first, err := r.PendingAlbums(ctx, "", cutoff)
	if err != nil || len(first) != 100 {
		t.Fatalf("first=%d err=%v", len(first), err)
	}
	last, err := r.PendingAlbums(ctx, first[99].SourceID, cutoff)
	if err != nil || len(last) != 3 {
		t.Fatalf("last=%d err=%v", len(last), err)
	}
	for i, season := range []int{2, 1} {
		if err = r.SaveAlbum(ctx, works[i].SourceID, hongguo.Album{ID: "9000000000000000099", Season: season}); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.SaveAlbum(ctx, works[2].SourceID, hongguo.Album{ID: "9000000000000000098", Season: 1}); err != nil {
		t.Fatal(err)
	}
	if err = r.SaveAlbum(ctx, works[3].SourceID, hongguo.Album{}); err != nil {
		t.Fatal(err)
	}
	if err = r.RetryAlbum(ctx, works[0].SourceID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	group, err := r.Group(ctx, "9000000000000000099")
	if err != nil || len(group.Members) != 2 || group.Members[0].SourceID != works[1].SourceID {
		t.Fatalf("group=%+v err=%v", group, err)
	}
	rows, err := r.PendingAlbums(ctx, "", time.Now())
	if err != nil || len(rows) != 99 {
		t.Fatalf("remaining=%d err=%v", len(rows), err)
	}
	// 网页详情更新与补充结果隔离，包括成功无关系的检查点。
	if _, err = r.SaveDetail(ctx, hongguo.Work{SourceID: works[0].SourceID, Title: "更新后的标题", Snapshot: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	w, err := r.FindBySourceID(ctx, works[0].SourceID)
	if err != nil || w.ID != works[0].ID || w.RelatedAlbumID != "9000000000000000099" || w.SeasonIndex != 2 || w.AlbumCheckedAt == nil {
		t.Fatalf("lost relation: %+v %v", w, err)
	}
	if err = r.SaveAlbum(ctx, works[0].SourceID, hongguo.Album{ID: "bad", Season: 1}); err == nil {
		t.Fatal("invalid relation accepted")
	}
}
