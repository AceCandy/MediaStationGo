package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type tmdbReuseCounter struct {
	logger.Interface
	personReads, personWrites, localReads, subjectUpdates int
}

func (c *tmdbReuseCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if strings.Contains(sql, `FROM "tmdb_person_profiles"`) {
		c.personReads++
	}
	if strings.HasPrefix(sql, `UPDATE "tmdb_person_profiles"`) || strings.HasPrefix(sql, `INSERT INTO "tmdb_person_profiles"`) {
		c.personWrites++
	}
	if strings.Contains(sql, `FROM metadata_items item`) || strings.Contains(sql, `FROM "metadata_items"`) || strings.Contains(sql, `FROM "metadata_identifiers"`) {
		c.localReads++
	}
	if strings.HasPrefix(sql, `UPDATE "tmdb_subjects"`) {
		c.subjectUpdates++
	}
}

func TestTMDbCatalogReusesPeopleWithinSeasonTransaction(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	personReq := TMDbSourceRequest{Kind: "person", TMDbID: 5, Component: "summary", Language: "zh-CN"}
	if err := r.TMDb.SaveResponse(ctx, personReq, []byte(`{"id":5,"name":"Actor","profile_path":"/a.jpg"}`), at, ""); err != nil {
		t.Fatal(err)
	}
	profile, err := r.TMDb.PersonIdentity(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	retry := at.Add(time.Hour)
	if err := r.DB.Model(profile).Updates(map[string]any{"image_status": "ready", "image_key": "local.jpg", "image_retry_at": retry}).Error; err != nil {
		t.Fatal(err)
	}
	rootReq := TMDbSourceRequest{Kind: "series", TMDbID: 20, Component: "detail", Language: "zh-CN"}
	if err := r.TMDb.SaveResponse(ctx, rootReq, []byte(`{"id":20,"name":"Series","genres":[{"id":1,"name":"Drama"}]}`), at, ""); err != nil {
		t.Fatal(err)
	}
	episodes := []map[string]any{}
	for i := 1; i <= 30; i++ {
		name := "Actor"
		if i == 30 {
			name = "Actor final"
		}
		episodes = append(episodes, map[string]any{"id": 100 + i, "season_number": 1, "episode_number": i,
			"guest_stars": []any{map[string]any{"id": 5, "name": name, "profile_path": "/a.jpg", "character": "Guest"}},
			"crew":        []any{map[string]any{"id": 5, "job": "Writer"}}})
	}
	raw, _ := json.Marshal(map[string]any{"id": 30, "season_number": 1, "episodes": episodes})
	counter := &tmdbReuseCounter{Interface: logger.Default}
	catalog := &TMDbCatalogRepository{db: r.DB.Session(&gorm.Session{Logger: counter})}
	seasonReq := TMDbSourceRequest{Kind: "season", TMDbID: 30, SeriesID: 20, Season: 1, Component: "detail", Language: "zh-CN"}
	if err := catalog.SaveResponse(ctx, seasonReq, raw, at, ""); err != nil {
		t.Fatal(err)
	}
	t.Logf("30 episodes, 60 roles: person reads=%d writes=%d parent updates=%d", counter.personReads, counter.personWrites, counter.subjectUpdates)
	if counter.personReads != 1 || counter.personWrites != 1 || counter.subjectUpdates != 0 {
		t.Fatalf("repeated person/parent work: %+v", counter)
	}
	profile, err = r.TMDb.PersonIdentity(ctx, 5)
	if err != nil || profile.Name != "Actor final" || profile.ImageStatus != "ready" || profile.ImageKey != "local.jpg" || profile.ImageRetryAt == nil || !profile.ImageRetryAt.Equal(retry) {
		t.Fatalf("cached merge lost fields or reset image: %+v, %v", profile, err)
	}
	var count int64
	if err := r.DB.Model(&model.TMDbCredit{}).Where("person_profile_id=?", profile.ID).Count(&count).Error; err != nil || count != 60 {
		t.Fatalf("roles lost: %d, %v", count, err)
	}
	// 下一事务必须重新读取，不能复用上一事务的资料或覆盖外部图片状态。
	if err := r.DB.Model(profile).Updates(map[string]any{"name": "Externally updated", "image_key": "external.jpg"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveResponse(ctx, personReq, []byte(`{"id":5,"profile_path":"/a.jpg"}`), at, ""); err != nil {
		t.Fatal(err)
	}
	profile, err = r.TMDb.PersonIdentity(ctx, 5)
	if err != nil || profile.Name != "Externally updated" || profile.ImageKey != "external.jpg" {
		t.Fatalf("cache leaked across transactions: %+v, %v", profile, err)
	}
	for _, change := range []struct {
		raw, path, status string
	}{
		{`{"id":5,"profile_path":"/b.jpg"}`, "/b.jpg", "pending"},
		{`{"id":5,"profile_path":null}`, "", "no_image"},
	} {
		if err := catalog.SaveResponse(ctx, personReq, []byte(change.raw), at, ""); err != nil {
			t.Fatal(err)
		}
		profile, err = r.TMDb.PersonIdentity(ctx, 5)
		if err != nil || profile.ProfilePath != change.path || profile.ImageStatus != change.status || profile.ImageRetryAt != nil {
			t.Fatalf("image transition lost: %+v, %v", profile, err)
		}
		if err := catalog.SaveResponse(ctx, personReq, []byte(`{"id":5,"profile_path":"/old.jpg"}`), at.Add(-time.Hour), ""); err != nil {
			t.Fatal(err)
		}
		profile, err = r.TMDb.PersonIdentity(ctx, 5)
		if err != nil || profile.ProfilePath != change.path || profile.ImageStatus != change.status {
			t.Fatalf("older response regressed image: %+v, %v", profile, err)
		}
	}
}

func TestTMDbCatalogLocalParentLookupIsOneRead(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "season", TMDbID: 30, SeriesID: 20, Season: 1, Component: "detail", Language: "zh-CN"}, []byte(`{"id":30,"season_number":1}`), at, ""); err != nil {
		t.Fatal(err)
	}
	series := model.MetadataItem{Kind: "series"}
	if err := r.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}
	if err := r.DB.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1}
	if err := r.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{PermanentBase: model.PermanentBase{ID: "root-first"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "20"},
		{PermanentBase: model.PermanentBase{ID: "root-last"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "99"},
		{PermanentBase: model.PermanentBase{ID: "episode-first"}, MetadataID: episode.ID, Provider: "tmdb", EntityKind: "episode", ExternalID: "40"},
		{PermanentBase: model.PermanentBase{ID: "episode-last"}, MetadataID: episode.ID, Provider: "tmdb", EntityKind: "episode", ExternalID: "41"},
	}
	if err := r.DB.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	counter := &tmdbReuseCounter{Interface: logger.Default}
	metadata := &MetadataRepository{db: r.DB.Session(&gorm.Session{Logger: counter})}
	raw := []byte(`{"id":40,"season_number":1,"episode_number":1,"name":"Episode"}`)
	if err := metadata.saveTMDbSourceData(ctx, episode.ID, raw, at, false); err != nil {
		t.Fatal(err)
	}
	t.Logf("episode local identity/parent reads=%d parent updates=%d", counter.localReads, counter.subjectUpdates)
	if counter.localReads != 1 || counter.subjectUpdates != 0 {
		t.Fatalf("duplicate parent queries or writes: %+v", counter)
	}
	if err := r.DB.Model(&identifiers[0]).Update("external_id", "21").Error; err != nil {
		t.Fatal(err)
	}
	if err := metadata.saveTMDbSourceData(ctx, episode.ID, raw, at, false); err == nil {
		t.Fatal("source parent bypassed changed local root identity")
	}
	if err := metadata.saveTMDbSourceData(ctx, episode.ID, []byte(`{"id":41}`), at, false); err == nil || !strings.Contains(err.Error(), "binding changed") {
		t.Fatalf("did not preserve first identifier selection: %v", err)
	}
}

