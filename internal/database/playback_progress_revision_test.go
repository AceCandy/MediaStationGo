package database

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPlaybackProgressRevision(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePlaybackProgressRevisionTriggers(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO metadata_items(id,kind,title,source) VALUES ('item','movie','Item','local'),('other','movie','Other','local')`).Error; err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"legacy", "nfo", "hongguo", "huangguoai"} {
		t.Run(source, func(t *testing.T) {
			id := repository.ProgressIdentity{UserID: "user", Source: source, ItemID: "item", MediaID: "file"}
			if source == "hongguo" || source == "huangguoai" {
				id.EpisodeNumber = 1
			}
			read := func() repository.PlaybackProgressSnapshot {
				t.Helper()
				state, err := repository.ReadPlaybackProgress(t.Context(), db, id, repository.MediaQueryFilter{})
				if err != nil {
					t.Fatal(err)
				}
				return state
			}
			write := func(expected, position int64, played bool) int64 {
				t.Helper()
				rev, err := repository.WritePlaybackProgress(t.Context(), db, id, expected, position, 120000, played)
				if err != nil {
					t.Fatal(err)
				}
				return rev
			}
			initial := read()
			if initial.Revision != 0 || initial.PositionMs != 0 || initial.Completed {
				t.Fatalf("initial: %+v", initial)
			}
			rev := write(0, 1000, false)
			state := read()
			if state.PositionMs != 1000 || state.Revision != rev {
				t.Fatalf("short: %+v", state)
			}
			if _, err := repository.WritePlaybackProgress(t.Context(), db, id, 0, 9999, 120000, true); !errors.Is(err, repository.ErrPlaybackProgressConflict) {
				t.Fatalf("stale err=%v", err)
			}
			if state = read(); state.PositionMs != 1000 || state.Revision != rev {
				t.Fatalf("conflict changed state: %+v", state)
			}
			rev = write(rev, 0, false)
			if state = read(); state.PositionMs != 0 || state.Completed {
				t.Fatalf("clear: %+v", state)
			}
			rev = write(rev, 3000, true)
			if state = read(); state.PositionMs != 3000 || !state.Completed {
				t.Fatalf("watched replay: %+v", state)
			}
			table, column := "playback_histories", "metadata_id"
			switch source {
			case "nfo":
				table, column = "nfo_user_states", "item_id"
			case "hongguo":
				table, column = "hongguo_user_states", "source_id"
			case "huangguoai":
				table, column = "huangguoai_user_states", "source_id"
			}
			if source == "nfo" {
				if err := db.Table(table).Where("user_id=? AND item_id=?", id.UserID, id.ItemID).Updates(map[string]any{"favorite": true, "updated_at": time.Now()}).Error; err != nil {
					t.Fatal(err)
				}
				if read().Revision != rev {
					t.Fatal("favorite advanced revision")
				}
			}
			// 不经过新同步接口的旧写入也必须使版本失效。
			if err := db.Table(table).Where("user_id=? AND "+column+"=?", id.UserID, id.ItemID).Update("position_ms", 4000).Error; err != nil {
				t.Fatal(err)
			}
			if read().Revision <= rev {
				t.Fatal("ordinary writer did not advance revision")
			}
			rev = read().Revision
			if err := db.Exec("DELETE FROM "+table+" WHERE user_id=? AND "+column+"=?", id.UserID, id.ItemID).Error; err != nil {
				t.Fatal(err)
			}
			deleted := read()
			if deleted.Revision <= rev || deleted.PositionMs != 0 || deleted.Completed {
				t.Fatalf("tombstone: %+v", deleted)
			}
			if _, err := repository.WritePlaybackProgress(t.Context(), db, id, rev, 4000, 120000, false); !errors.Is(err, repository.ErrPlaybackProgressConflict) {
				t.Fatalf("deleted stale err=%v", err)
			}
			var count int64
			db.Table(table).Where("user_id=? AND "+column+"=?", id.UserID, id.ItemID).Count(&count)
			if count != 0 || read().Revision != deleted.Revision {
				t.Fatal("conflict left placeholder or advanced tombstone")
			}
			write(deleted.Revision, 0, false)
		})
	}
	legacy := repository.ProgressIdentity{UserID: "user", Source: "legacy", ItemID: "item", MediaID: "file"}
	before, err := repository.ReadPlaybackProgress(t.Context(), db, legacy, repository.MediaQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id=? AND metadata_id=?", legacy.UserID, legacy.ItemID).Delete(&model.PlaybackHistory{}).Error; err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.ReadPlaybackProgress(t.Context(), db, legacy, repository.MediaQueryFilter{})
	if err != nil || deleted.Revision <= before.Revision || deleted.PositionMs != 0 {
		t.Fatalf("soft delete: %+v err=%v", deleted, err)
	}
	if _, err := repository.WritePlaybackProgress(t.Context(), db, legacy, before.Revision, 1000, 120000, false); !errors.Is(err, repository.ErrPlaybackProgressConflict) {
		t.Fatalf("soft-deleted stale: %v", err)
	}
	rebuilt, err := repository.WritePlaybackProgress(t.Context(), db, legacy, deleted.Revision, 1000, 120000, false)
	if err != nil || rebuilt <= deleted.Revision {
		t.Fatalf("rebuild revision=%d err=%v", rebuilt, err)
	}
	if err := db.Model(&model.PlaybackHistory{}).Where("user_id=? AND metadata_id=?", legacy.UserID, legacy.ItemID).Update("metadata_id", "other").Error; err != nil {
		t.Fatal(err)
	}
	oldIdentity, err := repository.ReadPlaybackProgress(t.Context(), db, legacy, repository.MediaQueryFilter{})
	if err != nil || oldIdentity.Revision <= rebuilt {
		t.Fatalf("old merged identity: %+v err=%v", oldIdentity, err)
	}
	legacy.ItemID = "other"
	newIdentity, err := repository.ReadPlaybackProgress(t.Context(), db, legacy, repository.MediaQueryFilter{})
	if err != nil || newIdentity.Revision == 0 || newIdentity.PositionMs != 1000 {
		t.Fatalf("new merged identity: %+v err=%v", newIdentity, err)
	}
	for _, table := range []string{"playback_events", "nfo_playback_events", "hongguo_playback_events", "huangguoai_playback_events"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("events %s count=%d err=%v", table, count, err)
		}
	}
}

func TestPlaybackProgressConcurrentFirstWrite(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO metadata_items(id,kind,title,source) VALUES ('race','movie','Race','local')`).Error; err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	second, err := gorm.Open(postgres.Open(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := second.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err := second.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"legacy", "nfo", "hongguo", "huangguoai"} {
		id := repository.ProgressIdentity{UserID: "race-user", Source: source, ItemID: "race", MediaID: "file"}
		if source == "hongguo" || source == "huangguoai" {
			id.EpisodeNumber = 1
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, conn := range []*gorm.DB{db, second} {
			wg.Add(1)
			go func(conn *gorm.DB) {
				defer wg.Done()
				<-start
				_, err := repository.WritePlaybackProgress(t.Context(), conn, id, 0, 1000, 120000, false)
				results <- err
			}(conn)
		}
		close(start)
		wg.Wait()
		close(results)
		wins, conflicts := 0, 0
		for err := range results {
			if err == nil {
				wins++
			} else if errors.Is(err, repository.ErrPlaybackProgressConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatalf("%s winners=%d conflicts=%d", source, wins, conflicts)
		}
	}
}
