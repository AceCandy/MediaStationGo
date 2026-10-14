package database

import (
	"errors"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestMetadataIndexTracksCommittedSourceChanges(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	item := model.MetadataItem{Kind: "movie"}
	subject := model.TMDbSubject{Kind: "movie", TMDbID: 901, Title: "最初", Fields: "{}"}
	for _, row := range []any{&item, &subject} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	binding := model.MetadataIdentifier{MetadataID: item.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "901"}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	var observed model.MetadataIndexJob
	if err := db.First(&observed, "metadata_id=?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&subject).Update("title", "回滚").Error; err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var current model.MetadataIndexJob
	if err := db.First(&current, "metadata_id=?", item.ID).Error; err != nil || current.Revision != observed.Revision {
		t.Fatalf("rollback leaked revision: %+v %v", current, err)
	}
	if err := db.Model(&subject).Update("title", "已提交").Error; err != nil {
		t.Fatal(err)
	}
	result := db.Where("metadata_id=? AND revision=?", item.ID, observed.Revision).Delete(&model.MetadataIndexJob{})
	if result.Error != nil || result.RowsAffected != 0 {
		t.Fatalf("stale completion consumed new work: %d %v", result.RowsAffected, result.Error)
	}
	if err := db.First(&current, "metadata_id=?", item.ID).Error; err != nil || current.Revision <= observed.Revision {
		t.Fatalf("source did not enqueue: %+v %v", current, err)
	}
	revision := current.Revision
	if err := db.Delete(&subject).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&current, "metadata_id=?", item.ID).Error; err != nil || current.Revision <= revision {
		t.Fatalf("source deletion did not enqueue: %+v %v", current, err)
	}
}

func TestMetadataIndexTracksSourcePeopleAndTranslations(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	item := model.MetadataItem{Kind: "movie"}
	subject := model.TMDbSubject{Kind: "movie", TMDbID: 903}
	person := model.Person{Name: "Old", OriginalName: "Old", Source: "tmdb"}
	for _, row := range []any{&item, &subject, &person} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	profile := model.TMDbPersonProfile{TMDbID: 904, PersonID: person.ID, Name: "Old", OriginalName: "Old"}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	credit := model.TMDbCredit{SubjectID: subject.ID, PersonProfileID: profile.ID, Origin: "subject", Category: "cast", CreditKey: "cast", Character: "Hero"}
	binding := model.MetadataIdentifier{MetadataID: item.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "903"}
	for _, row := range []any{&binding, &credit} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	revision := int64(0)
	check := func() {
		t.Helper()
		var job model.MetadataIndexJob
		if err := db.First(&job, "metadata_id=?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		if job.Revision <= revision {
			t.Fatalf("source change not enqueued: %d <= %d", job.Revision, revision)
		}
		revision = job.Revision
	}
	check()
	if err := db.Model(&profile).Update("name", "New").Error; err != nil {
		t.Fatal(err)
	}
	check()
	for _, cache := range []model.TranslationCache{
		{Kind: "person_name", ContextKey: person.ID, SourceText: "Old", TargetLanguage: "zh-CN", PromptVersion: "test", TranslatedText: "演员"},
		{Kind: "role", ContextKey: subject.ID, SourceText: "Hero", TargetLanguage: "zh-CN", PromptVersion: "test", TranslatedText: "英雄"},
	} {
		if err := db.Create(&cache).Error; err != nil {
			t.Fatal(err)
		}
		check()
		if err := db.Delete(&cache).Error; err != nil {
			t.Fatal(err)
		}
		check()
	}
}
