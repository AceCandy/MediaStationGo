package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoAlbumFavoriteIdentity(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	ctx := t.Context()
	lib := model.Library{Name: "合集收藏", Path: "/test/favorite", Type: model.LibraryTypeHongGuo}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	const albumID = "hg-group-99001"
	addSeason := func(source string, season int) *model.HongGuoWork {
		t.Helper()
		work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: source, Title: "合集收藏", EpisodeCount: 2, Snapshot: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		if err := repos.HongGuo.SaveAlbum(ctx, source, hongguo.Album{ID: "99001", Season: season}); err != nil {
			t.Fatal(err)
		}
		media := model.Media{LibraryID: lib.ID, Path: fmt.Sprintf("%s/%s.mkv", lib.Path, source), CatalogSource: "hongguo", LookupCatalogID: source, SeasonNum: 1, EpisodeNum: 1}
		if err := repos.Media.Upsert(ctx, &media); err != nil {
			t.Fatal(err)
		}
		return work
	}
	// 兼容未入库时保留的源收藏，补齐关系后提升为一条合集收藏。
	if err := repos.HongGuo.SetFavorite(ctx, "viewer", "91001", true); err != nil {
		t.Fatal(err)
	}
	orphan, err := repos.HongGuo.UserState(ctx, "viewer", "91001", 0)
	if err != nil || !orphan.Favorite {
		t.Fatalf("orphan source lost its stable identity: %+v %v", orphan, err)
	}
	first := addSeason("91001", 1)
	assertFavorite := func(user string, want bool) {
		t.Helper()
		item, err := e.Item(ctx, albumID, user)
		if err != nil || item == nil || item["UserData"].(map[string]any)["IsFavorite"] != want {
			t.Fatalf("favorite=%+v want=%v err=%v", item, want, err)
		}
		cards, total, err := repos.HongGuo.UserCards(ctx, user, "favourites", 1, 10, repository.MediaQueryFilter{})
		if err != nil || (total == 1) != want || int64(len(cards)) != total {
			t.Fatalf("favorite cards=%+v total=%d want=%v err=%v", cards, total, want, err)
		}
		for _, filters := range [][]string{{"IsFavorite"}, {"IsFavorite", "IsUnplayed"}} {
			page, err := e.Items(ctx, ItemsParams{UserID: user, Recursive: true, IncludeItemTypes: []string{"Series"},
				Filters: filters, SortBy: "DateLastContentAdded,SortName", SortOrder: "Descending", Limit: 30})
			if err != nil || (page["TotalRecordCount"] == int64(1)) != want || (len(page["Items"].([]map[string]any)) == 1) != want {
				t.Fatalf("global favorite page=%v want=%v err=%v", page, want, err)
			}
			if want {
				item := page["Items"].([]map[string]any)[0]
				if ids, ok := item["LibraryIds"].([]string); !ok || len(ids) != 1 || ids[0] != lib.ID {
					t.Fatalf("album library membership = %#v", item["LibraryIds"])
				}
			}
		}
	}
	assertFavorite("viewer", true)
	assertFavorite("other", false)
	second := addSeason("91002", 2)
	// 原收藏季文件消失，仅新季可见时仍是同一个收藏对象。
	if err := db.Where("lookup_catalog_id = ?", first.SourceID).Delete(&model.Media{}).Error; err != nil {
		t.Fatal(err)
	}
	assertFavorite("viewer", true)
	state, err := repos.HongGuo.UserState(ctx, "viewer", second.SourceID, 0)
	if err != nil || !state.Favorite {
		t.Fatalf("new season lost album favorite: %+v %v", state, err)
	}
	if err := repos.HongGuo.SetFavorite(ctx, "viewer", second.SourceID, false); err != nil {
		t.Fatal(err)
	}
	assertFavorite("viewer", false)
	for i := 0; i < 2; i++ {
		if err := e.SetFavorite(ctx, "viewer", albumID, true); err != nil {
			t.Fatal(err)
		}
	}
	var favorites []model.HongGuoFavorite
	if err := db.Where("user_id = ?", "viewer").Find(&favorites).Error; err != nil || len(favorites) != 1 || favorites[0].ItemID != albumID || !favorites[0].Favorite {
		t.Fatalf("favorite must be one album row: %+v %v", favorites, err)
	}
	var legacy int64
	if err := db.Model(&model.HongGuoUserState{}).Where("episode_number = 0").Count(&legacy).Error; err != nil || legacy != 0 {
		t.Fatalf("favorite wrote playback state: %d %v", legacy, err)
	}
	for _, visibility := range []MediaVisibility{{HiddenLibraryIDs: []string{lib.ID}}, {LibraryRestricted: true}} {
		e.visibilityCache = map[string]embyVisibilityCacheEntry{repos.ReadCacheKey() + "viewer": {visibility: visibility, expiresAt: time.Now().Add(time.Hour)}}
		if err := e.SetFavorite(ctx, "viewer", albumID, false); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("invisible album changed: %v", err)
		}
		page, err := e.Items(ctx, ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Series"},
			Filters: []string{"IsFavorite"}, SortBy: "DateLastContentAdded", Limit: 30})
		if err != nil || page["TotalRecordCount"] != int64(0) || len(page["Items"].([]map[string]any)) != 0 {
			t.Fatalf("global favorites exposed hidden album: %v %v", page, err)
		}
	}
	e.visibilityCache = nil
	assertFavorite("viewer", true)
	if err := e.SetFavorite(ctx, "viewer", "hg-season-"+second.ID, true); !errors.Is(err, repository.ErrFavoriteUnsupportedType) {
		t.Fatalf("season favorite accepted: %v", err)
	}
	if err := e.SetFavorite(ctx, "viewer", "hg-group-99999", true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown album accepted: %v", err)
	}
	if err := repos.HongGuo.SaveAlbum(ctx, second.SourceID, hongguo.Album{ID: "99002", Season: 1}); err != nil {
		t.Fatal(err)
	}
	state, err = repos.HongGuo.UserState(ctx, "viewer", second.SourceID, 0)
	if err != nil || state.Favorite {
		t.Fatalf("member moved the album favorite: %+v %v", state, err)
	}
	// 新成员加入已收藏的原合集，无需重新点收藏。
	addSeason("91003", 3)
	assertFavorite("viewer", true)
	// 已有合集关系的电影变为连载剧时，也必须提升原电影收藏。
	movie := hongguo.Work{SourceID: "94001", Title: "类型补全", Completed: true, EpisodeCount: 1, TotalEpisodes: 1, Snapshot: []byte(`{}`)}
	if _, err := repos.HongGuo.SaveDetail(ctx, movie); err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.SaveAlbum(ctx, movie.SourceID, hongguo.Album{ID: movie.SourceID, Season: 1}); err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.SetFavorite(ctx, "viewer", movie.SourceID, true); err != nil {
		t.Fatal(err)
	}
	movie.Completed, movie.EpisodeCount, movie.TotalEpisodes = false, 2, 0
	if _, err := repos.HongGuo.SaveDetail(ctx, movie); err != nil {
		t.Fatal(err)
	}
	state, err = repos.HongGuo.UserState(ctx, "viewer", movie.SourceID, 0)
	if err != nil || !state.Favorite {
		t.Fatalf("kind change lost source favorite: %+v %v", state, err)
	}
}
