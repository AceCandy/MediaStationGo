package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestReplaceCreditsConcurrentPersonCreation(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{}); err != nil {
		t.Fatal(err)
	}
	works := []model.MetadataItem{{Kind: model.MetadataKindSeries, Title: "First", Source: "tmdb"}, {Kind: model.MetadataKindSeries, Title: "Second", Source: "tmdb"}}
	if err := db.Create(&works).Error; err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ready, release := make(chan struct{}, 2), make(chan struct{})
	results := make(chan error, 2)
	var verify *gorm.DB
	for i := range works {
		conn, err := pool.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
			t.Fatal(err)
		}
		worker, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		verify = worker
		// 两个事务都确认标识不存在后才允许创建，稳定覆盖首次写入竞争。
		if err := worker.Callback().Query().After("gorm:query").Register("test:person-create-race", func(tx *gorm.DB) {
			if tx.Statement.Table == "person_identifiers" && errors.Is(tx.Error, gorm.ErrRecordNotFound) {
				ready <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		go func() {
			results <- (&PersonRepository{db: worker}).ReplaceCredits(ctx, works[i].ID, []string{model.CreditTypeActor}, []CreditInput{{Provider: "tmdb", ExternalID: "1924538", Name: "Shared Actor", Type: model.CreditTypeActor, OriginalRole: works[i].Title}})
		}()
	}
	for range works {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("concurrent lookups did not reach the barrier")
		}
	}
	close(release)
	for range works {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var peopleCount, identifierCount int64
	if err := verify.Model(&model.Person{}).Unscoped().Count(&peopleCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := verify.Model(&model.PersonIdentifier{}).Unscoped().Count(&identifierCount).Error; err != nil {
		t.Fatal(err)
	}
	var credits []model.MetadataCredit
	if err := verify.Order("metadata_id").Find(&credits).Error; err != nil {
		t.Fatal(err)
	}
	if peopleCount != 1 || identifierCount != 1 || len(credits) != 2 || credits[0].PersonID != credits[1].PersonID || credits[0].OriginalRole == credits[1].OriginalRole {
		t.Fatalf("want one shared person and separate credits: people=%d identifiers=%d credits=%+v", peopleCount, identifierCount, credits)
	}
}

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
	before, err := repo.ListCreditsWithPeople(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	identifiers, err := repo.ListIdentifiers(t.Context(), before[0].PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, credits); err != nil {
		t.Fatal(err)
	}
	after, err := repo.ListCreditsWithPeople(t.Context(), metadata.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("unchanged credits or people were rewritten: before=%+v after=%+v err=%v", before, after, err)
	}
	afterIdentifiers, err := repo.ListIdentifiers(t.Context(), before[0].PersonID)
	if err != nil || !reflect.DeepEqual(identifiers, afterIdentifiers) {
		t.Fatal("unchanged identifiers were rewritten", err)
	}
	credits[0].SortOrder = 5
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, credits); err != nil {
		t.Fatal(err)
	}
	after, err = repo.ListCreditsWithPeople(t.Context(), metadata.ID)
	if err != nil || len(after) != 1 || after[0].ID != before[0].ID || after[0].Role != "英雄" || after[0].SortOrder != 5 || !reflect.DeepEqual(after[0].Person, before[0].Person) {
		t.Fatal("sort change must preserve credit identity, translation and person", err)
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
	director := CreditInput{Provider: "tmdb", ExternalID: "2", Name: "Director", Type: model.CreditTypeDirector}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeDirector}, []CreditInput{director}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListCredits(t.Context(), metadata.ID)
	if err != nil || len(rows) != 2 {
		t.Fatal("unloaded scope must preserve all credits", err)
	}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, nil); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.ListCredits(t.Context(), metadata.ID)
	if err != nil || len(rows) != 1 || rows[0].Type != model.CreditTypeDirector {
		t.Fatal("empty loaded scope must delete only its own credits", err)
	}
	if err := db.Delete(&model.Person{}, "id = ?", before[0].PersonID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&model.PersonIdentifier{}, "person_id = ?", before[0].PersonID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, credits); err != nil {
		t.Fatal("removed credit must be able to reappear", err)
	}
	person, err := repo.FindByID(t.Context(), before[0].PersonID)
	if err != nil || person == nil {
		t.Fatal("soft-deleted person was not restored", err)
	}
	afterIdentifiers, err = repo.ListIdentifiers(t.Context(), before[0].PersonID)
	if err != nil || len(afterIdentifiers) != 1 || afterIdentifiers[0].ID != identifiers[0].ID {
		t.Fatal("soft-deleted identifier was not restored", err)
	}
	rows, err = repo.ListCredits(t.Context(), metadata.ID)
	if err != nil || len(rows) != 2 {
		t.Fatal("reappearing credit duplicated or erased another scope", err)
	}
}

func TestPersonSourceUpdatesOnlyChangesSourceFields(t *testing.T) {
	person := model.Person{Name: "译名", OriginalName: "Actor", NormalizedName: "actor", Source: "tmdb", ProfileURL: "old-url", ProfileImageKey: "old-key"}
	input := CreditInput{Name: "Actor", ProfileURL: "old-url"}
	if updates := personSourceUpdates(person, input, "tmdb"); len(updates) != 0 {
		t.Fatalf("unchanged source rewrites translation or image: %v", updates)
	}
	input.ProfileURL = "new-url"
	if updates := personSourceUpdates(person, input, "tmdb"); !reflect.DeepEqual(updates, map[string]any{"profile_url": "new-url"}) {
		t.Fatalf("failed image import must retain old key: %v", updates)
	}
	input.ProfileImageKey = "new-key"
	if updates := personSourceUpdates(person, input, "tmdb"); updates["profile_image_key"] != "new-key" || updates["profile_image_source_url"] != "new-url" || len(updates) != 3 {
		t.Fatalf("successful image import not applied: %v", updates)
	}
	input.ProfileURL = ""
	if updates := personSourceUpdates(person, input, "tmdb"); updates["profile_image_key"] != "" {
		t.Fatalf("removed profile retains key: %v", updates)
	}
	input.Name = "New Actor"
	person.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
	updates := personSourceUpdates(person, input, "tmdb")
	if _, ok := updates["deleted_at"]; !ok || updates["name"] != "New Actor" || updates["original_name"] != "New Actor" {
		t.Fatalf("source rename or soft-delete recovery lost: %v", updates)
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
