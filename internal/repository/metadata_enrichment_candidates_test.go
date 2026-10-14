package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestDoubanEnrichmentCandidateJSONAndPagination(t *testing.T) {
	for _, kind := range []string{model.MetadataKindMovie, model.MetadataKindSeries} {
		t.Run(kind, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.TMDbSubject{}, &model.TMDbPersonProfile{}, &model.TMDbPayload{}, &model.TMDbCredit{}, &model.TMDbArtwork{}, &model.TMDbRefreshJob{}, &model.TMDbQueryCache{}, &model.MetadataIdentifier{}, &model.DoubanSubject{}, &model.ArtworkAsset{}); err != nil {
				t.Fatal(err)
			}
			ensureSourcePresentationTestSchema(t, db)
			cutoff := time.Now().UTC().Add(-24 * time.Hour)
			cases := []struct {
				payload   string
				degraded  bool
				recent    bool
				want      bool
				wrongKind bool
				ambiguous bool
			}{
				{payload: "", want: true},
				{payload: "null"},
				{payload: `123`},
				{payload: `["subject"]`},
				{payload: `{"intro":"text","pic":null}`},
				{payload: `{"intro":"text","cover_url":null}`},
				{payload: `{"intro":"text","pic":"x","subject":null}`, want: true},
				{payload: `{"intro":"text","pic":"x","data":null}`, want: true},
				{payload: `{"intro":" \t","pic":"x"}`},
				{payload: `{"intro":" ","pic":"x"}`},
				{payload: `{"intro":123,"pic":null}`},
				{payload: `{}`, degraded: true},
				{payload: `{}`, recent: true},
				{wrongKind: true},
				{ambiguous: true},
			}
			var want []string
			for i, tc := range cases {
				item := model.MetadataItem{PermanentBase: model.PermanentBase{ID: fmt.Sprintf("movie-%03d", i)}, Kind: kind, Title: "中文标题", Overview: "简介"}
				if err := db.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
				identifierKind := kind
				if tc.wrongKind {
					identifierKind = model.MetadataKindEpisode
				}
				if err := db.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: identifierKind, ExternalID: fmt.Sprint(i)}).Error; err != nil {
					t.Fatal(err)
				}
				if tc.ambiguous {
					if err := db.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: kind, ExternalID: "999"}).Error; err != nil {
						t.Fatal(err)
					}
				}
				if tc.payload != "" {
					fetched := cutoff.Add(-time.Second)
					if tc.recent {
						fetched = cutoff
					}
					if err := db.Create(&model.DoubanSubject{DoubanID: fmt.Sprint(i), EntityKind: kind, Payload: &tc.payload, Quality: model.DoubanPayloadQuality([]byte(tc.payload), tc.degraded), FetchedAt: &fetched}).Error; err != nil {
						t.Fatal(err)
					}
				}
				asset := model.ArtworkAsset{SHA256: fmt.Sprintf("sha-%d", i), StorageKey: fmt.Sprintf("test/%d", i), MimeType: "image/jpeg"}
				if err := db.Create(&asset).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&model.DoubanSubject{}).Where("douban_id=? AND entity_kind=?", fmt.Sprint(i), kind).Update("poster_asset_id", asset.ID).Error; err != nil {
					t.Fatal(err)
				}
				if !tc.recent && !tc.wrongKind && !tc.ambiguous {
					want = append(want, item.ID)
				}
			}
			repo := New(db).Metadata
			var got []string
			for after := ""; ; {
				page, err := repo.ListDoubanMovieEnrichmentAfter(t.Context(), after, cutoff, 2)
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range page {
					got = append(got, row.MetadataID)
					after = row.MetadataID
				}
				if len(page) < 2 {
					break
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("candidates = %v, want %v", got, want)
			}
			// 同一豆瓣身份只能保存一份共享资料。
			if err := db.Create(&model.DoubanSubject{DoubanID: "1", EntityKind: kind}).Error; err == nil {
				t.Fatal("duplicate provider snapshot accepted")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := repo.ListDoubanMovieEnrichmentAfter(ctx, "", cutoff, 2); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled query = %v", err)
			}
		})
	}
}
