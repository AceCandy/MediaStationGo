package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMediaUpsertConcurrentPath(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	libraries := map[string]model.Library{}
	for _, source := range []string{"", model.TaskSystemHongGuo, model.CatalogSourceNFO} {
		kind := "tv"
		if source == model.TaskSystemHongGuo {
			kind = model.LibraryTypeHongGuo
		} else if source == model.CatalogSourceNFO {
			kind = model.LibraryTypeNFOTV
		}
		lib := model.Library{Name: kind, Path: "/concurrent/" + kind, Type: kind}
		if err := db.Create(&lib).Error; err != nil {
			t.Fatal(err)
		}
		libraries[source] = lib
	}
	const sourceID = "900000000000000001"
	if _, err := New(db).HongGuo.SaveDetail(t.Context(), hongguo.Work{SourceID: sourceID, Title: "并发测试", EpisodeCount: 1, TotalEpisodes: 1, Snapshot: []byte(`{}`)}); err != nil {
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
	pool.SetMaxOpenConns(3)
	for _, sources := range [][2]string{{"", ""}, {"hongguo", "hongguo"}, {"nfo", "nfo"}, {"", "hongguo"}, {"hongguo", ""}, {"nfo", "hongguo"}, {"hongguo", "nfo"}, {"nfo", ""}, {"", "nfo"}} {
		t.Run(fmt.Sprintf("%s_then_%s", sources[0], sources[1]), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			arrived := make(chan struct{}, 2)
			release := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
			done := [2]chan error{make(chan error, 1), make(chan error, 1)}
			start := make(chan struct{})
			// NFO 同源已有事务级路径锁，第二个事务应在 INSERT 之前等待。
			synchronizeInsert := sources != [2]string{"nfo", "nfo"}
			media := [2]model.Media{}
			var verify *gorm.DB
			for i, source := range sources {
				conn, err := pool.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
					t.Fatal(err)
				}
				worker, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
				if err != nil {
					t.Fatal(err)
				}
				verify = worker
				if err := worker.Callback().Create().Before("gorm:create").Register("test:concurrent-media", func(tx *gorm.DB) {
					if tx.Statement.Table != "media" || !synchronizeInsert {
						return
					}
					arrived <- struct{}{}
					select {
					case <-release[i]:
					case <-ctx.Done():
						tx.AddError(ctx.Err())
					}
				}); err != nil {
					t.Fatal(err)
				}
				media[i] = model.Media{LibraryID: libraries[source].ID, CatalogSource: source, Path: "/concurrent/" + t.Name() + ".strm", LookupCatalogID: sourceID, SeasonNum: 1, EpisodeNum: 1}
				go func() {
					<-start
					if source == model.CatalogSourceNFO {
						_, err := New(worker).NFO.Ingest(ctx, &media[i], nil)
						done[i] <- err
					} else {
						done[i] <- (&MediaRepository{db: worker}).Upsert(ctx, &media[i])
					}
				}()
			}
			close(start)
			// 两个独立事务均查无路径后，再依次提交，固定复现首次插入竞争。
			if synchronizeInsert {
				for range 2 {
					select {
					case <-arrived:
					case <-ctx.Done():
						t.Fatal("writers did not reach insert barrier")
					}
				}
			}
			close(release[0])
			firstErr := <-done[0]
			close(release[1])
			secondErr := <-done[1]
			if firstErr != nil {
				t.Fatalf("first writer: %v", firstErr)
			}
			if sources[0] == sources[1] {
				if secondErr != nil || media[0].ID != media[1].ID {
					t.Fatalf("same-path reuse: ids=%q/%q err=%v", media[0].ID, media[1].ID, secondErr)
				}
			} else if secondErr == nil {
				t.Fatal("cross-catalog path accepted")
			}
			var rows []model.Media
			if err := verify.Where("path = ?", media[0].Path).Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].ID != media[0].ID || rows[0].CatalogSource != sources[0] {
				t.Fatalf("saved path identity changed: rows=%+v err=%v", rows, err)
			}
			var bindings []model.HongGuoMediaBinding
			if err := verify.Where("media_id = ?", media[0].ID).Find(&bindings).Error; err != nil {
				t.Fatal(err)
			}
			if sources[0] == model.TaskSystemHongGuo {
				if len(bindings) != 1 || bindings[0].EpisodeID == nil || rows[0].ScrapeStatus != "matched" {
					t.Fatalf("missing HongGuo binding: %+v", bindings)
				}
			} else if len(bindings) != 0 {
				t.Fatal("rejected HongGuo writer left a binding")
			}
		})
	}
}

func TestMediaUpsertDoesNotIgnoreOtherInsertErrors(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repo := &MediaRepository{db: db}
	first := model.Media{Path: "/first.mkv", Title: "保留原文件"}
	if err := repo.Upsert(t.Context(), &first); err != nil {
		t.Fatal(err)
	}
	incoming := model.Media{PermanentBase: model.PermanentBase{ID: first.ID}, Path: "/second.mkv"}
	var pgErr *pgconn.PgError
	if err := repo.Upsert(t.Context(), &incoming); !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName == "idx_media_path" {
		t.Fatalf("non-path insert conflict was hidden: %v", err)
	}
	var rows []model.Media
	if err := db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].Path != first.Path || rows[0].Title != first.Title {
		t.Fatalf("failed insert changed existing row: %+v %v", rows, err)
	}
}
