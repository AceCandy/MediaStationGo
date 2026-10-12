package database

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTMDbCatalogSourceNotificationUsesFullIdentityIndex(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// 大样本先于触发器安装，避免夹具生成本身注册三万次复查。
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	local := model.MetadataItem{Kind: "series", Title: "目标作品"}
	other := model.MetadataItem{Kind: "series", Title: "无关作品"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO metadata_identifiers(id,created_at,updated_at,metadata_id,provider,entity_kind,external_id)
		SELECT 'perf-' || n,now(),now(),?, 'tmdb','series',n::text FROM generate_series(1,30000) n`, local.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []model.MetadataIdentifier{
		{MetadataID: other.ID, Provider: "douban", EntityKind: "series", ExternalID: "25000"},
		{MetadataID: other.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "25000"},
		{MetadataID: other.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "025000"},
		{MetadataID: other.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "invalid"},
		{MetadataID: other.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "10000000000"},
	} {
		if err := db.Create(&id).Error; err != nil {
			t.Fatal(err)
		}
	}
	subject := model.TMDbSubject{PermanentBase: model.PermanentBase{ID: "perf-source-series"}, Kind: "series", TMDbID: 25000, Fields: "{}"}
	if err := db.Create(&subject).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := ensureTMDbCatalogSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE metadata_identifiers").Error; err != nil {
		t.Fatal(err)
	}
	// 从已安装函数抽取实际查询，防止测试查询与生产触发器漂移。
	var definition string
	if err := db.Raw("SELECT pg_get_functiondef('tmdb_catalog_source_changed()'::regprocedure)").Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	begin, end := strings.Index(definition, "SELECT mid.metadata_id"), strings.Index(definition, " LOOP")
	if begin < 0 || end <= begin {
		t.Fatal("source lookup query not found in installed trigger")
	}
	lookup := strings.ReplaceAll(definition[begin:end], "NEW.subject_id", "$1")
	if err := db.Exec("SET plan_cache_mode = force_generic_plan").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PREPARE tmdb_source_lookup(text) AS " + lookup).Error; err != nil {
		t.Fatal(err)
	}
	var lines []string
	if err := db.Raw("EXPLAIN (ANALYZE,BUFFERS) EXECUTE tmdb_source_lookup('perf-source-series')").Scan(&lines).Error; err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	t.Log(plan)
	fullIdentity := false
	for _, line := range lines {
		if strings.Contains(line, "Index Cond:") && strings.Contains(line, "provider") && strings.Contains(line, "entity_kind") && strings.Contains(line, "external_id") {
			fullIdentity = true
		}
	}
	if !fullIdentity || !strings.Contains(plan, "uidx_metadata_identifier") || strings.Contains(plan, "Rows Removed by Filter") || strings.Contains(plan, "Seq Scan on metadata_identifiers") {
		t.Fatalf("source notification scans unrelated identities:\n%s", plan)
	}

	clearChanges := func() {
		t.Helper()
		if err := db.Exec("DELETE FROM tm_db_recheck_changes").Error; err != nil {
			t.Fatal(err)
		}
	}
	assertChanges := func(want int64) {
		t.Helper()
		var total, target int64
		if err := db.Model(&model.TMDbRecheckChange{}).Where("pending").Count(&total).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.TMDbRecheckChange{}).Where("pending AND metadata_id = ?", local.ID).Count(&target).Error; err != nil {
			t.Fatal(err)
		}
		if total != want || target != want {
			t.Fatalf("wrong notification ownership: total=%d target=%d want=%d", total, target, want)
		}
	}
	payload := func(id *string) model.TMDbPayload {
		return model.TMDbPayload{SubjectID: id, Component: "base", Language: "und", Body: "{}", Origin: "api", FetchedAt: time.Now()}
	}
	clearChanges()
	row := payload(&subject.ID)
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	assertChanges(1)
	clearChanges()
	rollback := errors.New("rollback source and notification")
	if err := db.Transaction(func(tx *gorm.DB) error {
		row := payload(&subject.ID)
		row.Language = "zh-CN"
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	assertChanges(0)
	person := model.Person{Name: "来源人物"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	profile := model.TMDbPersonProfile{TMDbID: 25000, PersonID: person.ID, Fields: "{}"}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	row = payload(nil)
	row.PersonProfileID = &profile.ID
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	assertChanges(0)
	// 保留旧查询的 1–10 位限制，超长数字不通知任何本地作品。
	long := model.TMDbSubject{Kind: "series", TMDbID: 10000000000, Fields: "{}"}
	if err := db.Create(&long).Error; err != nil {
		t.Fatal(err)
	}
	row = payload(&long.ID)
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	assertChanges(0)
}
