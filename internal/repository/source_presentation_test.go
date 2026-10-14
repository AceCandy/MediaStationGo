package repository

import (
	"context"
	"errors"
	"gorm.io/driver/postgres"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestSourcePresentationReadsCurrentBindings(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := New(db)
	lib := model.Library{Name: "来源测试", Type: "movie", Path: t.TempDir(), Enabled: true}
	item := model.MetadataItem{Kind: "movie", Title: "旧人工标题", Overview: "旧人工简介", Source: "manual"}
	for _, row := range []any{&lib, &item} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	media := model.Media{LibraryID: lib.ID, MetadataID: item.ID, Path: lib.Path + "/movie.mkv", Title: "扫描标题"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	tmdb := model.TMDbSubject{Kind: "movie", TMDbID: 101, Title: "来源标题", ReleaseDate: "2024-01-02", Fields: `{"genres":[{"name":"剧情"}]}`}
	payload := `{"data":{"title":"豆瓣标题","intro":"豆瓣简介","countries":["中国"],"year":"2023"}}`
	douban := model.DoubanSubject{DoubanID: "201", EntityKind: "movie", Payload: &payload, Rating: 8}
	for _, row := range []any{&tmdb, &douban,
		&model.MetadataIdentifier{MetadataID: item.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "101"},
		&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: "movie", ExternalID: "201"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	view, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Title != "来源标题" || view.Overview != "豆瓣简介" || view.Year != 2024 || view.Genres != "剧情" || view.Countries != "中国" || view.DoubanRating != 8 || view.Rating != 0 {
		t.Fatalf("unexpected source presentation: %+v", view)
	}
	ambiguous := model.MetadataIdentifier{MetadataID: item.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "102"}
	if err := db.Create(&ambiguous).Error; err != nil {
		t.Fatal(err)
	}
	view, err = repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view.Title != "豆瓣标题" {
		t.Fatalf("ambiguous source was selected: %+v %v", view, err)
	}
	if err := db.Delete(&ambiguous).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&tmdb).Updates(map[string]any{"title": "实时新标题", "overview": "实时简介"}).Error; err != nil {
		t.Fatal(err)
	}
	view, err = repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view.Title != "实时新标题" || view.Overview != "实时简介" {
		t.Fatalf("source update not visible: %+v %v", view, err)
	}
	results, _, err := repos.MediaView.SearchFilteredPage(t.Context(), "实时新标题", 0, 20, MediaQueryFilter{})
	if err != nil || len(results) != 1 || results[0].Title != "实时新标题" {
		t.Fatalf("search differs from detail: %+v %v", results, err)
	}
	_, _, _, err = repos.MediaView.ListLibraryMetadataPage(t.Context(), lib.ID, "movie", "", 0, 20, MediaQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where("metadata_id=? AND provider='tmdb'", item.ID).Delete(&model.MetadataIdentifier{}).Error; err != nil {
		t.Fatal(err)
	}
	view, err = repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view.Title != "豆瓣标题" || view.Overview != "豆瓣简介" {
		t.Fatalf("removed binding still contributes: %+v %v", view, err)
	}
	if err := db.Where("metadata_id=?", item.ID).Delete(&model.MetadataIdentifier{}).Error; err != nil {
		t.Fatal(err)
	}
	view, err = repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view.Title != "扫描标题" || view.Overview != "" {
		t.Fatalf("legacy values leaked: %+v %v", view, err)
	}
}

func TestSourceRebindingClearsDescendantsAtomically(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := New(db)
	series := model.MetadataItem{Kind: "series"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1}
	other := model.MetadataItem{Kind: "series"}
	for _, item := range []*model.MetadataItem{&episode, &other} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	bindings := []model.MetadataIdentifier{
		{MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "1"},
		{MetadataID: season.ID, Provider: "tmdb", EntityKind: "season", ExternalID: "2"},
		{MetadataID: episode.ID, Provider: "tmdb", EntityKind: "episode", ExternalID: "3"},
		{MetadataID: other.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "4"},
	}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.ReplaceIdentifier(t.Context(), series.ID, "tmdb", "series", "4"); err == nil {
		t.Fatal("conflicting binding accepted")
	}
	var count int64
	if err := db.Model(&model.MetadataIdentifier{}).Where("metadata_id IN ?", []string{series.ID, season.ID, episode.ID}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("rollback lost bindings: %d %v", count, err)
	}
	if err := repos.Metadata.ReplaceIdentifier(t.Context(), series.ID, "tmdb", "series", "1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MetadataIdentifier{}).Where("metadata_id IN ?", []string{season.ID, episode.ID}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("same binding cleared children: %d %v", count, err)
	}
	if err := repos.Metadata.ReplaceIdentifier(t.Context(), series.ID, "tmdb", "series", "5"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MetadataIdentifier{}).Where("metadata_id IN ?", []string{season.ID, episode.ID}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("old child bindings remain: %d %v", count, err)
	}
	if err := repos.Metadata.RebindTMDb(t.Context(), episode, "3", []byte(`{"id":3}`)); !errors.Is(err, ErrMetadataAssociationChanged) {
		t.Fatalf("stale child response accepted: %v", err)
	}
	var kept model.MetadataItem
	if err := db.First(&kept, "id=?", episode.ID).Error; err != nil || kept.ParentID == nil || *kept.ParentID != season.ID {
		t.Fatalf("local identity changed: %+v %v", kept, err)
	}
}

// ensureSourcePresentationTestSchema 为精简仓储测试补齐普通展示依赖，不创建媒体或任务表。
func ensureSourcePresentationTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.ArtworkAsset{}, &model.PersonIdentifier{}, &model.TMDbPayload{}, &model.TranslationCache{}, &model.TMDbCredit{}, &model.TMDbSubject{}, &model.TMDbArtwork{}, &model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureMetadataPresentationSchema(db); err != nil {
		t.Fatal(err)
	}
}

func createSourceTestMetadata(t *testing.T, repos *Container, item model.MetadataItem, identifiers ...model.MetadataIdentifier) *model.MetadataItem {
	t.Helper()
	created := createTestMetadata(t, repos, item, identifiers...)
	if err := testdb.SaveMetadataPresentationFixture(repos.DB, *created); err != nil {
		t.Fatal(err)
	}
	return created
}

func saveSourceArtworkFixture(db *gorm.DB, metadataID, assetID, kind string) error {
	var subject model.TMDbSubject
	if err := db.Table("tmdb_subjects subject").Select("subject.*").Joins("JOIN metadata_identifiers binding ON binding.provider='tmdb' AND binding.entity_kind=subject.kind AND binding.external_id=subject.tmdb_id::text").Where("binding.metadata_id=?", metadataID).Take(&subject).Error; err != nil {
		return err
	}
	return db.Create(&model.TMDbArtwork{SubjectID: subject.ID, Type: kind, AssetID: &assetID, Status: "ready"}).Error
}

func TestSourceRebindingSerializesParentAndChild(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ensureSourcePresentationTestSchema(t, db)
	series := model.MetadataItem{Kind: "series"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&episode, "id=?", episode.ID).Error; err != nil {
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
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	child, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := child.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
		t.Fatal(err)
	}
	parent := db.WithContext(ctx).Begin()
	defer parent.Rollback()
	if err := parent.Exec(`SET LOCAL search_path TO "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	if err := lockMetadataHierarchy(parent, series.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- (&MetadataRepository{db: child}).RebindTMDb(ctx, episode, "3", []byte(`{"id":3}`)) }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("child bypassed parent lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
			var blocked bool
			if err := parent.Raw("SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=? AND NOT granted)", pid).Scan(&blocked).Error; err != nil {
				t.Fatal(err)
			}
			if !blocked {
				continue
			}
			if err := (&MetadataRepository{db: parent}).ReplaceIdentifier(ctx, series.ID, "tmdb", "series", "5"); err != nil {
				t.Fatal(err)
			}
			if err := parent.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, ErrMetadataAssociationChanged) {
				t.Fatalf("stale child write=%v", err)
			}
			var count int64
			if err := child.Model(&model.MetadataIdentifier{}).Where("metadata_id=?", episode.ID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("stale binding persisted: %d %v", count, err)
			}
			return
		}
	}
}

func TestSourceChangesRegisterTMDbRecheck(t *testing.T) {
	db := recheckQueueDB(t)
	series := model.MetadataItem{Kind: "series"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	if err := testdb.SaveMetadataPresentationFixture(db, season); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE tm_db_recheck_changes SET pending=false, expand=false").Error; err != nil {
		t.Fatal(err)
	}
	var subject model.TMDbSubject
	if err := db.Where("kind='season'").First(&subject).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&subject).Update("overview", "来源新简介").Error; err != nil {
		t.Fatal(err)
	}
	var change model.TMDbRecheckChange
	if err := db.First(&change, "metadata_id=?", season.ID).Error; err != nil || !change.Pending {
		t.Fatalf("source update not registered: %+v %v", change, err)
	}
	if err := db.Exec("UPDATE tm_db_recheck_changes SET pending=false").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TMDbArtwork{SubjectID: subject.ID, Type: "poster", SourcePath: "/poster.jpg", Status: "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&change, "metadata_id=?", season.ID).Error; err != nil || !change.Pending {
		t.Fatalf("source artwork not registered: %+v %v", change, err)
	}
}

func TestSourceCreditsRefreshWithoutMediaCopy(t *testing.T) {
	repos := newMetadataMergeTestRepository(t)
	if err := repos.DB.AutoMigrate(&model.TMDbPayload{}); err != nil {
		t.Fatal(err)
	}
	item := createTestMetadata(t, repos, model.MetadataItem{Kind: "movie"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "movie", ExternalID: "42"})
	now := time.Now().UTC()
	if err := repos.Metadata.SaveProviderData(t.Context(), item.ID, "tmdb", []byte(`{"id":42,"title":"电影","credits":{"cast":[{"id":1,"name":"Actor","character":"Hero"}],"crew":[]}}`), now); err != nil {
		t.Fatal(err)
	}
	rows, err := repos.Person.ListCreditsWithPeople(t.Context(), item.ID)
	if err != nil || len(rows) != 1 || rows[0].Person.Name != "Actor" || rows[0].Role != "Hero" {
		t.Fatalf("source credits=%+v %v", rows, err)
	}
	cache := model.TranslationCache{Kind: "role", ContextKey: rows[0].SourceSubjectID, SourceText: "Hero", TargetLanguage: "zh-CN", PromptVersion: "test", TranslatedText: "英雄"}
	if err := repos.Person.SaveAndApplyTranslation(t.Context(), cache, []TranslationTarget{{Kind: "role", ID: rows[0].ID, OriginalText: "Hero"}}); err != nil {
		t.Fatal(err)
	}
	rows, err = repos.Person.ListCreditsWithPeople(t.Context(), item.ID)
	if err != nil || len(rows) != 1 || rows[0].Role != "英雄" {
		t.Fatalf("source role translation=%+v %v", rows, err)
	}

	personID := rows[0].PersonID
	nameCache := model.TranslationCache{Kind: "person_name", ContextKey: personID, SourceText: "Actor", TargetLanguage: "zh-CN", PromptVersion: "test", TranslatedText: "演员"}
	if err := repos.DB.Create(&nameCache).Error; err != nil {
		t.Fatal(err)
	}
	person, err := repos.Person.FindByID(t.Context(), personID)
	if err != nil || person.Name != "演员" {
		t.Fatalf("translated person=%+v %v", person, err)
	}
	if err := repos.DB.Model(&model.TMDbPersonProfile{}).Where("person_id = ?", personID).Updates(map[string]any{"name": "Updated Actor", "original_name": "Updated Actor", "biography": "最新简介"}).Error; err != nil {
		t.Fatal(err)
	}
	rows, err = repos.Person.ListCreditsWithPeople(t.Context(), item.ID)
	if err != nil || len(rows) != 1 || rows[0].Person.Name != "Updated Actor" || rows[0].Person.Overview != "最新简介" {
		t.Fatalf("stale source person=%+v %v", rows, err)
	}
	person, err = repos.Person.FindByID(t.Context(), personID)
	if err != nil || person.Name != "Updated Actor" {
		t.Fatalf("stale translated person=%+v %v", person, err)
	}
	if err := repos.Metadata.SaveProviderData(t.Context(), item.ID, "tmdb", []byte(`{"id":42,"title":"电影","credits":{"cast":[{"id":2,"name":"New Actor","character":"Villain"}],"crew":[]}}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rows, err = repos.Person.ListCreditsWithPeople(t.Context(), item.ID)
	if err != nil || len(rows) != 1 || rows[0].Person.Name != "New Actor" || rows[0].Role != "Villain" {
		t.Fatalf("stale credits=%+v %v", rows, err)
	}
}

// 未入媒体库的来源演职员仍需翻译；大批已本地化角色不能拖慢领取。
func TestUnboundSourceRoleTranslationCandidates(t *testing.T) {
	repos := newMetadataMergeTestRepository(t)
	db := repos.DB
	person := model.Person{Name: "演员", OriginalName: "演员", Source: "tmdb"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	profile := model.TMDbPersonProfile{TMDbID: 801, PersonID: person.ID, Name: "演员"}
	subject := model.TMDbSubject{Kind: "movie", TMDbID: 802, Title: "未入库电影"}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&subject).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO tmdb_credits(id,subject_id,person_profile_id,origin,credit_key,category,character)
 SELECT lpad(i::text,8,'0'),?,?,'subject',i::text,'cast',CASE WHEN i<=30000 THEN '中文角色' ELSE 'Hero' END
 FROM generate_series(1,30010) i`, subject.ID, profile.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE tmdb_credits").Error; err != nil {
		t.Fatal(err)
	}
	rows, err := repos.Person.ListPendingRoleTranslations(t.Context(), "zh-CN", "v1", "", "", 5)
	if err != nil || len(rows) != 5 || rows[0].Subject.Title != "未入库电影" {
		t.Fatalf("unbound roles=%+v %v", rows, err)
	}
	var plan []struct {
		Line string `gorm:"column:QUERY PLAN"`
	}
	if err := db.Raw(`EXPLAIN (ANALYZE) SELECT id FROM tmdb_credits WHERE origin='subject' AND category='cast'
 AND character<>'' AND character !~ '[一-鿿]' AND (subject_id,id)>('','') ORDER BY subject_id,id LIMIT 5`).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	indexed := false
	for _, line := range plan {
		if strings.Contains(line.Line, "idx_tmdb_credits_pending_roles") {
			indexed = true
		}
	}
	if !indexed {
		t.Fatalf("pending source role index missing: %+v", plan)
	}
	cache := model.TranslationCache{Kind: "role", ContextKey: subject.ID, SourceText: "Hero", TargetLanguage: "zh-CN", PromptVersion: "v1", TranslatedText: "英雄"}
	if err := db.Create(&cache).Error; err != nil {
		t.Fatal(err)
	}
	rows, err = repos.Person.ListPendingRoleTranslations(t.Context(), "zh-CN", "v2", "", "", 5)
	if err != nil || len(rows) != 0 {
		t.Fatalf("translated roles requeued=%+v %v", rows, err)
	}
}