func TestTMDbCatalogWriteCacheIsDiscardedOnRollback(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	req := TMDbSourceRequest{Kind: "movie", TMDbID: 12, Component: "detail", Language: "zh-CN"}
	bad := []byte(`{"id":12,"credits":{"cast":[{"id":5,"name":"Actor"},{"id":0}],"crew":[]}}`)
	if err := r.TMDb.SaveResponse(ctx, req, bad, at, ""); err == nil {
		t.Fatal("invalid person did not roll back")
	}
	for _, table := range []string{"tmdb_subjects", "tmdb_person_profiles", "tmdb_payloads", "tmdb_credits", "people", "person_identifiers"} {
		var count int64
		if err := r.DB.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rollback left %s rows: %d, %v", table, count, err)
		}
	}
	good := []byte(`{"id":12,"credits":{"cast":[{"id":5,"name":"Actor"}],"crew":[]}}`)
	if err := r.TMDb.SaveResponse(ctx, req, good, at, ""); err != nil {
		t.Fatal(err)
	}
	subject, err := r.TMDb.SubjectIdentity(ctx, "movie", 12)
	if err != nil {
		t.Fatal(err)
	}
	_, count, err := r.TMDb.Credits(ctx, subject.ID, false, 1, 100)
	if err != nil || count != 1 {
		t.Fatalf("retry reused uncommitted IDs: %d, %v", count, err)
	}
	// 相同时间的实际评分变化仍必须写回，不能被指针别名误判为无变化。
	if err := r.TMDb.SaveResponse(ctx, req, []byte(`{"id":12,"vote_average":7}`), at, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.TMDb.SaveResponse(ctx, req, []byte(`{"id":12,"vote_average":8}`), at, ""); err != nil {
		t.Fatal(err)
	}
	subject, err = r.TMDb.SubjectIdentity(ctx, "movie", 12)
	if err != nil || subject.Rating == nil || *subject.Rating != 8 {
		t.Fatalf("changed rating skipped: %+v, %v", subject, err)
	}
}
