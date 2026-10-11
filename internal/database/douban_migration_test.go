package database

import (
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestDoubanMigrationPreservesIdentityQualityAndTime(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}, &model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	newer := old.Add(time.Hour)
	for _, row := range []struct {
		local, actual, payload string
		at                     time.Time
		degraded               bool
	}{
		{"movie", "", `{"title":"完整详情","rating":{"value":8.5}}`, old, false},
		{"series", "movie", `{"title":"降级资料","rating":{"value":7}}`, newer, true},
	} {
		item := model.MetadataItem{Kind: row.local, Title: row.local}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: row.local, DoubanEntityKind: row.actual, ExternalID: "100"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: item.ID, Provider: "douban", Payload: row.payload, Degraded: row.degraded, FetchedAt: row.at}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := migrateDoubanSubjects(db); err != nil {
			t.Fatal(err)
		}
	}
	var subjects []model.DoubanSubject
	if err := db.Find(&subjects).Error; err != nil {
		t.Fatal(err)
	}
	if len(subjects) != 1 || subjects[0].EntityKind != "movie" || subjects[0].Quality != model.DoubanQualityComplete || subjects[0].Rating != 8.5 || !subjects[0].FetchedAt.Equal(old) {
		t.Fatalf("migration: %#v", subjects)
	}
	fresh := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Model(&subjects[0]).Updates(map[string]any{"rating": 9.2, "fetched_at": fresh}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateDoubanSubjects(db); err != nil {
		t.Fatal(err)
	}
	var current model.DoubanSubject
	if err := db.First(&current).Error; err != nil || current.Rating != 9.2 || !current.FetchedAt.Equal(fresh) {
		t.Fatalf("existing subject overwritten: %#v %v", current, err)
	}
	var count int64
	if err := db.Model(&model.MetadataProviderSnapshot{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("old data changed: %d %v", count, err)
	}
}

func TestDoubanMigrationUsesSnapshotIdentityWithoutBinding(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}, &model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	want := map[string]string{}
	for _, row := range []struct {
		name, payload, bindingID, bindingKind, id, kind string
	}{
		{"unbound movie", `{"id":"201","type":"movie","rating":{"value":8.5}}`, "", "", "201", "movie"},
		{"unbound tv", `{"id":"202","type":"tv","is_tv":true}`, "", "", "202", "series"},
		{"subject", `{"subject":{"id":203,"is_tv":true}}`, "", "", "203", "series"},
		{"data", `{"data":{"id":"204","type":"movie"}}`, "", "", "204", "movie"},
		{"normalized numeric id", `{"id":205,"data":{"id":"00205","type":"movie"}}`, "", "", "205", "movie"},
		{"changed id", `{"id":"206","type":"movie"}`, "906", "", "206", "movie"},
		{"changed kind", `{"id":"207","type":"tv","is_tv":true}`, "207", "", "207", "series"},
		{"kind fallback", `{"id":"208","title":"旧资料"}`, "208", "series", "208", "series"},
		{"id fallback", `{"type":"tv","is_tv":true}`, "209", "series", "209", "series"},
		{"nested identity with empty root", `{"id":"","type":"subject","data":{"id":"210","type":"movie"}}`, "", "", "210", "movie"},
		{"missing identity", `{"r":0}`, "", "", "", ""},
		{"missing kind", `{"id":"220","title":"无类型"}`, "", "", "", ""},
		{"rebound missing kind", `{"id":"221","title":"改绑后缺少类型"}`, "921", "", "", ""},
		{"conflicting ids", `{"id":"230","type":"movie","subject":{"id":"231"}}`, "", "", "", ""},
		{"conflicting kinds", `{"id":"240","type":"movie","data":{"type":"tv"}}`, "", "", "", ""},
		{"invalid is_tv", `{"id":"250","type":"movie","is_tv":"false"}`, "", "", "", ""},
		{"zero id", `{"id":0,"type":"movie"}`, "", "", "", ""},
	} {
		item := model.MetadataItem{Kind: "movie", Title: row.name}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		if row.bindingID != "" {
			if err := db.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: "movie", DoubanEntityKind: row.bindingKind, ExternalID: row.bindingID}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: item.ID, Provider: "douban", Payload: row.payload, FetchedAt: at}).Error; err != nil {
			t.Fatal(err)
		}
		if row.id != "" {
			want[row.id+"/"+row.kind] = row.payload
		}
	}
	// 多个旧关联无法兜底，但不妨碍快照自身身份完整的资料入库。
	for i, payload := range []string{`{"title":"不明身份"}`, `{"id":"260","type":"movie"}`} {
		item := model.MetadataItem{Kind: "movie", Title: "多个旧关联"}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"270", "271"} {
			if i == 1 {
				id += "0"
			}
			if err := db.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: "movie", ExternalID: id}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: item.ID, Provider: "douban", Payload: payload, FetchedAt: at}).Error; err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			want["260/movie"] = payload
		}
	}
	var beforeSnapshots, afterSnapshots []model.MetadataProviderSnapshot
	var beforeBindings, afterBindings []model.MetadataIdentifier
	if err := db.Order("id").Find(&beforeSnapshots).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&beforeBindings).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrateDoubanSubjects(db); err != nil {
			t.Fatal(err)
		}
	}
	var subjects []model.DoubanSubject
	if err := db.Find(&subjects).Error; err != nil {
		t.Fatal(err)
	}
	if len(subjects) != len(want) {
		t.Fatalf("got %d shared subjects, want %d: %#v", len(subjects), len(want), subjects)
	}
	for _, subject := range subjects {
		payload, ok := want[subject.DoubanID+"/"+subject.EntityKind]
		if !ok || subject.Payload == nil || subject.FetchedAt == nil || !subject.FetchedAt.Equal(at) || subject.Quality != model.DoubanPayloadQuality([]byte(payload), false) || subject.Rating != model.DoubanRating(payload) {
			t.Fatalf("unexpected shared subject: %#v", subject)
		}
		var equal bool
		if err := db.Raw("SELECT ?::jsonb = ?::jsonb", *subject.Payload, payload).Scan(&equal).Error; err != nil || !equal {
			t.Fatalf("payload changed: %s %v", subject.DoubanID, err)
		}
	}
	if err := db.Order("id").Find(&afterSnapshots).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterBindings).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeSnapshots, afterSnapshots) || !reflect.DeepEqual(beforeBindings, afterBindings) {
		t.Fatal("migration changed old snapshots or media bindings")
	}
}

