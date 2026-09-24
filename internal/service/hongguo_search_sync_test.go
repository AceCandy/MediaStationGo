package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoSearchServiceMutationOwners(t *testing.T) {
	for _, name := range []string{"delete", "library", "root", "prune", "prune_partial", "watcher", "duplicate", "organizer_delete", "organizer_replace", "move", "library_only", "library_rollback"} {
		t.Run(name, func(t *testing.T) {
			db := newServiceTestDB(t)
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			repo := repository.New(db)
			ctx := t.Context()
			root := t.TempDir()
			lib := model.Library{Base: model.Base{ID: "library"}, Name: "source", Type: model.LibraryTypeHongGuo, Path: root}
			work := model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "12345678901", Kind: "series", Title: "Target"}
			file := model.Media{PermanentBase: model.PermanentBase{ID: "file"}, LibraryID: lib.ID, LibraryRootID: "root", CatalogSource: "hongguo", Path: filepath.Join(root, "missing.mkv")}
			for _, row := range []any{&lib, &work, &file, &model.HongGuoMediaBinding{MediaID: file.ID, WorkID: work.ID}} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var writes []string
			var document repository.MetadataSearchDocument
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					http.NotFound(w, r)
					return
				}
				if strings.Contains(r.URL.Path, "/_doc/") {
					mu.Lock()
					writes = append(writes, r.Method+" "+r.URL.Path)
					if r.Method == http.MethodPut {
						_ = json.NewDecoder(r.Body).Decode(&document)
					}
					mu.Unlock()
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer upstream.Close()
			repo.HongGuo.SetSearchBackend(repository.NewOpenSearchHongGuoBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL, Index: "test"}))
			if _, err := repo.HongGuo.BackfillSearchIndex(ctx, 10, 0); err != nil {
				t.Fatal(err)
			}
			media := NewMediaService(&config.Config{}, zap.NewNop(), repo)
			scanner := NewScannerService(&config.Config{}, zap.NewNop(), repo, nil, nil, nil)
			organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repo)
			var err error
			switch name {
			case "delete":
				err = media.Delete(ctx, file.ID)
			case "library":
				err = media.DeleteLibrary(ctx, lib.ID)
			case "root":
				err = repo.Media.DeleteByLibraryRoot(ctx, lib.ID, "root")
			case "prune":
				_, err = scanner.deleteMediaByIDs(ctx, []string{file.ID})
			case "prune_partial":
				calls := 0
				if err := db.Callback().Delete().Before("gorm:delete").Register("test:fail-second-delete", func(tx *gorm.DB) {
					if tx.Statement.Table == "media" {
						calls++
						if calls == 2 {
							tx.AddError(errors.New("second batch failed"))
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				ids := make([]string, 501)
				ids[0] = file.ID
				if removed, err := scanner.deleteMediaByIDs(ctx, ids); err == nil || removed != 1 {
					t.Fatalf("partial delete removed=%d err=%v", removed, err)
				}
			case "watcher":
				_, err = scanner.RemovePath(ctx, file.Path)
			case "duplicate":
				report := &Report{}
				NewDuplicateService(zap.NewNop(), repo, nil).removeMissingRows(ctx, []model.Media{file}, report)
				if report.MissingRemoved != 1 {
					t.Fatal(report)
				}
			case "organizer_delete":
				organizer.deleteMediaRowForPath(ctx, file.Path)
			case "organizer_replace":
				// 缺失源导致最终传输失败，但前面的数据库删除已经提交，仍须同步。
				if err := organizer.replaceVersions(ctx, filepath.Join(root, "absent-source"), []string{file.Path}, filepath.Join(root, "target"), TransferMove); err == nil {
					t.Fatal("expected missing source")
				}
			case "move":
				err = organizer.updateReclassifiedMediaRow(ctx, file.Path, file.Path, organizeExistingReclassifyRequest{TargetLibraryID: "other"})
			case "library_only":
				_, err = organizer.reclassifyScannedMediaLibraryOnly(ctx, file, lib, model.Library{Base: model.Base{ID: "other"}}, "test", "tv", false, &OrganizeResult{}, nil)
			case "library_rollback":
				if err := db.Callback().Delete().Before("gorm:delete").Register("test:rollback-library", func(tx *gorm.DB) {
					if tx.Statement.Table == "libraries" {
						tx.AddError(errors.New("library delete failed"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				if err := media.DeleteLibrary(ctx, lib.ID); err == nil {
					t.Fatal("expected rollback")
				}
				var count int64
				db.Model(&model.Media{}).Where("id = ?", file.ID).Count(&count)
				if count != 1 {
					t.Fatal("media deletion did not rollback")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if name == "library_rollback" {
				if len(writes) != 0 {
					t.Fatalf("rollback published %v", writes)
				}
				return
			}
			if name == "move" || name == "library_only" {
				if document.ID != "hg-work-work" || !reflect.DeepEqual(document.LibraryIDs, []string{"other"}) {
					t.Fatalf("document=%+v writes=%v", document, writes)
				}
			} else if !reflect.DeepEqual(writes, []string{"DELETE /test_hongguo/_doc/hg-work-work"}) {
				t.Fatalf("writes=%v", writes)
			}
		})
	}
}
