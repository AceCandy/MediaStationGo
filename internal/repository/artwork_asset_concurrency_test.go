package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestArtworkAssetConcurrentReuse(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.ArtworkAsset{}, &model.Library{}, &model.NFOItem{}, &model.NFOMediaBinding{}); err != nil {
		t.Fatal(err)
	}
	ensureSourcePresentationTestSchema(t, db)
	library := model.Library{Name: "Local", Path: "/local", Type: model.LibraryTypeNFOMovie}
	if err := db.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	var schemaName string
	if err := db.Raw("SELECT current_schema()").Scan(&schemaName).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"SaveAsset", "SaveSourceArtwork", "NFOIngest"}
	pool.SetMaxOpenConns(len(names))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	workers := make([]*ArtworkRepository, len(names))
	for i := range workers {
		conn, err := pool.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schemaName+`"`); err != nil {
			t.Fatal(err)
		}
		worker, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		workers[i] = &ArtworkRepository{db: worker}
	}
	// 固定独立连接的 search_path，避免并发测试串入 public schema。
	db = workers[0].db.WithContext(ctx)
	const rounds = 100
	for round := range rounds {
		items := make([]model.MetadataItem, len(names)-1)
		for i := range items {
			items[i] = model.MetadataItem{Kind: model.MetadataKindMovie, Title: names[i], Source: "tmdb"}
		}
		if err := db.Create(&items).Error; err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if err := testdb.SaveMetadataPresentationFixture(db, item); err != nil {
				t.Fatal(err)
			}
			var subject model.TMDbSubject
			if err := db.Table("tmdb_subjects s").Select("s.*").Joins("JOIN metadata_identifiers b ON b.provider='tmdb' AND b.entity_kind=s.kind AND b.external_id=s.tmdb_id::text").Where("b.metadata_id=?", item.ID).Take(&subject).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.TMDbArtwork{SubjectID: subject.ID, Type: "poster", SourcePath: "/new.jpg", Status: "pending"}).Error; err != nil {
				t.Fatal(err)
			}
		}
		nfoMedia := model.Media{LibraryID: library.ID, Path: fmt.Sprintf("/local/%d.mkv", round), CatalogSource: model.CatalogSourceNFO, ScrapeStatus: "matched"}
		save := func(i int, asset *model.ArtworkAsset) (*model.ArtworkAsset, error) {
			repo := workers[i]
			if i == len(items) {
				input := &NFOIngest{
					Items:   []model.NFOItem{{Kind: "movie", LocalKey: nfoMedia.Path, NFOFields: model.NFOFields{Title: "Local"}}},
					Binding: model.NFOMediaBinding{Fingerprint: asset.SHA256},
					Artwork: []NFOArtwork{{Type: model.ArtworkTypePoster, Asset: asset}},
				}
				if _, err := (&NFORepository{db: repo.db}).Ingest(ctx, &nfoMedia, input); err != nil {
					return nil, err
				}
				var saved model.ArtworkAsset
				err := repo.db.WithContext(ctx).First(&saved, "id = ?", input.Items[0].PosterAssetID).Error
				return &saved, err
			}
			if i == 0 {
				return repo.SaveAsset(ctx, asset)
			}
			return repo.SaveSourceArtwork(ctx, items[i].ID, model.ArtworkTypePoster, "https://example.test/new.jpg", asset)
		}

		hash := fmt.Sprintf("shared-%d", round)
		results := make([]struct {
			asset *model.ArtworkAsset
			err   error
		}, len(workers))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				asset := &model.ArtworkAsset{SHA256: hash, StorageKey: hash + ".jpg", MimeType: "image/jpeg", Width: 100}
				results[i].asset, results[i].err = save(i, asset)
			}()
		}
		close(start)
		wg.Wait()
		var stored model.ArtworkAsset
		if err := db.Where("sha256 = ?", hash).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		for i, result := range results {
			if result.err != nil || result.asset == nil || result.asset.ID != stored.ID {
				t.Fatalf("round %d %s: asset=%+v err=%v", round, names[i], result.asset, result.err)
			}
			// 同路径却不同哈希必须报错，不能错误关联已有图片或吞掉异常。
			invalid := &model.ArtworkAsset{SHA256: "different", StorageKey: stored.StorageKey, MimeType: "image/jpeg"}
			if _, err := save(i, invalid); err == nil {
				t.Fatalf("%s accepted a storage key belonging to another hash", names[i])
			}
			if i == len(items) {
				var binding model.NFOMediaBinding
				if err := db.First(&binding, "media_id = ?", nfoMedia.ID).Error; err != nil || binding.PosterAssetID != stored.ID {
					t.Fatalf("NFO binding=%+v err=%v", binding, err)
				}
				continue
			}
			if i == 0 {
				continue
			}
			selected, err := workers[i].FindSelection(ctx, items[i].ID, model.ArtworkTypePoster)
			if err != nil || selected == nil || selected.ID != stored.ID {
				t.Fatalf("%s selection=%+v err=%v", names[i], selected, err)
			}
		}
		// 重复引用保留首条资产属性，不覆盖图片记录。
		duplicate := &model.ArtworkAsset{SHA256: hash, StorageKey: stored.StorageKey, MimeType: "image/jpeg", Width: 999}
		saved, err := save(0, duplicate)
		if err != nil || saved.ID != stored.ID || saved.Width != 100 || !saved.UpdatedAt.Equal(stored.UpdatedAt) {
			t.Fatalf("existing asset changed: saved=%+v err=%v", saved, err)
		}
	}
	var count int64
	if err := db.Model(&model.ArtworkAsset{}).Count(&count).Error; err != nil || count != rounds {
		t.Fatalf("asset count=%d, want %d: %v", count, rounds, err)
	}
}
