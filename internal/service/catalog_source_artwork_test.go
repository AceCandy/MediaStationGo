package service

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestCatalogSourceArtworkRequiresCurrentKey(t *testing.T) {
	for _, tc := range []struct{ provider, category string }{
		{"hongguo", "artwork"},
		{"hongguo", "people"},
		{"huangguoai", "artwork"},
	} {
		t.Run(tc.provider+"/"+tc.category, func(t *testing.T) {
			db := newServiceTestDB(t, &model.HongGuoArtwork{}, &model.HuangGuoAIArtwork{})
			repos := repository.New(db)
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			stored, err := prepareStoredImage(encoded.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			key, err := catalogImageKey(tc.provider, tc.category, stored.StorageKey)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			path, err := managedImagePath(root, tc.category, key)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeStoredImage(path, encoded.Bytes(), ".test-*"); err != nil {
				t.Fatal(err)
			}
			images := NewImageProxy(&config.Config{}, zap.NewNop())
			var id string
			var row any
			var serve func(http.ResponseWriter, *http.Request) error
			if tc.provider == "hongguo" {
				sourceID := "71"
				artwork := &model.HongGuoArtwork{SourceID: &sourceID, SourceURL: "https://example.invalid/image.png", LocalKey: key}
				if tc.category == "people" {
					person := model.HongGuoPerson{SourceID: "person-71", Name: "演员"}
					if err := db.Create(&person).Error; err != nil {
						t.Fatal(err)
					}
					artwork.SourceID, artwork.PersonID = nil, &person.ID
				}
				if err := db.Create(artwork).Error; err != nil {
					t.Fatal(err)
				}
				row, id = artwork, artwork.ID
				s := NewHongGuoService(repos, nil, images, root)
				serve = func(w http.ResponseWriter, r *http.Request) error { return s.ServeArtwork(t.Context(), w, r, id) }
			} else {
				artwork := &model.HuangGuoAIArtwork{SourceID: "71", SourceURL: "https://example.invalid/image.png", LocalKey: key}
				if err := db.Create(artwork).Error; err != nil {
					t.Fatal(err)
				}
				row, id = artwork, artwork.ID
				s := NewHuangGuoAIService(repos, nil, images, root)
				serve = func(w http.ResponseWriter, r *http.Request) error { return s.ServeArtwork(t.Context(), w, r, id) }
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				rec := httptest.NewRecorder()
				if err := serve(rec, httptest.NewRequest(method, "/image", nil)); err != nil || rec.Code != http.StatusOK {
					t.Fatalf("current key %s: code=%d err=%v", method, rec.Code, err)
				}
				if method == http.MethodHead && rec.Body.Len() != 0 {
					t.Fatal("HEAD returned a body")
				}
			}
			if err := db.Model(row).Update("local_key", stored.StorageKey).Error; err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			if err := serve(rec, httptest.NewRequest(http.MethodGet, "/image", nil)); !errors.Is(err, ErrArtworkNotFound) || rec.Body.Len() != 0 {
				t.Fatalf("old key served despite existing catalog file: %v", err)
			}
			var repaired struct {
				LocalKey string
				Pending  bool
			}
			if err := db.Model(row).Select("local_key, next_attempt_at IS NOT NULL AS pending").Scan(&repaired).Error; err != nil || repaired.LocalKey != stored.StorageKey || !repaired.Pending {
				t.Fatalf("invalid key not scheduled for repair: %+v err=%v", repaired, err)
			}
		})
	}
}
