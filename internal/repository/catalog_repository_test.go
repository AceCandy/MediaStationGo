package repository

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)





func TestListDoubanMovieEnrichmentAfterRefreshesOnlyStaleIncompleteWorks(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.TMDbSubject{}, &model.TMDbPersonProfile{}, &model.TMDbPayload{}, &model.TMDbCredit{}, &model.TMDbArtwork{}, &model.TMDbRefreshJob{}, &model.TMDbQueryCache{}, &model.MetadataIdentifier{}, &model.DoubanSubject{}, &model.ArtworkAsset{}); err != nil {
		t.Fatal(err)
	}
	ensureSourcePresentationTestSchema(t, db)
	now := time.Now().UTC()
	refreshBefore := now.Add(-24 * time.Hour)
	items := []model.MetadataItem{
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000100"}, Kind: model.MetadataKindMovie, Title: "Pending"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000200"}, Kind: model.MetadataKindMovie, Title: "Recent"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000300"}, Kind: model.MetadataKindMovie, Title: "完整标题", Overview: "完整简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000325"}, Kind: model.MetadataKindMovie, Title: "快照缺字段", Overview: "已有简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000350"}, Kind: model.MetadataKindMovie, Title: "旧快照完整标题", Overview: "完整简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000400"}, Kind: model.MetadataKindMovie, Title: "缺海报", Overview: "已有简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000500"}, Kind: model.MetadataKindMovie, Title: "缺简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000600"}, Kind: model.MetadataKindMovie, Title: "English", Overview: "已有简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000700"}, Kind: model.MetadataKindSeries, Title: "Series"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000800"}, Kind: model.MetadataKindMovie, Title: "Ambiguous"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := make([]model.MetadataIdentifier, 0, len(items)+1)
	for i := range items {
		identifiers = append(identifiers, model.MetadataIdentifier{
			MetadataID: items[i].ID, Provider: "douban", EntityKind: items[i].Kind, ExternalID: fmt.Sprintf("%d00", i+1),
		})
	}
	identifiers = append(identifiers, model.MetadataIdentifier{MetadataID: items[9].ID, Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "801"})
	if err := db.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	snapshots := make([]model.DoubanSubject, 0, len(items)-1)
	for i := 1; i < len(items)-1; i++ {
		fetchedAt := refreshBefore.Add(-time.Hour)
		raw, err := json.Marshal(map[string]string{"title": items[i].Title, "intro": items[i].Overview, "cover_url": "https://image.test/poster.jpg"})
		if err != nil {
			t.Fatal(err)
		}
		payload := string(raw)
		if i == 1 {
			fetchedAt = now
		}
		if i == 1 || i == 4 {
			payload = `{"subject":{}}`
		}
		if i == 3 {
			payload = `{"title":"移动端缺失详情"}`
		}
		snapshots = append(snapshots, model.DoubanSubject{DoubanID: fmt.Sprintf("%d00", i+1), EntityKind: items[i].Kind, Payload: &payload, Quality: model.DoubanPayloadQuality([]byte(payload), false), FetchedAt: &fetchedAt})
	}
	if err := db.Create(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{2, 3, 4, 6, 7} {
		asset := model.ArtworkAsset{PermanentBase: model.PermanentBase{ID: fmt.Sprintf("asset-%d", index)}, SHA256: fmt.Sprintf("sha-%d", index), StorageKey: fmt.Sprintf("test/%d.jpg", index), MimeType: "image/jpeg"}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.DoubanSubject{}).Where("douban_id=? AND entity_kind=?", fmt.Sprintf("%d00", index+1), items[index].Kind).Update("poster_asset_id", asset.ID).Error; err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := New(db).Metadata.ListDoubanMovieEnrichmentAfter(t.Context(), "", refreshBefore, 20)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{items[0].ID, items[3].ID, items[4].ID, items[5].ID, items[6].ID, items[7].ID, items[8].ID}
	if len(candidates) != len(wantIDs) {
		t.Fatalf("douban enrichment candidates = %#v", candidates)
	}
	for i := range wantIDs {
		if candidates[i].MetadataID != wantIDs[i] {
			t.Fatalf("douban enrichment candidates = %#v", candidates)
		}
	}
}
