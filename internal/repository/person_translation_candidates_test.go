package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestPendingPeopleTranslationsFiltersBeforeLimit(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Person{}, &model.TranslationCache{}); err != nil {
		t.Fatal(err)
	}
	people := make([]model.Person, 1002)
	for i := range people {
		people[i] = model.Person{Base: model.Base{ID: fmt.Sprintf("%08d", i)}, Name: "中文", OriginalName: "中文"}
	}
	// 等于上下边界的汉字跳过，范围之外和空白原文仍按原有规则处理。
	names := []string{"Negative", "Positive", "OldVersion", "OtherLanguage", "OtherContext", "OtherSource", "DeletedCache", "\u4e00", "\u9fff", "\u4dff", "\ua000", " ", ""}
	for i, name := range names {
		people = append(people, model.Person{Base: model.Base{ID: fmt.Sprintf("z%08d", i)}, Name: name, OriginalName: name})
	}
	if err := db.CreateInBatches(&people, 100).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		person := people[1002+i]
		cache := model.TranslationCache{Kind: "person_name", ContextKey: person.ID, SourceText: person.OriginalName, TargetLanguage: "zh-CN", PromptVersion: "v1"}
		switch i {
		case 1:
			cache.TranslatedText = "有效译文"
		case 2:
			cache.PromptVersion = "v0"
		case 3:
			cache.TargetLanguage = "en"
		case 4:
			cache.ContextKey = "other"
		case 5:
			cache.SourceText = "other"
		}
		if err := db.Create(&cache).Error; err != nil {
			t.Fatal(err)
		}
		if i == 6 {
			if err := db.Delete(&cache).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	repo := &PersonRepository{db: db}
	rows, err := repo.ListPendingPeopleTranslations(t.Context(), "zh-CN", "v1", 2)
	if err != nil || len(rows) != 2 || rows[0].OriginalName != "Positive" || rows[1].OriginalName != "OldVersion" {
		t.Fatalf("limited candidates = %+v, err = %v", rows, err)
	}
	rows, err = repo.ListPendingPeopleTranslations(t.Context(), "zh-CN", "v1", 100)
	if err != nil || len(rows) != 9 {
		t.Fatalf("all candidates = %+v, err = %v", rows, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repo.ListPendingPeopleTranslations(ctx, "zh-CN", "v1", 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query = %v", err)
	}
}
