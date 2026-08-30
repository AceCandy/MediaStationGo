package repository

import (
	"fmt"
	"strings"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestReplaceCreditsIsIdempotentAndReplacesLoadedType(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{}, &model.TranslationCache{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Film", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	repo := &PersonRepository{db: db}
	credits := []CreditInput{{Provider: "tmdb", ExternalID: "1", Name: "Actor One", Type: model.CreditTypeActor, OriginalRole: "Hero", SortOrder: 0}}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, credits); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MetadataCredit{}).Where("metadata_id = ?", metadata.ID).Update("role", "英雄").Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, credits); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.MetadataCredit{}).Where("metadata_id = ?", metadata.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("credit count = %d, err=%v", count, err)
	}
	var preserved model.MetadataCredit
	if err := db.First(&preserved, "metadata_id = ?", metadata.ID).Error; err != nil || preserved.Role != "英雄" {
		t.Fatalf("preserved credit = %#v, err=%v", preserved, err)
	}
	credits[0].OriginalRole = "Villain"
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, credits); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MetadataCredit{}).Where("metadata_id = ?", metadata.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("credit count after replacement = %d, err=%v", count, err)
	}
	if err := db.Model(&model.MetadataCredit{}).Where("id = ?", preserved.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("replaced credit count = %d, err=%v", count, err)
	}
	var personCount int64
	if err := db.Model(&model.PersonIdentifier{}).Where("provider = ? AND external_id = ?", "tmdb", "1").Count(&personCount).Error; err != nil || personCount != 1 {
		t.Fatalf("identifier count = %d, err=%v", personCount, err)
	}
}

func TestReplaceCreditsPreservesLongRole(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Film", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	longRole := strings.Repeat("Character / ", 30) + "Character"
	repo := &PersonRepository{db: db}
	credits := []CreditInput{{Provider: "tmdb", ExternalID: "1", Name: "Actor One", Type: model.CreditTypeActor, OriginalRole: longRole}}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, credits); err != nil {
		t.Fatal(err)
	}
	var credit model.MetadataCredit
	if err := db.Where("metadata_id = ?", metadata.ID).First(&credit).Error; err != nil {
		t.Fatal(err)
	}
	if credit.OriginalRole != longRole || credit.Role != longRole {
		t.Fatalf("long role was not preserved: original=%d role=%d want=%d", len(credit.OriginalRole), len(credit.Role), len(longRole))
	}
}

func TestPeopleTranslationPostgresTextColumns(t *testing.T) {
	db, err := gorm.Open(postgres.Open(""), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	for schemaModel, fieldNames := range map[any][]string{
		&model.MetadataCredit{}:   {"OriginalRole", "Role"},
		&model.TranslationCache{}: {"SourceText", "TranslatedText"},
	} {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(schemaModel); err != nil {
			t.Fatal(err)
		}
		for _, fieldName := range fieldNames {
			dataType := db.Migrator().FullDataTypeOf(stmt.Schema.LookUpField(fieldName)).SQL
			if !strings.Contains(strings.ToLower(dataType), "text") {
				t.Fatalf("%s.%s postgres type = %q, want text", stmt.Schema.Table, fieldName, dataType)
			}
		}
	}
}

func TestPostgresArrayParameterIsNotExpanded(t *testing.T) {
	db, err := gorm.Open(postgres.Open(""), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"one", "two"}
	stmt := db.Session(&gorm.Session{DryRun: true}).Where("id = ANY(?)", &ids).Find(&model.Person{}).Statement
	if len(stmt.Vars) != 1 {
		t.Fatalf("bind variables = %d, want 1", len(stmt.Vars))
	}
}

func TestTranslationCacheIsContextScopedAndRejectsStaleTargets(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Person{}, &model.MetadataCredit{}, &model.TranslationCache{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "metadata-1"}, Kind: model.MetadataKindMovie, Title: "Film", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	person := model.Person{Name: "Old Name", OriginalName: "Old Name", NormalizedName: "old name", Source: "tmdb"}
	credit := model.MetadataCredit{MetadataID: "metadata-1", PersonID: "person-1", Type: model.CreditTypeActor, Role: "Old Role", OriginalRole: "Old Role"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	credit.PersonID = person.ID
	if err := db.Create(&credit).Error; err != nil {
		t.Fatal(err)
	}
	repo := &PersonRepository{db: db}
	base := model.TranslationCache{Kind: "role", SourceText: "Old Role", TargetLanguage: "zh-CN", PromptVersion: "v1", TranslatedText: "旧角色"}
	first, second := base, base
	first.ContextKey = "metadata-1"
	second.ContextKey = "metadata-2"
	if err := repo.SaveAndApplyTranslation(t.Context(), first, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAndApplyTranslation(t.Context(), second, nil); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.TranslationCache{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("context-scoped cache count = %d, err=%v", count, err)
	}
	if err := db.Model(&person).Updates(map[string]any{"name": "New Name", "original_name": "New Name"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&credit).Updates(map[string]any{"role": "New Role", "original_role": "New Role"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyCachedTranslation(t.Context(), []TranslationTarget{
		{Kind: "person_name", ID: person.ID, OriginalText: "Old Name"},
		{Kind: "role", ID: credit.ID, OriginalText: "Old Role"},
	}, "陈旧译文"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&person, "id = ?", person.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&credit, "id = ?", credit.ID).Error; err != nil {
		t.Fatal(err)
	}
	if person.Name != "New Name" || credit.Role != "New Role" {
		t.Fatalf("stale translation overwrote current values: person=%q role=%q", person.Name, credit.Role)
	}
}

func TestListPersonWorkContextsAcceptsMoreThanPostgresParameterLimit(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Person{}, &model.MetadataCredit{}); err != nil {
		t.Fatal(err)
	}
	personIDs := make([]string, 65536)
	for i := range personIDs {
		personIDs[i] = fmt.Sprintf("person-%05d", i)
	}
	rows, err := (&PersonRepository{db: db}).ListPersonWorkContexts(t.Context(), personIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("work contexts = %d, want 0", len(rows))
	}
}

func TestListTranslationCachesAcceptsMoreThanPostgresParameterLimit(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TranslationCache{}); err != nil {
		t.Fatal(err)
	}
	cache := model.TranslationCache{
		Kind: "role", ContextKey: "context-00000", SourceText: "source-00000",
		TargetLanguage: "zh-CN", PromptVersion: "v1", TranslatedText: "译文",
	}
	if err := db.Create(&cache).Error; err != nil {
		t.Fatal(err)
	}
	lookups := make([]TranslationCacheLookup, 33000)
	for i := range lookups {
		lookups[i] = TranslationCacheLookup{
			Kind: "role", ContextKey: fmt.Sprintf("context-%05d", i), SourceText: fmt.Sprintf("source-%05d", i),
			TargetLanguage: "zh-CN", PromptVersion: "v1",
		}
	}
	rows, err := (&PersonRepository{db: db}).ListTranslationCaches(t.Context(), lookups)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != cache.ID {
		t.Fatalf("translation caches = %#v, want cache %q", rows, cache.ID)
	}
}
