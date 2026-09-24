package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestHongGuoSearchDocumentsRequireFilesAndLibraries(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "12345678001", Title: "Indexed title", EpisodeCount: 2, Snapshot: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"hg-work-" + work.ID}
	docs, err := repos.HongGuo.searchDocuments(ctx, ids)
	if err != nil || len(docs) != 0 {
		t.Fatalf("fileless documents=%v err=%v", docs, err)
	}
	for _, id := range []string{"visible", "hidden"} {
		lib := model.Library{Base: model.Base{ID: id}, Name: id, Path: "/test/" + id, Type: model.LibraryTypeHongGuo}
		if err := repos.DB.Create(&lib).Error; err != nil {
			t.Fatal(err)
		}
		file := model.Media{LibraryID: id, Path: "/test/" + id + "/episode.mkv", CatalogSource: "hongguo", LookupCatalogID: work.SourceID, SeasonNum: 1, EpisodeNum: 1}
		if err := repos.Media.Upsert(ctx, &file); err != nil {
			t.Fatal(err)
		}
	}
	docs, err = repos.HongGuo.searchDocuments(ctx, ids)
	if err != nil || len(docs) != 1 || !reflect.DeepEqual(docs[0].LibraryIDs, []string{"hidden", "visible"}) {
		t.Fatalf("documents=%v err=%v", docs, err)
	}
}

type libraryFilteredHongGuoBackend struct{ recordingMetadataSearchBackend }

func (b *libraryFilteredHongGuoBackend) SearchMetadataIDs(_ context.Context, _ string, _, limit int, filter MetadataSearchFilter) ([]string, int64, error) {
	ids := []string{}
	for _, doc := range b.indexed {
		visible := !filter.LibraryRestricted
		for _, library := range doc.LibraryIDs {
			visible = visible || slices.Contains(filter.VisibleLibraryIDs, library)
		}
		if visible && (filter.CandidateIDs == nil || slices.Contains(filter.CandidateIDs, doc.ID)) {
			ids = append(ids, doc.ID)
		}
	}
	return ids[:min(limit, len(ids))], int64(len(ids)), nil
}

func TestHongGuoSearchPermissionBeforeLimit(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	for _, sql := range []string{
		`INSERT INTO hongguo_works (id,source_id,kind,title,refreshed_at) SELECT 'work-'||n,n::text,'series','Target',now() FROM generate_series(1,151) n`,
		`INSERT INTO media (id,library_id,catalog_source,path) SELECT 'file-'||n,CASE WHEN n=151 THEN 'visible' ELSE 'hidden' END,'hongguo','/test/'||n FROM generate_series(1,151) n`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id) SELECT 'file-'||n,'work-'||n FROM generate_series(1,151) n`,
		`INSERT INTO media (id,library_id,catalog_source,path) VALUES ('mixed','hidden','hongguo','/test/mixed')`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id) VALUES ('mixed','work-151')`,
	} {
		if err := repos.DB.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	backend := &libraryFilteredHongGuoBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	if _, err := repos.HongGuo.BackfillSearchIndex(t.Context(), 200, 0); err != nil {
		t.Fatal(err)
	}
	// 将唯一可见作品放在 150 个隐藏竞争项之后，缺失索引过滤会使它被上限截掉。
	slices.SortFunc(backend.indexed, func(a, b MetadataSearchDocument) int {
		if a.ID == b.ID {
			return 0
		}
		if a.ID == "hg-work-work-151" {
			return 1
		}
		if b.ID == "hg-work-work-151" {
			return -1
		}
		return 0
	})
	for _, tc := range []struct {
		name            string
		allowed, hidden []string
		restricted      bool
		want            int
	}{
		{"allowed", []string{"visible"}, nil, false, 1},
		{"raw_restricted", []string{"visible"}, nil, true, 1},
		{"hidden_only", nil, []string{"hidden"}, false, 1},
		{"mixed", []string{"visible", "hidden"}, []string{"hidden"}, false, 1},
		{"empty_intersection", []string{"hidden"}, []string{"hidden"}, false, 0},
		{"locked", nil, nil, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := MetadataSearchFilter{Kinds: []string{"series"}, LibraryRestricted: tc.restricted, MediaQueryFilter: MediaQueryFilter{AllowedLibraryIDs: tc.allowed, HiddenLibraryIDs: tc.hidden}}
			rows, err := repos.HongGuo.SearchCandidates(t.Context(), "Target", filter)
			if err != nil || len(rows) != tc.want || (tc.want == 1 && rows[0].ID != "hg-work-work-151") {
				t.Fatalf("rows=%v err=%v", rows, err)
			}
		})
	}
}

