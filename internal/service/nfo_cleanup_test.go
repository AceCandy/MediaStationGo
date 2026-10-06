package service

import (
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestNFODeletePathsClearEmptyWorks(t *testing.T) {
	for _, mode := range []string{"manual", "watcher", "batch", "root", "library"} {
		t.Run(mode, func(t *testing.T) {
			scanner, repos := newScannerTestEnv(t)
			root := t.TempDir()
			lib := model.Library{Name: "NFO", Path: root, Type: model.LibraryTypeNFOMovie}
			if err := repos.Library.Create(t.Context(), &lib); err != nil {
				t.Fatal(err)
			}
			m := model.Media{Path: filepath.Join(root, "removed.mkv"), LibraryID: lib.ID, LibraryRootID: "fixture-root", CatalogSource: model.CatalogSourceNFO, ScrapeStatus: "matched"}
			if _, err := repos.NFO.Ingest(t.Context(), &m, &repository.NFOIngest{
				Items:   []model.NFOItem{{Kind: "movie", LocalKey: "movie", NFOFields: model.NFOFields{Title: "Movie"}}},
				Binding: model.NFOMediaBinding{Fingerprint: "movie"},
			}); err != nil {
				t.Fatal(err)
			}
			media := NewMediaService(&config.Config{}, zap.NewNop(), repos)
			var err error
			switch mode {
			case "manual":
				err = media.Delete(t.Context(), m.ID)
			case "watcher":
				_, err = scanner.RemovePath(t.Context(), m.Path)
			case "batch":
				_, err = scanner.deleteMediaByIDs(t.Context(), []string{m.ID})
			case "root":
				err = repos.Media.DeleteByLibraryRoot(t.Context(), lib.ID, m.LibraryRootID)
			case "library":
				err = media.DeleteLibrary(t.Context(), lib.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range []any{&model.Media{}, &model.NFOItem{}, &model.NFOMediaBinding{}} {
				var count int64
				if err := repos.DB.Model(row).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("%T count=%d err=%v", row, count, err)
				}
			}
		})
	}
}
