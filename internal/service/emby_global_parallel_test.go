package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

func TestEmbyGlobalPayloadsParallel(t *testing.T) {
	for _, api := range []string{"emby", "web"} {
		for _, mode := range []string{"parallel", "error", "single-connection"} {
			t.Run(api+"/"+mode, func(t *testing.T) {
				svc := newParallelSearchService(t)
				e := NewEmbyService(svc.cfg, svc.log, svc.repo)
				e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {
					visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour),
				}}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				failure := errors.New("fixture payload failure")
				started := make(chan string, 3)
				release := make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				var mu sync.Mutex
				seen := map[string]bool{}
				observe := func(tx *gorm.DB) {
					if tx.Error != nil || mode == "single-connection" {
						return
					}
					sql, source := tx.Statement.SQL.String(), ""
					switch {
					case api == "web" && strings.Contains(sql, "hongguo_works"):
						source = "hongguo"
					case api == "web" && strings.Contains(sql, "nfo_media_bindings"):
						source = "nfo"
					case api == "web" && strings.Contains(sql, "view_metadata_source"):
						source = "legacy"
					case tx.Statement.Table == "metadata_items" && strings.Contains(sql, "id, kind"):
						source = "legacy"
					case strings.Contains(sql, "AS nodes") && strings.Contains(sql, "nfo_media_bindings"):
						source = "nfo"
					case strings.HasPrefix(sql, "WITH page_works AS MATERIALIZED") || strings.Contains(sql, "AS nodes") && strings.Contains(sql, "hongguo_media_bindings"):
						source = "hongguo"
					}
					mu.Lock()
					first := source != "" && !seen[source]
					seen[source] = true
					mu.Unlock()
					if !first {
						return
					}
					started <- source
					// 已执行各来源的第一条 SQL；屏障证明查询重叠，而非仅启动 goroutine。
					if mode == "error" && source != "legacy" {
						<-tx.Statement.Context.Done()
						_ = tx.AddError(tx.Statement.Context.Err())
						return
					}
					select {
					case <-release:
					case <-tx.Statement.Context.Done():
					}
					if mode == "error" {
						_ = tx.AddError(failure)
					}
				}
				if err := e.repo.DB.Callback().Row().After("gorm:row").Register("test:payload-overlap", observe); err != nil {
					t.Fatal(err)
				}
				if err := e.repo.DB.Callback().Query().After("gorm:query").Register("test:payload-overlap", observe); err != nil {
					t.Fatal(err)
				}
				if mode == "single-connection" {
					db, _ := e.repo.DB.DB()
					db.SetMaxOpenConns(1)
				}
				ids := []string{"nfo-local", "ordinary", "hg-work-source", "ordinary"}
				var items []map[string]any
				var loadErr error
				done := make(chan struct{})
				go func() {
					defer close(done)
					if api == "web" {
						views, err := e.repo.MediaView.FindByLogicalMetadataIDs(ctx, ids, repository.MediaQueryFilter{})
						loadErr = err
						for _, view := range views {
							items = append(items, map[string]any{"Id": view.ID})
						}
					} else {
						items, loadErr = e.globalItemPayloads(ctx, ids, ItemsParams{UserID: "viewer", Fields: []string{"BasicSyncInfo"}})
					}
				}()
				t.Cleanup(func() { cancel(); unblock(); <-done })
				if mode != "single-connection" {
					for i := 0; i < 3; i++ {
						select {
						case <-started:
						case <-ctx.Done():
							t.Fatal("source SQL did not overlap")
						}
					}
				}
				unblock()
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("payload loading did not finish or cancel")
				}
				if mode == "error" {
					if !errors.Is(loadErr, failure) || items != nil {
						t.Fatalf("error=%v items=%v", loadErr, items)
					}
					return
				}
				var got []string
				for _, item := range items {
					got = append(got, item["Id"].(string))
				}
				want := ids
				if api == "web" {
					want = []string{"source-file", "local-file", "ordinary-file"}
				}
				if loadErr != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("ids=%v want=%v err=%v", got, want, loadErr)
				}
			})
		}
	}
}
