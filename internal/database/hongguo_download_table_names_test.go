package database

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoDownloadTableNamesMigration(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.HongGuoDownloadWork{}, &model.HongGuoDownload{}); err != nil {
		t.Fatal(err)
	}
	work := model.HongGuoDownloadWork{SourceID: "123", Title: "Synthetic", Root: "/synthetic", Directory: "2026/10/one"}
	leaseUntil := time.Now().Add(time.Hour)
	rows := []model.HongGuoDownload{
		{SourceID: "123", Episode: 1, Title: "Synthetic", Root: work.Root, RelativePath: "2026/10/one/S01E001.mp4", Status: "completed", Bytes: 2048, TotalBytes: 2048, Attempts: 2, SHA256: "synthetic-checksum", VerifiedSize: 2048, Source: "app", Quality: 1080, Width: 1080, Height: 1920, Codec: "hevc", Duration: 120},
		{SourceID: "123", Episode: 2, Title: "Synthetic", Root: work.Root, RelativePath: "2026/10/one/S01E002.mp4", Status: "waiting_verify", Bytes: 1024, RawSize: 1024, StagingPath: "downloading/synthetic", LeaseToken: "synthetic-lease", LeaseUntil: &leaseUntil, SourceErrors: map[string]string{"official": "synthetic error"}},
	}
	if err = db.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var beforeWork model.HongGuoDownloadWork
	var beforeRows []model.HongGuoDownload
	if err = db.First(&beforeWork).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Order("episode").Find(&beforeRows).Error; err != nil {
		t.Fatal(err)
	}
	pairs := [][2]string{{"hongguo_download_works", "hong_guo_download_works"}, {"hongguo_downloads", "hong_guo_downloads"}}
	oids := make([]int64, len(pairs))
	for i, names := range pairs {
		if err = db.Raw("SELECT CAST(? AS regclass)::oid", names[0]).Scan(&oids[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Migrator().RenameTable(names[0], names[1]); err != nil {
			t.Fatal(err)
		}
		if err = db.Exec("ALTER INDEX " + names[0] + "_pkey RENAME TO " + names[1] + "_pkey").Error; err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		// 使用完整启动入口，验证重命名先于 AutoMigrate，重启不会创建空的替代表。
		if err = AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		for i, names := range pairs {
			if db.Migrator().HasTable(names[1]) || !db.Migrator().HasTable(names[0]) {
				t.Fatalf("unexpected table names: %v", names)
			}
			var oid int64
			if err = db.Raw("SELECT CAST(? AS regclass)::oid", names[0]).Scan(&oid).Error; err != nil || oid != oids[i] {
				t.Fatalf("table was replaced: %s %v", names[0], err)
			}
		}
		var savedWork model.HongGuoDownloadWork
		var savedRows []model.HongGuoDownload
		if err = db.First(&savedWork).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Order("episode").Find(&savedRows).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(savedWork, beforeWork) || !reflect.DeepEqual(savedRows, beforeRows) {
			t.Fatal("download records changed during migration")
		}
	}
	for _, name := range []string{"uidx_hg_download_episode", "idx_hg_download_due", "idx_hg_download_transfer_claim", "idx_hg_download_verification_claim"} {
		if !db.Migrator().HasIndex(&model.HongGuoDownload{}, name) {
			t.Fatalf("missing index: %s", name)
		}
	}
	if err = db.Create(&model.HongGuoDownload{SourceID: "123", Episode: 1, Status: "queued"}).Error; err == nil {
		t.Fatal("episode uniqueness was lost")
	}
}

func TestHongGuoDownloadTableNamesFreshDatabase(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"hong_guo_download_works", "hongguo_download_works"}, {"hong_guo_downloads", "hongguo_downloads"}} {
		if db.Migrator().HasTable(pair[0]) || !db.Migrator().HasTable(pair[1]) {
			t.Fatalf("unexpected fresh table names: %v", pair)
		}
	}
}

func TestHongGuoDownloadTableNamesConflictRollsBack(t *testing.T) {
	for _, target := range []string{"hongguo_download_works", "hongguo_downloads"} {
		t.Run(target, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"hong_guo_download_works", "hong_guo_downloads", target} {
				if err = db.Exec("CREATE TABLE " + name + " (id text PRIMARY KEY, payload text)").Error; err != nil {
					t.Fatal(err)
				}
				if err = db.Exec("INSERT INTO " + name + " VALUES ('synthetic','preserve')").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err = AutoMigrate(db); err == nil || !strings.Contains(err.Error(), "重命名冲突") {
				t.Fatalf("expected conflict: %v", err)
			}
			for _, name := range []string{"hong_guo_download_works", "hong_guo_downloads", target} {
				var payload string
				if err = db.Table(name).Select("payload").Scan(&payload).Error; err != nil || payload != "preserve" {
					t.Fatalf("conflicting data changed: %s %v", name, err)
				}
			}
			if target == "hongguo_downloads" && db.Migrator().HasTable("hongguo_download_works") {
				t.Fatal("first rename was not rolled back")
			}
		})
	}
}