func TestHongGuoSearchMutationAndRecovery(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	for _, id := range []string{"a", "b"} {
		if err := repos.DB.Create(&model.Library{Base: model.Base{ID: id}, Name: id, Path: "/test/" + id, Type: model.LibraryTypeHongGuo}).Error; err != nil {
			t.Fatal(err)
		}
	}
	works := []*model.HongGuoWork{}
	for i := 0; i < 2; i++ {
		work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: fmt.Sprint(12345678910 + i), Title: "Target", EpisodeCount: 2, Snapshot: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		works = append(works, work)
	}
	backend := &hongGuoSearchTestBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	file := model.Media{LibraryID: "a", Path: "/test/file", CatalogSource: "hongguo", LookupCatalogID: works[0].SourceID, SeasonNum: 1, EpisodeNum: 1}
	upsert := func() {
		t.Helper()
		if err := repos.Media.Upsert(ctx, &file); err != nil {
			t.Fatal(err)
		}
	}
	upsert()
	filter := MetadataSearchFilter{Kinds: []string{"series"}}
	rows, err := repos.HongGuo.SearchCandidates(ctx, "Target", filter)
	if err != nil || len(rows) != 1 || len(backend.filters) != 0 {
		t.Fatalf("startup must fallback rows=%v err=%v", rows, err)
	}
	if _, err := repos.HongGuo.BackfillSearchIndex(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
	backend.ids = []string{"hg-work-" + works[0].ID}
	file.LibraryID = "b"
	upsert()
	// 扫描 Upsert 不迁移已归属媒体库；索引必须反映真实库，而非传入的库。
	if got := backend.upserts[len(backend.upserts)-1]; !reflect.DeepEqual(got.LibraryIDs, []string{"a"}) {
		t.Fatal(got)
	}
	file.LookupCatalogID = works[1].SourceID
	upsert()
	if !slices.Contains(backend.deletes, "hg-work-"+works[0].ID) || backend.upserts[len(backend.upserts)-1].ID != "hg-work-"+works[1].ID {
		t.Fatal("rebind missed old/new identity")
	}
	// 绑定失败回滚：不能发布文档，也不能登记已提交 dirty 身份。
	if err := repos.Setting.Set(ctx, "hongguo.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	before := len(backend.upserts) + len(backend.deletes)
	repos.HongGuo.searchRebuild, repos.HongGuo.searchDirty = true, map[string]struct{}{}
	file.LibraryID = "a"
	if err := repos.Media.Upsert(ctx, &file); err == nil {
		t.Fatal("expected rollback")
	}
	if len(backend.upserts)+len(backend.deletes) != before || len(repos.HongGuo.searchDirty) != 0 {
		t.Fatal("rollback published index changes")
	}
	repos.HongGuo.finishSearchRebuild()
	if err := repos.Setting.Set(ctx, "hongguo.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	file.LibraryID = "b"
	file.EpisodeNum = 99
	upsert()
	if !slices.Contains(backend.deletes, "hg-work-"+works[1].ID) {
		t.Fatal("pending file retained document")
	}
	file.EpisodeNum = 1
	upsert()
	backend.writeErr = errors.New("index unavailable")
	file.LibraryID = "a"
	upsert()
	if !repos.HongGuo.searchFailed.Load() || repos.Media.hongGuo != repos.HongGuo {
		t.Fatal("writer and reader do not share failure state")
	}
	backend.writeErr = nil
	if _, err := repos.HongGuo.BackfillSearchIndex(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
	if repos.HongGuo.searchFailed.Load() {
		t.Fatal("rebuild did not recover")
	}
	deletesBefore := len(backend.deletes)
	if err := repos.Media.DeleteByLibrary(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if len(backend.deletes) != deletesBefore+1 || backend.deletes[len(backend.deletes)-1] != "hg-work-"+works[1].ID {
		t.Fatal("last file deletion missed")
	}
	// 即使复用已就绪的 backend，新进程仓库仍必须先重建。
	restarted := New(repos.DB)
	restarted.HongGuo.SetSearchBackend(backend)
	if !restarted.HongGuo.searchFailed.Load() {
		t.Fatal("restart trusted persisted alias")
	}
}

func TestHongGuoSearchCaptureFailureBlocksRebuild(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	backend := &hongGuoSearchTestBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	if err := repos.DB.Create(&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "empty"}, SourceID: "12345678901", Kind: "series", Title: "Target"}).Error; err != nil {
		t.Fatal(err)
	}
	backend.onIndex = func() {
		backend.onIndex = nil
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		refresh := repos.HongGuo.PrepareMediaSearchRefresh(repos.DB.WithContext(ctx).Model(&model.Media{}))
		refresh()
	}
	if _, err := repos.HongGuo.BackfillSearchIndex(t.Context(), 1, 0); err == nil || backend.activated || !backend.discarded || !repos.HongGuo.searchFailed.Load() {
		t.Fatalf("incomplete rebuild activated: err=%v", err)
	}
}

func TestHongGuoSearchCommittedCancellation(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	for _, value := range []any{
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "12345678901", Kind: "series", Title: "Target"},
		&model.Media{PermanentBase: model.PermanentBase{ID: "file"}, LibraryID: "visible", CatalogSource: "hongguo", Path: "/test/file"},
		&model.HongGuoMediaBinding{MediaID: "file", WorkID: "work"},
	} {
		if err := repos.DB.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	backend := &hongGuoSearchTestBackend{ids: []string{"hg-work-work"}}
	repos.HongGuo.SetSearchBackend(backend)
	if _, err := repos.HongGuo.BackfillSearchIndex(t.Context(), 10, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	refresh := repos.HongGuo.PrepareMediaSearchRefresh(repos.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", "file"))
	if err := repos.DB.Where("id = ?", "file").Delete(&model.Media{}).Error; err != nil {
		t.Fatal(err)
	}
	cancel()
	refresh()
	rows, err := repos.HongGuo.SearchCandidates(t.Context(), "Target", MetadataSearchFilter{Kinds: []string{"series"}})
	if err != nil || len(rows) != 0 || len(backend.filters) != 0 || !repos.HongGuo.searchFailed.Load() {
		t.Fatalf("cancelled publication trusted index rows=%v err=%v", rows, err)
	}
	if _, err := repos.HongGuo.BackfillSearchIndex(t.Context(), 10, 0); err != nil {
		t.Fatal(err)
	}
	if repos.HongGuo.searchFailed.Load() {
		t.Fatal("rebuild did not recover cancelled publication")
	}
}

func TestHongGuoSearchFilelessBatchAndMembershipDirtyReplay(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	for _, value := range []any{
		&model.Library{Base: model.Base{ID: "library"}, Name: "source", Type: model.LibraryTypeHongGuo, Path: "/test"},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "a"}, SourceID: "12345678901", Kind: "movie", Title: "Target"},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "z"}, SourceID: "12345678902", Kind: "movie", Title: "Target"},
		&model.Media{PermanentBase: model.PermanentBase{ID: "file"}, LibraryID: "library", CatalogSource: "hongguo", Path: "/test/file"},
		&model.HongGuoMediaBinding{MediaID: "file", WorkID: "z"},
	} {
		if err := repos.DB.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	backend := &hongGuoSearchTestBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	backend.onIndex = func() {
		backend.onIndex = nil
		file := model.Media{LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "12345678901", Path: "/test/new-file"}
		if err := repos.Media.Upsert(t.Context(), &file); err != nil {
			t.Fatal(err)
		}
	}
	if total, err := repos.HongGuo.BackfillSearchIndex(t.Context(), 1, 0); err != nil || total != 1 {
		t.Fatalf("fileless batch stopped rebuild total=%d err=%v", total, err)
	}
	var ids []string
	for _, doc := range backend.indexed {
		ids = append(ids, doc.ID)
	}
	if !slices.Contains(ids, "hg-work-a") || !slices.Contains(ids, "hg-work-z") {
		t.Fatalf("membership dirty replay lost: %v", ids)
	}
}

func TestHongGuoSearchRebindPartialCommit(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "12345678901", Title: "Target", EpisodeCount: 1, Snapshot: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		file := model.Media{PermanentBase: model.PermanentBase{ID: id}, LibraryID: "library", Path: "/test/" + id, CatalogSource: "hongguo", LookupCatalogID: work.SourceID, SeasonNum: 1, EpisodeNum: 1}
		if err := repos.DB.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
	}
	backend := &hongGuoSearchTestBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	if _, err := repos.HongGuo.BackfillSearchIndex(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Callback().Create().Before("gorm:create").Register("test:fail-second-binding", func(tx *gorm.DB) {
		if b, ok := tx.Statement.Dest.(*model.HongGuoMediaBinding); ok && b.MediaID == "b" {
			tx.AddError(errors.New("second binding failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.RebindWork(ctx, work.SourceID); err == nil {
		t.Fatal("expected partial failure")
	}
	var count int64
	repos.DB.Model(&model.HongGuoMediaBinding{}).Count(&count)
	if count != 1 || len(backend.upserts) != 1 || backend.upserts[0].ID != "hg-work-"+work.ID {
		t.Fatalf("committed partial batch lost: bindings=%d upserts=%v", count, backend.upserts)
	}
}

func TestHongGuoSearchUsesLibraryScopeAndEmptyShortCircuit(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "12345678002", Title: "Indexed title", EpisodeCount: 2, Snapshot: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	file := model.Media{LibraryID: "visible", Path: "/test/visible/file", CatalogSource: "hongguo"}
	if err := repos.DB.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.HongGuoMediaBinding{MediaID: file.ID, WorkID: work.ID}).Error; err != nil {
		t.Fatal(err)
	}
	backend := &hongGuoSearchTestBackend{ids: []string{"hg-work-" + work.ID}}
	repos.HongGuo.SetSearchBackend(backend)
	if _, err := repos.HongGuo.BackfillSearchIndex(ctx, 10, 0); err != nil {
		t.Fatal(err)
	}
	filter := MetadataSearchFilter{Kinds: []string{"series"}, MediaQueryFilter: MediaQueryFilter{AllowedLibraryIDs: []string{"visible", "hidden"}, HiddenLibraryIDs: []string{"hidden"}}}
	rows, err := repos.HongGuo.SearchCandidates(ctx, "Indexed", filter)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	got := backend.filters[len(backend.filters)-1]
	if got.CandidateIDs != nil || !got.LibraryRestricted || !reflect.DeepEqual(got.VisibleLibraryIDs, []string{"visible"}) {
		t.Fatalf("index filter=%+v", got)
	}
	backend.ids = nil
	// 空命中不应再访问作品/绑定表；删除表只影响此隔离测试 schema。
	if err := repos.DB.Exec("DROP TABLE hongguo_media_bindings").Error; err != nil {
		t.Fatal(err)
	}
	rows, err = repos.HongGuo.SearchCandidates(ctx, "Indexed", filter)
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty indexed result=%v err=%v", rows, err)
	}
}
