package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestEmbyResumeSourcesGroupBeforeMerge(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	ctx := t.Context()
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	user := "resume-viewer"
	var libraries []string
	var newest []string
	for sourceIndex, source := range []string{"legacy", "hongguo", "nfo"} {
		library := model.Library{Base: model.Base{ID: source + "-library"}, Name: source, Path: "/test/" + source, Type: "movie"}
		create(&library)
		libraries = append(libraries, library.ID)
		seriesID, seasonID := source+"-series", source+"-season"
		if source == "legacy" {
			create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: "series", Title: "Series", Source: "test"})
			create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: "season", ParentID: &seriesID, SeasonNum: 1, Title: "Season", Source: "test"})
		} else if source == "nfo" {
			create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: seriesID}, LibraryID: library.ID, LocalKey: seriesID, Kind: "series", NFOFields: model.NFOFields{Title: "Series"}})
			create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: seasonID}, LibraryID: library.ID, LocalKey: seasonID, Kind: "season", ParentID: &seriesID, SeasonNum: 1, NFOFields: model.NFOFields{Title: "Season"}})
		}
		for i := 0; i < 7; i++ {
			id := fmt.Sprintf("%s-item-%d", source, i)
			kind, episode := "movie", 0
			var parent *string
			if i == 2 || i == 3 {
				kind, episode, parent = "episode", i-1, &seasonID
			}
			watched := base.Add(time.Duration(i*3+sourceIndex) * time.Minute)
			position := int64(1) // 混合查询保留 > 0，不套用独立历史的 20 秒阈值。
			stateUser, completed := user, i == 4
			if i == 5 {
				position = 0
			}
			if i == 6 {
				stateUser = "another-user"
			}
			file := model.Media{PermanentBase: model.PermanentBase{ID: id + "-file", CreatedAt: base}, LibraryID: library.ID, Path: "/test/" + id + ".mkv"}
			publicID := id
			switch source {
			case "legacy":
				create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: kind, ParentID: parent, EpisodeNum: episode, Title: "同名 " + id, Source: "test"})
				file.MetadataID = id
				create(&file)
				create(&model.PlaybackHistory{UserID: stateUser, MetadataID: id, MediaID: file.ID, PositionMs: position, Completed: completed, WatchedAt: watched})
			case "nfo":
				create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: id}, LibraryID: library.ID, LocalKey: id, Kind: kind, ParentID: parent, EpisodeNum: episode, NFOFields: model.NFOFields{Title: "同名 " + id}})
				file.CatalogSource = source
				create(&file)
				create(&model.NFOMediaBinding{MediaID: file.ID, ItemID: id, NFOFields: model.NFOFields{Title: "同名 " + id}})
				create(&model.NFOUserState{UserID: stateUser, ItemID: id, MediaID: file.ID, PositionMs: position, Completed: completed, WatchedAt: &watched})
				publicID = "nfo-" + id
			case "hongguo":
				work := model.HongGuoWork{PermanentBase: model.PermanentBase{ID: id}, SourceID: id, Kind: "movie", Title: "同名 " + id}
				var episodeID *string
				if episode > 0 {
					work.Kind, work.RelatedAlbumID, work.SeasonIndex = "series", "album", episode
					epID := id + "-episode"
					episodeID = &epID
				}
				create(&work)
				if episodeID != nil {
					create(&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: *episodeID}, WorkID: work.ID, Number: 1})
				}
				file.CatalogSource = source
				create(&file)
				create(&model.HongGuoMediaBinding{MediaID: file.ID, WorkID: work.ID, EpisodeID: episodeID})
				create(&model.HongGuoUserState{UserID: stateUser, SourceID: work.SourceID, EpisodeNumber: 1, MediaID: file.ID, PositionMs: position, Completed: completed, WatchedAt: &watched})
				publicID = "hg-work-" + id
				if episodeID != nil {
					publicID = "hg-episode-" + *episodeID
				}
			}
			if i == 3 {
				newest = append(newest, publicID)
				version := file
				version.ID, version.Path = file.ID+"-v2", file.Path+".v2"
				create(&version)
				switch source {
				case "nfo":
					create(&model.NFOMediaBinding{MediaID: version.ID, ItemID: id, NFOFields: model.NFOFields{Title: id}})
				case "hongguo":
					epID := id + "-episode"
					create(&model.HongGuoMediaBinding{MediaID: version.ID, WorkID: id, EpisodeID: &epID})
				}
			}
		}
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{user: {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	p := ItemsParams{UserID: user, Recursive: true, Filters: []string{"IsResumable"}, SortBy: "DatePlayed", SortOrder: "Descending", Limit: 2, Fields: []string{"MediaSources"}}
	assertPage := func(params ItemsParams, total int64, expected []string) {
		t.Helper()
		page, err := e.Items(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, item := range page["Items"].([]map[string]any) {
			ids = append(ids, item["Id"].(string))
		}
		if page["TotalRecordCount"] != total || !reflect.DeepEqual(ids, expected) || page["StartIndex"] != params.StartIndex {
			t.Fatalf("page=%v total=%v start=%v want=%v total=%d", ids, page["TotalRecordCount"], page["StartIndex"], expected, total)
		}
	}
	assertPage(p, 9, []string{newest[2], newest[1]})
	p.StartIndex = 2
	assertPage(p, 9, []string{newest[0], "nfo-nfo-item-1"})
	p.StartIndex = 6
	assertPage(p, 9, []string{"nfo-nfo-item-0", "hg-work-hongguo-item-0"})
	p.StartIndex = 9
	assertPage(p, 9, []string{})
	p.StartIndex = int(^uint(0) >> 1)
	assertPage(p, 9, []string{})
	p.StartIndex = 0
	resume, err := e.ResumeItems(ctx, user, 2)
	if err != nil || resume["TotalRecordCount"] != int64(9) {
		t.Fatalf("resume=%v err=%v", resume, err)
	}
	for _, item := range resume["Items"].([]map[string]any) {
		if len(item["MediaSources"].([]map[string]any)) != 2 {
			t.Fatalf("missing versions: %v", item)
		}
	}

	// 与原有全节点 SQL 对照所有排序，覆盖 SQL NULL/名称排序和归组代表项。
	for _, sortBy := range []string{"DatePlayed", "DateCreated", "SortName", "CommunityRating", "PremiereDate"} {
		for _, direction := range []string{"Ascending", "Descending"} {
			p.SortBy, p.SortOrder, p.StartIndex = sortBy, direction, 1
			expected := oldResumePage(t, e, p)
			assertPage(p, 9, expected)
		}
	}
	p.SortBy, p.SortOrder, p.StartIndex = "DatePlayed", "Descending", 0
	p.IncludeItemTypes = []string{"Movie"}
	assertPage(p, 6, []string{"nfo-nfo-item-1", "hg-work-hongguo-item-1"})
	p.IncludeItemTypes = nil
	p.SearchTerm = "同名"
	assertPage(p, 6, []string{"nfo-nfo-item-1", "hg-work-hongguo-item-1"})
	p.SearchTerm = ""
	create(&model.Favorite{UserID: user, MetadataID: "legacy-item-1", MediaID: "legacy-item-1-file"})
	create(&model.HongGuoUserState{UserID: user, SourceID: "hongguo-item-1", EpisodeNumber: 0, Favorite: true})
	if err := db.Model(&model.NFOUserState{}).Where("user_id = ? AND item_id = ?", user, "nfo-item-1").Update("favorite", true).Error; err != nil {
		t.Fatal(err)
	}
	p.Filters = []string{"IsResumable", "IsFavorite"}
	assertPage(p, 3, []string{"nfo-nfo-item-1", "hg-work-hongguo-item-1"})
	create(&model.Person{Base: model.Base{ID: "actor"}, Name: "Actor"})
	create(&model.MetadataCredit{MetadataID: "legacy-season", PersonID: "actor", Type: "Actor"})
	create(&model.HongGuoPerson{PermanentBase: model.PermanentBase{ID: "actor"}, SourceID: "actor", Name: "Actor"})
	create(&model.HongGuoCredit{WorkID: "hongguo-item-3", PersonID: "actor"})
	p.Filters, p.PersonIDs = []string{"IsResumable"}, []string{"actor", "hg-person-actor"}
	assertPage(p, 2, []string{newest[1], newest[0]})
	p.PersonIDs = nil
	p.Filters = []string{"IsResumable", "IsPlayed"}
	assertPage(p, 0, []string{})
	p.Filters = []string{"IsResumable"}
	p.UserID = "no-history"
	assertPage(p, 0, []string{})
	p.UserID = user
	for _, visibility := range []MediaVisibility{
		{IncludeNSFW: true, LibraryRestricted: true, AllowedLibraryIDs: []string{libraries[0]}},
		{HiddenLibraryIDs: []string{libraries[1], libraries[2]}},
	} {
		e.visibilityCache[user] = embyVisibilityCacheEntry{visibility: visibility, expiresAt: time.Now().Add(time.Hour)}
		assertPage(p, 3, []string{newest[0], "legacy-item-1"})
	}
	e.visibilityCache[user] = embyVisibilityCacheEntry{visibility: MediaVisibility{LibraryRestricted: true}, expiresAt: time.Now().Add(time.Hour)}
	assertPage(p, 0, []string{})
	e.visibilityCache[user] = embyVisibilityCacheEntry{visibility: MediaVisibility{}, expiresAt: time.Now().Add(time.Hour)}
	if err := db.Model(&model.NFOItem{}).Where("id = ?", "nfo-series").Update("nsfw", true).Error; err != nil {
		t.Fatal(err)
	}
	assertPage(p, 8, []string{newest[1], newest[0]})
	e.visibilityCache[user] = embyVisibilityCacheEntry{visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}
	for _, sql := range []string{
		`UPDATE playback_histories SET watched_at = NULL WHERE user_id = 'resume-viewer'`,
		`UPDATE nfo_user_states SET watched_at = NULL WHERE user_id = 'resume-viewer'`,
		`UPDATE hongguo_user_states SET watched_at = NULL WHERE user_id = 'resume-viewer'`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 缺失观看时间回退到文件入库时间；相同时间仍按 ID 确定代表集和全局顺序。
	assertPage(p, 9, []string{"nfo-nfo-item-3", "nfo-nfo-item-1"})
	assertPage(p, 9, oldResumePage(t, e, p))
	if err := db.Where("metadata_id = ? AND user_id = ?", "legacy-item-0", user).Delete(&model.PlaybackHistory{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id IN ?", []string{"nfo-item-0-file", "hongguo-item-0-file"}).Delete(&model.Media{}).Error; err != nil {
		t.Fatal(err)
	}
	assertPage(p, 6, oldResumePage(t, e, p))
}

// oldResumePage 保留优化前的目录展开作为结果对照，避免只验证新实现自身。
func oldResumePage(t *testing.T, e *EmbyService, p ItemsParams) []string {
	t.Helper()
	db := e.repo.DB.WithContext(t.Context())
	files := e.applyUserMediaVisibility(t.Context(), db.Model(&model.Media{}), p.UserID).Select("media.metadata_id, media.created_at")
	legacy := db.Table("(?) AS f", files).
		Joins("JOIN metadata_items leaf ON leaf.id = f.metadata_id").
		Joins("LEFT JOIN metadata_items parent ON parent.id = leaf.parent_id").
		Joins("LEFT JOIN metadata_items grandparent ON grandparent.id = parent.parent_id").
		Joins("JOIN metadata_items item ON item.id IN (leaf.id,parent.id,grandparent.id)").
		Joins("LEFT JOIN playback_histories h ON h.metadata_id = leaf.id AND h.user_id = ? AND h.deleted_at IS NULL", p.UserID).
		Select(`item.id, CASE WHEN item.kind = 'episode' THEN 'legacy:' || COALESCE(grandparent.id,item.id) ELSE 'legacy:' || item.id END AS resume_key,
 item.kind, item.title, MAX(f.created_at) AS created_at, COALESCE(MAX(h.watched_at),MAX(f.created_at)) AS played_at,
 BOOL_AND(COALESCE(h.completed,FALSE)) AS played, MAX(COALESCE(h.position_ms,0)) AS position_ms,
 item.rating, COALESCE(item.release_date,'') AS release_date, item.year`).Group("item.id, grandparent.id")
	hg := e.hongGuoNodes(t.Context(), p.UserID, "").Select("id,resume_key,LOWER(kind) AS kind,title,latest_at AS created_at,COALESCE(played_at,latest_at) AS played_at,played,position_ms,rating,'' AS release_date,0 AS year")
	nfo := e.nfoNodes(t.Context(), p.UserID, "").Select("id,resume_key,LOWER(kind) AS kind,title,latest_at AS created_at,COALESCE(played_at,latest_at) AS played_at,played,position_ms,rating,release_date,year")
	combined := db.Raw("? UNION ALL ? UNION ALL ?", legacy, hg, nfo)
	grouped := db.Table("(?) AS combined", combined).Where("NOT played AND position_ms > 0 AND kind IN ('movie','episode')").
		Select("DISTINCT ON (resume_key) *").Order("resume_key, played_at DESC, id DESC")
	var ids []string
	if err := db.Table("(?) AS grouped_resume", grouped).Order(globalItemsOrder(p)).Offset(p.StartIndex).Limit(p.Limit).Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestEmbyResumeCandidatesIgnoreUnwatchedCatalog(t *testing.T) {
	e := newTestEmbyService(t)
	db := e.repo.DB
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO metadata_items (id,kind,title,source) SELECT 'movie-' || n,'movie','Movie','test' FROM generate_series(1,20000) n`,
		`INSERT INTO media (id,metadata_id,path,created_at) SELECT 'file-' || n,'movie-' || n,'/test/' || n,now() FROM generate_series(1,20000) n`,
		`INSERT INTO playback_histories (id,user_id,metadata_id,media_id,position_ms,completed,watched_at) VALUES ('history','viewer','movie-1','file-1',1,false,now())`,
		`INSERT INTO playback_histories (id,user_id,metadata_id,media_id,position_ms,completed,watched_at) SELECT 'other-' || n,'other-user','movie-' || n,'file-' || n,1,false,now() FROM generate_series(1,20000) n`,
		`INSERT INTO nfo_items (id,library_id,local_key,kind,title) SELECT 'nfo-' || n,'nfo-library',n::text,'movie','Movie' FROM generate_series(1,20000) n`,
		`INSERT INTO media (id,catalog_source,path,created_at) SELECT 'nfo-file-' || n,'nfo','/test/nfo/' || n,now() FROM generate_series(1,20000) n`,
		`INSERT INTO nfo_media_bindings (media_id,item_id,fingerprint,title) SELECT 'nfo-file-' || n,'nfo-' || n,'','Movie' FROM generate_series(1,20000) n`,
		`INSERT INTO nfo_user_states (user_id,item_id,position_ms,completed,watched_at) VALUES ('viewer','nfo-1',1,false,now())`,
		`INSERT INTO nfo_user_states (user_id,item_id,position_ms,completed,watched_at) SELECT 'other-user','nfo-' || n,1,false,now() FROM generate_series(1,20000) n`,
		`INSERT INTO hongguo_works (id,source_id,kind,title,refreshed_at) SELECT 'hg-' || n,n::text,'movie','Movie',now() FROM generate_series(1,20000) n`,
		`INSERT INTO media (id,catalog_source,path,created_at) SELECT 'hg-file-' || n,'hongguo','/test/hg/' || n,now() FROM generate_series(1,20000) n`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id) SELECT 'hg-file-' || n,'hg-' || n FROM generate_series(1,20000) n`,
		`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,position_ms,completed,watched_at) VALUES ('viewer','1',1,1,false,now())`,
		`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,position_ms,completed,watched_at) SELECT 'other-user',n::text,1,1,false,now() FROM generate_series(1,20000) n`,
		`ANALYZE`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	p := ItemsParams{UserID: "viewer"}
	for name, source := range map[string]*gorm.DB{"legacy": e.legacyResumeCandidates(t.Context(), p), "nfo": e.nfoResumeCandidates(t.Context(), p), "hongguo": e.hongGuoResumeCandidates(t.Context(), p)} {
		query := source.Session(&gorm.Session{DryRun: true}).Find(&[]map[string]any{})
		var raw []byte
		if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.Statement.SQL.String(), query.Statement.Vars...).Row().Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plans []struct {
			Plan map[string]any
			Time float64 `json:"Execution Time"`
		}
		if err := json.Unmarshal(raw, &plans); err != nil {
			t.Fatal(err)
		}
		var inspect func(map[string]any)
		inspect = func(plan map[string]any) {
			relation, _ := plan["Relation Name"].(string)
			if relation == "media" || relation == "metadata_items" || relation == "nfo_items" || relation == "nfo_media_bindings" || relation == "hongguo_works" || relation == "hongguo_media_bindings" || relation == "playback_histories" || relation == "nfo_user_states" || relation == "hongguo_user_states" {
				rows, _ := plan["Actual Rows"].(float64)
				loops, _ := plan["Actual Loops"].(float64)
				removed, _ := plan["Rows Removed by Filter"].(float64)
				if (rows+removed)*loops > 10 || strings.Contains(fmt.Sprint(plan["Node Type"]), "Seq Scan") {
					t.Fatalf("%s resume scanned unrelated catalog: %s", name, raw)
				}
			}
			children, _ := plan["Plans"].([]any)
			for _, child := range children {
				inspect(child.(map[string]any))
			}
		}
		inspect(plans[0].Plan)
		t.Logf("%s: 20,000 movies, one resume candidate: %.3f ms", name, plans[0].Time)
	}
}
