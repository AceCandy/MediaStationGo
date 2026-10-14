package repository

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestDoubanSharedSubjectWriteAndAssociation(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	ensureSourcePresentationTestSchema(t, db)
	repos := New(db)
	item := model.MetadataItem{Kind: "movie", Title: "本地片名", Rating: 6}
	if err := repos.Metadata.Create(t.Context(), &item, []model.MetadataIdentifier{{Provider: "douban", EntityKind: "movie", DoubanEntityKind: "series", ExternalID: "100"}}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	raw := []byte(`{"rating":{"value":8.2},"title":"远端剧集"}`)
	if err := repos.Douban.Save(t.Context(), "100", "series", raw, model.DoubanQualityComplete, at); err != nil {
		t.Fatal(err)
	}
	// JSONB 的格式变化不应把本地补齐当作一次新的资料获取。
	if err := repos.Metadata.SaveProviderData(t.Context(), item.ID, "douban", []byte(`{ "title": "远端剧集", "rating": {"value": 8.2} }`), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, quality := range []string{model.DoubanQualityComplete, model.DoubanQualityDegraded, model.DoubanQualityPartial} {
		incomingAt := at.Add(-time.Hour)
		if quality != model.DoubanQualityComplete {
			incomingAt = at.Add(time.Hour)
		}
		if err := repos.Douban.Save(t.Context(), "100", "series", []byte(`{"title":"旧或降级资料","rating":7}`), quality, incomingAt); err != nil {
			t.Fatal(err)
		}
	}
	subject, err := repos.Douban.FindForMetadata(t.Context(), item.ID)
	if err != nil || subject == nil || subject.Rating != 8.2 || subject.EntityKind != "series" || !subject.FetchedAt.Equal(at) {
		t.Fatalf("subject %#v %v", subject, err)
	}
	ratings, err := repos.Metadata.DoubanRatingsByMetadataIDs(t.Context(), []string{item.ID})
	if err != nil || ratings[item.ID] != 8.2 {
		t.Fatalf("ratings %v %v", ratings, err)
	}
	saved, err := repos.Metadata.FindByID(t.Context(), item.ID)
	if err != nil || saved.Rating != 0 || saved.Title != "远端剧集" {
		t.Fatalf("source presentation mixed old values: %#v %v", saved, err)
	}
	retryAt := at.Add(2 * time.Hour)
	if err := repos.Douban.RecordFailure(t.Context(), "100", "series", at.Add(time.Hour), retryAt); err != nil {
		t.Fatal(err)
	}
	if err := repos.Douban.RecordFailure(t.Context(), "100", "series", at.Add(time.Minute), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	subject, err = repos.Douban.Find(t.Context(), "100", "series")
	if err != nil || subject.Rating != 8.2 || !subject.NextRetryAt.Equal(retryAt) {
		t.Fatalf("late failure %#v %v", subject, err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: "movie", ExternalID: "101"}).Error; err != nil {
		t.Fatal(err)
	}
	subject, err = repos.Douban.FindForMetadata(t.Context(), item.ID)
	if err != nil || subject != nil {
		t.Fatalf("ambiguous subject %#v %v", subject, err)
	}
	ratings, err = repos.Metadata.DoubanRatingsByMetadataIDs(t.Context(), []string{item.ID})
	if err != nil || len(ratings) != 0 {
		t.Fatalf("ambiguous ratings %v %v", ratings, err)
	}
	if err := repos.Metadata.ReplaceIdentifier(t.Context(), item.ID, "douban", "movie", "200"); err != nil {
		t.Fatal(err)
	}
	subject, err = repos.Douban.FindForMetadata(t.Context(), item.ID)
	if err != nil || subject != nil {
		t.Fatalf("old subject followed new binding: %#v %v", subject, err)
	}
}

func TestDoubanSubjectSuccessPreservesNewerFailure(t *testing.T) {
	for _, withPayload := range []bool{false, true} {
		name := "failure_placeholder"
		if withPayload {
			name = "existing_details"
		}
		t.Run(name, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.DoubanSubject{}); err != nil {
				t.Fatal(err)
			}
			ensureSourcePresentationTestSchema(t, db)
			repo := New(db).Douban
			at := time.Now().UTC().Truncate(time.Microsecond)
			if withPayload {
				if err := repo.Save(t.Context(), "100", "series", []byte(`{"title":"旧详情","rating":7}`), model.DoubanQualityComplete, at.Add(-time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			failedAt, retryAt := at.Add(time.Minute), at.Add(6*time.Minute)
			if err := repo.RecordFailure(t.Context(), "100", "series", failedAt, retryAt); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(t.Context(), "100", "series", []byte(`{"title":"有效详情","rating":8.1}`), model.DoubanQualityComplete, at); err != nil {
				t.Fatal(err)
			}
			subject, err := repo.Find(t.Context(), "100", "series")
			if err != nil || subject == nil || subject.Payload == nil || subject.Rating != 8.1 || subject.Quality != model.DoubanQualityComplete || subject.FetchedAt == nil || !subject.FetchedAt.Equal(at) {
				t.Fatalf("late success was not saved: %#v %v", subject, err)
			}
			if subject.LastAttemptAt == nil || !subject.LastAttemptAt.Equal(failedAt) || subject.NextRetryAt == nil || !subject.NextRetryAt.Equal(retryAt) {
				t.Fatalf("late success cleared newer failure: %#v", subject)
			}
			freshAt := failedAt.Add(time.Minute)
			if err := repo.Save(t.Context(), "100", "series", []byte(`{"title":"最新详情","rating":9}`), model.DoubanQualityComplete, freshAt); err != nil {
				t.Fatal(err)
			}
			subject, err = repo.Find(t.Context(), "100", "series")
			if err != nil || subject == nil || subject.Rating != 9 || subject.FetchedAt == nil || !subject.FetchedAt.Equal(freshAt) || subject.LastAttemptAt == nil || !subject.LastAttemptAt.Equal(freshAt) || subject.NextRetryAt != nil {
				t.Fatalf("new success did not clear old failure: %#v %v", subject, err)
			}
		})
	}
}

func TestDoubanWritesRejectInvalidIdentity(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	ensureSourcePresentationTestSchema(t, db)
	repo := New(db).Douban
	at := time.Now()
	for _, identity := range []struct{ id, kind string }{{"", "movie"}, {" ", "series"}, {"100", "season"}, {"100", "episode"}, {"100", "invalid"}} {
		if err := repo.Save(t.Context(), identity.id, identity.kind, []byte(`{"title":"Test"}`), model.DoubanQualityComplete, at); err == nil {
			t.Fatalf("invalid subject saved: %#v", identity)
		}
		if err := repo.RecordFailure(t.Context(), identity.id, identity.kind, at, at.Add(time.Hour)); err == nil {
			t.Fatalf("invalid placeholder saved: %#v", identity)
		}
	}
	var count int64
	if err := db.Model(&model.DoubanSubject{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid writes created %d rows", count)
	}
}
