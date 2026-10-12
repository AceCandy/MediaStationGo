package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTMDbCatalogPreservesConcurrentPersonImage(t *testing.T) {
	for _, changedPath := range []bool{false, true} {
		t.Run(fmt.Sprint(changedPath), func(t *testing.T) {
			r := tmdbTestRepo(t)
			ctx := t.Context()
			at := time.Now().UTC().Truncate(time.Microsecond)
			personReq := TMDbSourceRequest{Kind: "person", TMDbID: 5, Component: "summary", Language: "zh-CN"}
			if err := r.TMDb.SaveResponse(ctx, personReq, []byte(`{"id":5,"name":"Actor","profile_path":"/a.jpg"}`), at, ""); err != nil {
				t.Fatal(err)
			}
			p, err := r.TMDb.PersonIdentity(ctx, 5)
			if err != nil {
				t.Fatal(err)
			}
			var schema string
			if err = r.DB.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
				t.Fatal(err)
			}
			peer, err := gorm.Open(postgres.Open(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN")), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := peer.DB()
			if err != nil {
				t.Fatal(err)
			}
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err = peer.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
				t.Fatal(err)
			}
			completed := false
			if err = r.DB.Callback().Query().After("gorm:query").Register("tmdb_test_image", func(db *gorm.DB) {
				if completed || db.Statement.Table != "tmdb_person_profiles" {
					return
				}
				completed = true
				if e := (&TMDbCatalogRepository{db: peer}).SavePersonImage(ctx, p, "downloaded.jpg", false); e != nil {
					db.AddError(e)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.DB.Callback().Query().Remove("tmdb_test_image") })
			path := "/a.jpg"
			if changedPath {
				path = "/b.jpg"
			}
			raw := []byte(fmt.Sprintf(`{"id":12,"credits":{"cast":[{"id":5,"name":"Updated actor","profile_path":%q}],"crew":[]}}`, path))
			if err = r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "movie", TMDbID: 12, Component: "detail", Language: "zh-CN"}, raw, at, ""); err != nil {
				t.Fatal(err)
			}
			if !completed {
				t.Fatal("image update did not interleave")
			}
			current, err := r.TMDb.PersonIdentity(ctx, 5)
			status := "ready"
			if changedPath {
				status = "pending"
			}
			if err != nil || current.ImageKey != "downloaded.jpg" || current.ImageSourcePath != "/a.jpg" || current.ImageStatus != status || current.ProfilePath != path || current.Name != "Updated actor" {
				t.Fatalf("concurrent result lost: %+v, %v", current, err)
			}
			// 旧路径下载即使晚到，也不能覆盖新路径状态。
			if changedPath {
				if err = r.TMDb.SavePersonImage(ctx, p, "late.jpg", false); err != nil {
					t.Fatal(err)
				}
				current, err = r.TMDb.PersonIdentity(ctx, 5)
				if err != nil || current.ImageKey != "downloaded.jpg" || current.ImageStatus != "pending" {
					t.Fatalf("old download won: %+v %v", current, err)
				}
			}
		})
	}
}

type tmdbBatchCounter struct {
	logger.Interface
	creditInserts, personReads, jobInserts, subjectReads int
}

func (c *tmdbBatchCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if strings.HasPrefix(sql, `INSERT INTO "tmdb_credits"`) {
		c.creditInserts++
	}
	if strings.Contains(sql, `FROM "tmdb_person_profiles"`) {
		c.personReads++
	}
	if strings.HasPrefix(sql, `INSERT INTO "tmdb_refresh_jobs"`) {
		c.jobInserts++
	}
	if strings.Contains(sql, `FROM "tmdb_subjects"`) {
		c.subjectReads++
	}
}

func TestTMDbCatalogBatchesCreditsAndRollsBack(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "person", TMDbID: 5, Component: "summary", Language: "zh-CN"}, []byte(`{"id":5,"name":"Actor"}`), at, ""); err != nil {
		t.Fatal(err)
	}
	cast := []map[string]any{}
	for i := 0; i < 501; i++ {
		cast = append(cast, map[string]any{"id": 5, "name": "Actor", "character": fmt.Sprint(i)})
	}
	cast = append(cast, cast[0])
	raw, _ := json.Marshal(map[string]any{"id": 12, "credits": map[string]any{"cast": cast, "crew": []any{}}})
	counter := &tmdbBatchCounter{Interface: logger.Default}
	catalog := &TMDbCatalogRepository{db: r.DB.Session(&gorm.Session{Logger: counter})}
	req := TMDbSourceRequest{Kind: "movie", TMDbID: 12, Component: "detail", Language: "zh-CN"}
	if err := catalog.SaveResponse(ctx, req, raw, at, ""); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := r.DB.Model(&model.TMDbCredit{}).Count(&count).Error; err != nil || count != 501 || counter.creditInserts != 2 || counter.personReads != 1 {
		t.Fatalf("batch count=%d inserts=%d reads=%d err=%v", count, counter.creditInserts, counter.personReads, err)
	}
	t.Logf("502 inputs, 501 distinct roles: %d credit inserts, %d person reads", counter.creditInserts, counter.personReads)
	cast = append(cast, map[string]any{"id": 0})
	raw, _ = json.Marshal(map[string]any{"id": 12, "credits": map[string]any{"cast": cast, "crew": []any{}}})
	if err := catalog.SaveResponse(ctx, req, raw, at.Add(time.Hour), ""); err == nil {
		t.Fatal("invalid tail accepted")
	}
	if err := r.DB.Model(&model.TMDbCredit{}).Count(&count).Error; err != nil || count != 501 {
		t.Fatalf("failed replacement lost roles: %d %v", count, err)
	}
	// 第二个分块插入失败时，第一个分块和删除旧关系也必须一起回滚。
	insertBatches := 0
	if err := r.DB.Callback().Create().Before("gorm:create").Register("tmdb_test_credit_error", func(db *gorm.DB) {
		if db.Statement.Table == "tmdb_credits" {
			insertBatches++
			if insertBatches == 2 {
				db.AddError(fmt.Errorf("second credit batch unavailable"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	cast = cast[:len(cast)-1]
	for _, v := range cast {
		v["character"] = "replacement " + v["character"].(string)
	}
	raw, _ = json.Marshal(map[string]any{"id": 12, "credits": map[string]any{"cast": cast, "crew": []any{}}})
	err := catalog.SaveResponse(ctx, req, raw, at.Add(time.Hour), "")
	_ = r.DB.Callback().Create().Remove("tmdb_test_credit_error")
	if err == nil || insertBatches != 2 {
		t.Fatalf("did not fail second batch: batches=%d err=%v", insertBatches, err)
	}
	if err := r.DB.Model(&model.TMDbCredit{}).Where("character LIKE 'replacement %'").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("first batch leaked: %d %v", count, err)
	}
	if err := r.DB.Model(&model.TMDbCredit{}).Count(&count).Error; err != nil || count != 501 {
		t.Fatalf("batch error lost previous roles: %d %v", count, err)
	}
}