func TestDoubanMigrationPagesPastInvalidCandidates(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}, &model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	newer := old.Add(time.Minute)
	items := make([]model.MetadataItem, 1006)
	for i := range items {
		items[i] = model.MetadataItem{Kind: "movie", Title: "历史快照"}
	}
	if err := db.CreateInBatches(&items, 500).Error; err != nil {
		t.Fatal(err)
	}
	snapshots := make([]model.MetadataProviderSnapshot, len(items))
	for i := range snapshots {
		snapshots[i] = model.MetadataProviderSnapshot{MetadataID: items[i].ID, Provider: "douban", Payload: `{"id":"500","type":"movie","is_tv":true}`, FetchedAt: newer.Add(time.Minute)}
	}
	snapshots[1001].Payload, snapshots[1001].FetchedAt = `{"id":"500","type":"movie","rating":7}`, old
	snapshots[1002].Payload, snapshots[1002].FetchedAt = `{"id":"500","type":"movie","rating":8}`, newer
	snapshots[1003].Payload, snapshots[1003].FetchedAt = `{"id":"510","is_tv":false}`, newer
	snapshots[1004].Payload, snapshots[1004].Degraded, snapshots[1004].FetchedAt = `{"id":"600","type":"movie","rating":6}`, true, old
	snapshots[1005].Payload = `{"subject":{"id":"600","type":"movie","rating":9}}`
	if err := db.CreateInBatches(&snapshots, 500).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrateDoubanSubjects(db); err != nil {
			t.Fatal(err)
		}
	}
	var subjects []model.DoubanSubject
	if err := db.Order("douban_id").Find(&subjects).Error; err != nil {
		t.Fatal(err)
	}
	if len(subjects) != 3 || subjects[0].DoubanID != "500" || subjects[0].Rating != 8 || !subjects[0].FetchedAt.Equal(newer) || subjects[1].DoubanID != "510" || subjects[2].DoubanID != "600" || subjects[2].Quality != model.DoubanQualityDegraded || subjects[2].Rating != 6 || !subjects[2].FetchedAt.Equal(old) {
		t.Fatalf("pagination/quality selection: %#v", subjects)
	}
}

func TestDoubanRefreshTaskMigrationPreservesHistory(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	rows := []model.TaskExecution{
		{Kind: "artwork", Name: "豆瓣资料刷新", System: model.TaskSystemCatalog, Status: "completed", StartedAt: at, FinishedAt: &at, Metrics: `{"updated":3}`},
		{Kind: "artwork", Name: "豆瓣资料刷新", System: "", Status: "failed", StartedAt: at, FinishedAt: &at, Metrics: `{}`},
		{Kind: "artwork", Name: "豆瓣电影信息补齐", System: model.TaskSystemCatalog, Status: "completed", StartedAt: at, Metrics: `{}`},
		{Kind: "artwork", Name: "TMDb 图片本地化修复", System: model.TaskSystemCatalog, Status: "completed", StartedAt: at, Metrics: `{}`},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// 比较持久化后的原值，排除 JSONB 规范化和数据库微秒精度差异。
	for i := range rows {
		if err := db.First(&rows[i], "id = ?", rows[i].ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := migrateDoubanRefreshTasks(db); err != nil {
			t.Fatal(err)
		}
	}
	for i, before := range rows {
		var after model.TaskExecution
		if err := db.First(&after, "id = ?", before.ID).Error; err != nil {
			t.Fatal(err)
		}
		wantKind, wantSystem := before.Kind, before.System
		if i < 2 {
			wantKind, wantSystem = "douban_data_refresh", model.TaskSystemCommon
		}
		if after.Kind != wantKind || after.System != wantSystem || after.Name != before.Name || after.Status != before.Status || after.Metrics != before.Metrics || !after.StartedAt.Equal(before.StartedAt) || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Fatalf("migration changed unrelated history: before=%#v after=%#v", before, after)
		}
	}
}
