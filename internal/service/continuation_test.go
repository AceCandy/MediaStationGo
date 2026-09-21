package service

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestContinuationCrossSeason(t *testing.T) {
	for _, source := range []string{"legacy", "nfo", "hongguo"} {
		t.Run(source, func(t *testing.T) {
			db := newServiceTestDB(t)
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE UNIQUE INDEX test_history_identity ON playback_histories(user_id,metadata_id) WHERE deleted_at IS NULL`).Error; err != nil {
				t.Fatal(err)
			}
			create := func(value any) {
				t.Helper()
				if err := db.Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			repos := repository.New(db)
			e := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
			p := NewPlaybackService(zap.NewNop(), repos)
			user := "viewer"
			e.visibilityCache = map[string]embyVisibilityCacheEntry{user: {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
			lib := model.Library{Base: model.Base{ID: "library"}, Name: "Test", Path: "/test", Type: "tv"}
			create(&lib)
			seriesID := "series"
			switch source {
			case "legacy":
				create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: "series", Title: "Show", Source: "test"})
			case "nfo":
				create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: seriesID}, LibraryID: lib.ID, LocalKey: seriesID, Kind: "series", NFOFields: model.NFOFields{Title: "Show"}})
			}
			items, seasons, files := []string{}, []string{}, []string{}
			for season := 1; season <= 2; season++ {
				seasonID := fmt.Sprintf("season-%d", season)
				switch source {
				case "legacy":
					create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: "season", ParentID: &seriesID, SeasonNum: season, Title: "Season", Source: "test"})
					seasons = append(seasons, seasonID)
				case "nfo":
					create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: seasonID}, LibraryID: lib.ID, LocalKey: seasonID, Kind: "season", ParentID: &seriesID, SeasonNum: season, NFOFields: model.NFOFields{Title: "Season"}})
					seasons = append(seasons, "nfo-"+seasonID)
				case "hongguo":
					create(&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: seasonID}, SourceID: fmt.Sprint(season), Kind: "series", Title: "Show", RelatedAlbumID: "100", SeasonIndex: season})
					seasons = append(seasons, "hg-season-"+seasonID)
				}
				for episode := 1; episode <= 2; episode++ {
					id := fmt.Sprintf("s%de%d", season, episode)
					file := model.Media{PermanentBase: model.PermanentBase{ID: id + "-file"}, LibraryID: lib.ID, Path: "/test/" + id + ".mkv", SeasonNum: season, EpisodeNum: episode}
					switch source {
					case "legacy":
						create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: "episode", ParentID: &seasonID, EpisodeNum: episode, Title: id, Source: "test"})
						file.MetadataID = id
						create(&file)
						items = append(items, id)
					case "nfo":
						create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: id}, LibraryID: lib.ID, LocalKey: id, Kind: "episode", ParentID: &seasonID, EpisodeNum: episode, NFOFields: model.NFOFields{Title: id}})
						file.CatalogSource = source
						create(&file)
						create(&model.NFOMediaBinding{MediaID: file.ID, ItemID: id, NFOFields: model.NFOFields{Title: id}})
						items = append(items, "nfo-"+id)
					case "hongguo":
						create(&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: id}, WorkID: seasonID, Number: episode})
						file.CatalogSource = source
						file.LookupCatalogID = fmt.Sprint(season)
						create(&file)
						create(&model.HongGuoMediaBinding{MediaID: file.ID, WorkID: seasonID, EpisodeID: &id})
						items = append(items, "hg-episode-"+id)
					}
					files = append(files, file.ID)
				}
			}
			assertNext := func(want string) {
				t.Helper()
				web, err := p.ContinueHistory(t.Context(), user, 10, MediaVisibility{IncludeNSFW: true})
				if err != nil {
					t.Fatal(err)
				}
				emby, err := e.NextUpItems(t.Context(), ItemsParams{UserID: user, Limit: 10})
				if err != nil {
					t.Fatal(err)
				}
				if want == "" {
					if len(web) != 0 || emby["TotalRecordCount"] != int64(0) {
						t.Fatalf("unexpected recommendations: web=%v emby=%v", web, emby)
					}
					return
				}
				if len(web) != 1 || web[0].MetadataID != want || !web[0].IsNext || web[0].PositionMs != 0 || web[0].Media == nil {
					t.Fatalf("web=%+v want %s", web, want)
				}
				got := emby["Items"].([]map[string]any)
				if len(got) != 1 || got[0]["Id"] != want || emby["TotalRecordCount"] != int64(1) {
					t.Fatalf("emby=%v want %s", emby, want)
				}
				page, err := e.NextUpItems(t.Context(), ItemsParams{UserID: user, StartIndex: 1, Limit: 1})
				if err != nil || page["TotalRecordCount"] != int64(1) || len(page["Items"].([]map[string]any)) != 0 {
					t.Fatalf("empty page lost total: %v %v", page, err)
				}
			}
			assertNext("")
			if err := e.MarkPlayed(t.Context(), user, seasons[0], true); err != nil {
				t.Fatal(err)
			}
			assertNext(items[2])
			// 已移除的错误长版本不能继续阻止季完成与跨季；三种来源保持同一规则。
			create(&model.MediaProbeMetadata{MediaID: files[0], DurationMS: 1_440_000})
			var oldVersion model.Media
			if err := db.First(&oldVersion, "id = ?", files[0]).Error; err != nil {
				t.Fatal(err)
			}
			oldVersion.ID, oldVersion.Path = "removed-version", "/test/removed.mkv"
			create(&oldVersion)
			switch source {
			case "nfo":
				create(&model.NFOMediaBinding{MediaID: oldVersion.ID, ItemID: "s1e1", NFOFields: model.NFOFields{Title: "s1e1"}})
			case "hongguo":
				id := "s1e1"
				create(&model.HongGuoMediaBinding{MediaID: oldVersion.ID, WorkID: "season-1", EpisodeID: &id})
			}
			create(&model.MediaProbeMetadata{MediaID: oldVersion.ID, DurationMS: 3_900_000})
			if err := e.MarkPlayed(t.Context(), user, items[0], false); err != nil {
				t.Fatal(err)
			}
			if err := p.RecordProgress(t.Context(), user, oldVersion.ID, "", 1_440_000, 3_900_000, MediaVisibility{IncludeNSFW: true}); err != nil {
				t.Fatal(err)
			}
			assertSeason := func(played bool) {
				t.Helper()
				item, err := e.Item(t.Context(), seasons[0], user)
				if err != nil || item == nil || item["UserData"].(map[string]any)["Played"] != played {
					t.Fatalf("season=%v err=%v", item, err)
				}
			}
			assertSeason(false)
			if err := db.Delete(&model.Media{}, "id = ?", oldVersion.ID).Error; err != nil {
				t.Fatal(err)
			}
			assertSeason(true)
			assertNext(items[2])
			if err := e.RecordProgress(t.Context(), user, items[0], oldVersion.ID, "", 120_000*10_000, 3_900_000*10_000); err != nil {
				t.Fatal(err)
			}
			assertNext(items[2])
			if err := p.RecordProgress(t.Context(), user, files[0], "", 120_000, 1_440_000, MediaVisibility{IncludeNSFW: true}); err != nil {
				t.Fatal(err)
			}
			assertSeason(true)
			if err := e.MarkPlayed(t.Context(), user, items[0], true); err != nil {
				t.Fatal(err)
			}
			assertNext(items[2])
			// 整季逐集写入可能让较早集拥有更晚时间；完成边界仍必须在季末。
			if err := e.MarkPlayed(t.Context(), user, items[0], true); err != nil {
				t.Fatal(err)
			}
			assertNext(items[2])
			if source == "hongguo" {
				if err := db.Model(&model.HongGuoWork{}).Where("id = ?", "season-2").Update("season_index", 1).Error; err != nil {
					t.Fatal(err)
				}
				assertNext(items[2])
			}
			// 后续首集不可见时，选择下一可见集；计数也必须基于可见候选。
			create(&model.Library{Base: model.Base{ID: "hidden"}, Name: "Hidden", Path: "/hidden", Type: "tv"})
			if err := db.Model(&model.Media{}).Where("id = ?", files[2]).Update("library_id", "hidden").Error; err != nil {
				t.Fatal(err)
			}
			visible, total, err := repos.History.Continuations(t.Context(), user, repository.MediaQueryFilter{IncludeNSFW: true, HiddenLibraryIDs: []string{"hidden"}}, true, "", 0, 10)
			if err != nil || total != 1 || len(visible) != 1 || visible[0].ItemID != items[3] {
				t.Fatalf("hidden successor: %v total=%d err=%v", visible, total, err)
			}
			if err := db.Model(&model.Media{}).Where("id = ?", files[2]).Update("library_id", lib.ID).Error; err != nil {
				t.Fatal(err)
			}
			if source != "hongguo" {
				table := "metadata_items"
				if source == "nfo" {
					table = "nfo_items"
				}
				if err := db.Table(table).Where("id = ?", "s2e1").Update("nsfw", true).Error; err != nil {
					t.Fatal(err)
				}
				rows, total, err := repos.History.Continuations(t.Context(), user, repository.MediaQueryFilter{}, true, "", 0, 10)
				if err != nil || total != 1 || len(rows) != 1 || rows[0].ItemID != items[3] {
					t.Fatalf("NSFW successor: %v total=%d err=%v", rows, total, err)
				}
				if err := db.Table(table).Where("id = ?", "s2e1").Update("nsfw", false).Error; err != nil {
					t.Fatal(err)
				}
			}
			// 多版本只产生一个候选，续播保留最后播放的具体文件。
			var version model.Media
			if err := db.First(&version, "id = ?", files[2]).Error; err != nil {
				t.Fatal(err)
			}
			version.ID, version.Path = version.ID+"-v2", version.Path+".v2"
			create(&version)
			switch source {
			case "nfo":
				create(&model.NFOMediaBinding{MediaID: version.ID, ItemID: "s2e1", NFOFields: model.NFOFields{Title: "s2e1"}})
			case "hongguo":
				id := "s2e1"
				create(&model.HongGuoMediaBinding{MediaID: version.ID, WorkID: "season-2", EpisodeID: &id})
			}
			assertNext(items[2])
			for _, visibility := range []MediaVisibility{{HiddenLibraryIDs: []string{lib.ID}}, {LibraryRestricted: true}} {
				rows, err := p.ContinueHistory(t.Context(), user, 10, visibility)
				if err != nil || len(rows) != 0 {
					t.Fatalf("visibility leaked: %+v %v", rows, err)
				}
			}
			other, err := p.ContinueHistory(t.Context(), "other", 10, MediaVisibility{IncludeNSFW: true})
			if err != nil || len(other) != 0 {
				t.Fatalf("user isolation: %v %v", other, err)
			}
			if err := p.RecordProgress(t.Context(), user, version.ID, "", 30000, 120000, MediaVisibility{IncludeNSFW: true}); err != nil {
				t.Fatal(err)
			}
			web, err := p.ContinueHistory(t.Context(), user, 10, MediaVisibility{IncludeNSFW: true})
			if err != nil || len(web) != 1 || web[0].IsNext || web[0].PositionMs != 30000 || web[0].MetadataID != items[2] || web[0].MediaID != version.ID {
				t.Fatalf("resume priority: %+v %v", web, err)
			}
			next, err := e.NextUpItems(t.Context(), ItemsParams{UserID: user, Limit: 10})
			if err != nil || next["TotalRecordCount"] != int64(0) {
				t.Fatalf("next up duplicated resume: %v %v", next, err)
			}
			resume, err := e.ResumeItems(t.Context(), user, 10)
			if err != nil || len(resume["Items"].([]map[string]any)) != 1 || resume["Items"].([]map[string]any)[0]["Id"] != items[2] {
				t.Fatalf("resume changed: %v %v", resume, err)
			}
			if err := e.MarkPlayed(t.Context(), user, items[2], true); err != nil {
				t.Fatal(err)
			}
			assertNext(items[3])
			before := []int64{}
			for _, table := range []string{"playback_histories", "nfo_user_states", "hongguo_user_states"} {
				var n int64
				if err := db.Table(table).Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				before = append(before, n)
			}
			assertNext(items[3])
			after := []int64{}
			for _, table := range []string{"playback_histories", "nfo_user_states", "hongguo_user_states"} {
				var n int64
				if err := db.Table(table).Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				after = append(after, n)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("recommendation wrote history: %v -> %v", before, after)
			}
			if err := e.MarkPlayed(t.Context(), user, seasons[1], true); err != nil {
				t.Fatal(err)
			}
			assertNext("")
		})
	}
}
