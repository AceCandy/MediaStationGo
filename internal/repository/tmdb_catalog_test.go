package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func tmdbTestRepo(t *testing.T) *Container {
	t.Helper()
	db, e := testdb.OpenPostgres(t, &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(model.AllModels()...); e != nil {
		t.Fatal(e)
	}
	return New(db)
}
func TestTMDbCenterIsolationAndScopes(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	req := TMDbSourceRequest{Kind: "movie", TMDbID: 12, Language: "zh-CN", Component: "detail", Origin: "api"}
	raw := []byte(`{"id":12,"title":"来源标题","vote_average":7,"credits":{"cast":[{"id":5,"name":"演员","credit_id":"a","character":"甲"},{"id":5,"name":"演员","credit_id":"a","character":"乙"}],"crew":[]},"external_ids":{"imdb_id":"tt123"}}`)
	if e := r.TMDb.SaveResponse(ctx, req, raw, now, ""); e != nil {
		t.Fatal(e)
	}
	subject, e := r.TMDb.SubjectIdentity(ctx, "movie", 12)
	if e != nil {
		t.Fatal(e)
	}
	credits, n, e := r.TMDb.Credits(ctx, subject.ID, false, 1, 100)
	if e != nil || n != 2 {
		t.Fatalf("credits %d %v", n, e)
	}
	personID := credits[0].PersonProfile.PersonID
	if e = r.DB.Model(&model.Person{}).Where("id=?", personID).Updates(map[string]any{"name": "手工名字", "overview": "手工简介"}).Error; e != nil {
		t.Fatal(e)
	}
	req.Component = "base"
	if e = r.TMDb.SaveResponse(ctx, req, []byte(`{"id":12,"title":"新版来源","vote_average":8}`), now.Add(time.Hour), ""); e != nil {
		t.Fatal(e)
	}
	subject, e = r.TMDb.SubjectIdentity(ctx, "movie", 12)
	if e != nil || subject.Title != "新版来源" || subject.Quality != 3 || subject.Rating == nil || *subject.Rating != 8 {
		t.Fatalf("base projection %#v %v", subject, e)
	}
	_, n, _ = r.TMDb.Credits(ctx, subject.ID, false, 1, 100)
	if n != 2 {
		t.Fatal("base cleared credits")
	}
	req.Component = "detail"
	if e = r.TMDb.SaveResponse(ctx, req, []byte(`{"id":12,"credits":null}`), now.Add(2*time.Hour), ""); e != nil {
		t.Fatal(e)
	}
	_, n, _ = r.TMDb.Credits(ctx, subject.ID, false, 1, 100)
	if n != 2 {
		t.Fatal("null cleared credits")
	}
	p, _ := r.TMDb.Person(ctx, personID)
	f := TMDbSourceRequest{Kind: "person", TMDbID: 5, Language: "zh-CN", Component: "filmography"}
	film := []byte(`{"id":5,"cast":[{"id":12,"media_type":"movie","title":"summary","credit_id":"z","character":"丙"}],"crew":[]}`)
	if e = r.TMDb.SaveResponse(ctx, f, film, now.Add(3*time.Hour), ""); e != nil {
		t.Fatal(e)
	}
	_, n, _ = r.TMDb.Credits(ctx, p.ID, true, 1, 100)
	if n != 3 {
		t.Fatalf("filmography destroyed subject roles %d", n)
	}
	if e = r.TMDb.SaveResponse(ctx, f, []byte(`{"id":5,"cast":[]}`), now.Add(4*time.Hour), ""); e == nil {
		t.Fatal("partial filmography accepted")
	}
	_, n, _ = r.TMDb.Credits(ctx, p.ID, true, 1, 100)
	if n != 3 {
		t.Fatal("partial replaced old roles")
	}
	var local model.Person
	r.DB.First(&local, "id=?", personID)
	if local.Name != "手工名字" || local.Overview != "手工简介" {
		t.Fatal("source changed person display")
	}
	var count int64
	r.DB.Model(&model.MetadataItem{}).Count(&count)
	if count != 0 {
		t.Fatal("source created local metadata")
	}
}
func TestTMDbCenterSeasonAndLegacy(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	req := TMDbSourceRequest{Kind: "season", SeriesID: 20, Season: 0, Component: "detail", Language: "zh-CN"}
	raw := []byte(`{"id":30,"season_number":0,"name":"特别篇","episodes":[{"id":40,"episode_number":1,"season_number":0,"name":"一集","guest_stars":[{"id":8,"name":"客串","character":"角色"}]}]}`)
	if e := r.TMDb.SaveResponse(ctx, req, raw, now, ""); e != nil {
		t.Fatal(e)
	}
	season, e := r.TMDb.SubjectIdentity(ctx, "season", 30)
	if e != nil {
		t.Fatal(e)
	}
	ep, e := r.TMDb.SubjectIdentity(ctx, "episode", 40)
	if e != nil || ep.ParentID == nil || *ep.ParentID != season.ID {
		t.Fatal("parent missing", e)
	}
	req = TMDbSourceRequest{Kind: "episode", SeriesID: 20, Season: 0, Episode: 1, Component: "detail", Language: "zh-CN"}
	full := []byte(`{"id":40,"episode_number":1,"season_number":0,"name":"单集","external_ids":{"imdb_id":"tt999"}}`)
	if e = r.TMDb.SaveResponse(ctx, req, full, now.Add(time.Hour), ""); e != nil {
		t.Fatal(e)
	}
	req = TMDbSourceRequest{Kind: "season", SeriesID: 20, Season: 0, Component: "detail", Language: "zh-CN"}
	if e = r.TMDb.SaveResponse(ctx, req, raw, now.Add(2*time.Hour), ""); e != nil {
		t.Fatal(e)
	}
	payloads, e := r.TMDb.Payloads(ctx, ep.ID, false)
	if e != nil || len(payloads) != 2 {
		t.Fatal("lost full episode", e, len(payloads))
	}
	for _, p := range payloads {
		if p.Component == "detail" {
			var o map[string]any
			_ = json.Unmarshal([]byte(p.Body), &o)
			if o["external_ids"] == nil {
				t.Fatal("extended erased")
			}
		}
	}
	movie := model.MetadataItem{Kind: "movie", Title: "手改"}
	if e = r.DB.Create(&movie).Error; e != nil {
		t.Fatal(e)
	}
	ident := model.MetadataIdentifier{MetadataID: movie.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "90"}
	if e = r.DB.Create(&ident).Error; e != nil {
		t.Fatal(e)
	}
	if e = r.Metadata.UpsertProviderSnapshot(ctx, movie.ID, "tmdb", []byte(`{"id":90,"title":"来源"}`), now); e != nil {
		t.Fatal(e)
	}
	snap, e := r.Metadata.FindProviderSnapshot(ctx, movie.ID, "tmdb")
	if e != nil || snap == nil {
		t.Fatal("compatibility missing", e)
	}
	var count int64
	r.DB.Model(&model.MetadataProviderSnapshot{}).Where("provider='tmdb'").Count(&count)
	if count != 0 {
		t.Fatal("old snapshot writer active")
	}
	if e = r.DB.Delete(&ident).Error; e != nil {
		t.Fatal(e)
	}
	if e = r.DB.Delete(&movie).Error; e != nil {
		t.Fatal(e)
	}
	if _, e = r.TMDb.SubjectIdentity(ctx, "movie", 90); e != nil {
		t.Fatal("local delete removed source")
	}
}

func TestTMDbCenterApplyingSeasonEpisodePreservesSourceTime(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	raw := []byte(`{"id":40,"season_number":0,"episode_number":1,"name":"一集"}`)
	seasonRaw := []byte(`{"id":30,"season_number":0,"episodes":[` + string(raw) + `]}`)
	if err := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "season", SeriesID: 20, Season: 0, Component: "detail", Language: "zh-CN", Origin: "api"}, seasonRaw, at, ""); err != nil {
		t.Fatal(err)
	}
	series := model.MetadataItem{Kind: "series"}
	if err := r.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 0}
	if err := r.DB.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1}
	if err := r.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	ids := []model.MetadataIdentifier{
		{MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "20"},
		{MetadataID: episode.ID, Provider: "tmdb", EntityKind: "episode", ExternalID: "40"},
	}
	if err := r.DB.Create(&ids).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Metadata.UpsertProviderSnapshot(ctx, episode.ID, "tmdb", raw, time.Now()); err != nil {
		t.Fatal(err)
	}
	source, err := r.TMDb.SubjectIdentity(ctx, "episode", 40)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := r.TMDb.Payloads(ctx, source.ID, false)
	if err != nil || len(payloads) != 1 || !payloads[0].FetchedAt.Equal(at) || payloads[0].Origin != "season" {
		t.Fatalf("local apply advanced source time: %+v, %v", payloads, err)
	}
	if err := r.DB.Model(&ids[0]).Update("external_id", "21").Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Metadata.UpsertProviderSnapshot(ctx, episode.ID, "tmdb", raw, time.Now()); err == nil {
		t.Fatal("cached response bypassed changed local parent identity")
	}
}

func TestTMDbCenterPersonWorksGroupBeforePaging(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := context.Background()
	at := time.Now()
	if e := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "person", TMDbID: 5, Component: "detail", Language: "zh-CN"}, []byte(`{"id":5,"name":"人物"}`), at, ""); e != nil {
		t.Fatal(e)
	}
	req := TMDbSourceRequest{Kind: "person", TMDbID: 5, Component: "filmography", Language: "zh-CN"}
	raw := []byte(`{"id":5,"cast":[{"id":1,"media_type":"movie","release_date":"2025-01-01","character":"甲"},{"id":1,"media_type":"movie","release_date":"2025-01-01","character":"乙"},{"id":2,"media_type":"tv","first_air_date":"2024-01-01","character":"丙"}],"crew":[]}`)
	if e := r.TMDb.SaveResponse(ctx, req, raw, at, ""); e != nil {
		t.Fatal(e)
	}
	p, _ := r.TMDb.PersonIdentity(ctx, 5)
	rows, n, e := r.TMDb.PersonWorks(ctx, p.ID, "", "", "newest", 1, 1)
	if e != nil || n != 2 || len(rows) != 2 || rows[0].Subject.TMDbID != 1 {
		t.Fatalf("group pagination %d %#v %v", n, rows, e)
	}
	rows, n, e = r.TMDb.PersonWorks(ctx, p.ID, "series", "cast", "oldest", 1, 1)
	if e != nil || n != 1 || len(rows) != 1 || rows[0].Subject.TMDbID != 2 {
		t.Fatalf("filtered pagination %d %v", n, e)
	}
}

func TestTMDbCenterOlderResponseReleasesLease(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := context.Background()
	now := time.Now()
	req := TMDbSourceRequest{Kind: "movie", TMDbID: 15, Component: "base", Language: "zh-CN", Path: "/movie/15", Key: "older", ConfigKey: "test"}
	if _, e := r.TMDb.Enqueue(ctx, req, "", true); e != nil {
		t.Fatal(e)
	}
	j, e := r.TMDb.Claim(ctx, req.Key)
	if e != nil || j == nil {
		t.Fatal(e)
	}
	if e = r.TMDb.SaveResponse(ctx, req, []byte(`{"id":15,"title":"新"}`), now, ""); e != nil {
		t.Fatal(e)
	}
	if e = r.TMDb.SaveResponse(ctx, req, []byte(`{"id":15,"title":"旧"}`), now.Add(-time.Hour), j.Token); e != nil {
		t.Fatal(e)
	}
	j, e = r.TMDb.Job(ctx, req.Key)
	if e != nil || j.Status != "ready" || j.Token != "" || j.PayloadID == nil || j.LastSuccessAt == nil || !j.LastSuccessAt.Equal(now.Truncate(time.Microsecond)) {
		t.Fatalf("stale completion left lease %#v %v", j, e)
	}
	s, _ := r.TMDb.SubjectIdentity(ctx, "movie", 15)
	if s.Title != "新" {
		t.Fatal("stale projected over current")
	}
}

func TestTMDbCenterCreditsDoNotRegressAcrossLanguages(t *testing.T) {
	for _, tc := range []struct {
		name, component, language, newer string
		wantCast, wantCrew               string
	}{
		{"detail", "detail", "zh-CN", `{"id":12,"credits":{"cast":[{"id":5,"name":"Actor","character":"new"}],"crew":[]}}`, "new", ""},
		{"empty credits", "credits", "zh-CN", `{"id":12,"cast":[],"crew":[]}`, "", ""},
		{"partial group", "detail", "zh-CN", `{"id":12,"credits":{"cast":[]}}`, "", "old"},
		{"base without credits", "base", "zh-CN", `{"id":12,"title":"New title"}`, "old", "old"},
		{"secondary language", "detail", "en-US", `{"id":12,"credits":{"cast":[],"crew":[]}}`, "old", "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tmdbTestRepo(t)
			ctx := t.Context()
			at := time.Now().UTC().Truncate(time.Microsecond)
			if err := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "movie", TMDbID: 12, Component: "summary", Language: "und"}, []byte(`{"id":12}`), at.Add(-2*time.Hour), ""); err != nil {
				t.Fatal(err)
			}
			newReq := TMDbSourceRequest{Kind: "movie", TMDbID: 12, Component: tc.component, Language: tc.language}
			if err := r.TMDb.SaveResponse(ctx, newReq, []byte(tc.newer), at, ""); err != nil {
				t.Fatal(err)
			}
			oldReq := TMDbSourceRequest{Kind: "movie", TMDbID: 12, Component: "detail", Language: "und", Origin: "legacy"}
			old := []byte(`{"id":12,"credits":{"cast":[{"id":6,"name":"Old actor","character":"old"}],"crew":[{"id":7,"name":"Old crew","job":"old"}]}}`)
			if err := r.TMDb.SaveResponse(ctx, oldReq, old, at.Add(-time.Hour), ""); err != nil {
				t.Fatal(err)
			}
			subject, err := r.TMDb.SubjectIdentity(ctx, "movie", 12)
			if err != nil {
				t.Fatal(err)
			}
			rows, _, err := r.TMDb.Credits(ctx, subject.ID, false, 1, 100)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, row := range rows {
				if row.Category == "cast" {
					got["cast"] = row.Character
				} else {
					got["crew"] = row.Job
				}
			}
			if got["cast"] != tc.wantCast || got["crew"] != tc.wantCrew {
				t.Fatalf("relationships regressed: %v", got)
			}
			payloads, err := r.TMDb.Payloads(ctx, subject.ID, false)
			if err != nil || len(payloads) != 3 {
				t.Fatalf("old payload not retained: %d, %v", len(payloads), err)
			}
			// 正常的较新 und 更新仍可清空关系。
			if err := r.TMDb.SaveResponse(ctx, oldReq, []byte(`{"id":12,"credits":{"cast":[],"crew":[]}}`), at.Add(time.Hour), ""); err != nil {
				t.Fatal(err)
			}
			_, count, err := r.TMDb.Credits(ctx, subject.ID, false, 1, 100)
			if err != nil || count != 0 {
				t.Fatalf("new response blocked: %d, %v", count, err)
			}
		})
	}
}

func TestTMDbCenterPersonAndEpisodeCreditsDoNotRegress(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	personReq := TMDbSourceRequest{Kind: "person", TMDbID: 5, Component: "detail", Language: "zh-CN"}
	if err := r.TMDb.SaveResponse(ctx, personReq, []byte(`{"id":5,"name":"Actor"}`), at, ""); err != nil {
		t.Fatal(err)
	}
	personReq.Component = "filmography"
	if err := r.TMDb.SaveResponse(ctx, personReq, []byte(`{"id":5,"cast":[],"crew":[]}`), at, ""); err != nil {
		t.Fatal(err)
	}
	personReq.Language = "und"
	if err := r.TMDb.SaveResponse(ctx, personReq, []byte(`{"id":5,"cast":[{"id":12,"media_type":"movie","character":"old"}],"crew":[]}`), at.Add(-time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	person, err := r.TMDb.PersonIdentity(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	_, n, err := r.TMDb.Credits(ctx, person.ID, true, 1, 100)
	if err != nil || n != 0 {
		t.Fatalf("empty filmography regressed: %d, %v", n, err)
	}
	seasonReq := TMDbSourceRequest{Kind: "season", TMDbID: 30, SeriesID: 20, Season: 1, Component: "detail", Language: "zh-CN"}
	season := []byte(`{"id":30,"season_number":1,"episodes":[{"id":40,"season_number":1,"episode_number":1,"guest_stars":[],"crew":[]}]}`)
	if err := r.TMDb.SaveResponse(ctx, seasonReq, season, at, ""); err != nil {
		t.Fatal(err)
	}
	epReq := TMDbSourceRequest{Kind: "episode", TMDbID: 40, SeriesID: 20, Season: 1, Episode: 1, Component: "detail", Language: "und"}
	old := []byte(`{"id":40,"season_number":1,"episode_number":1,"credits":{"cast":[{"id":5,"character":"old"}],"crew":[]}}`)
	if err := r.TMDb.SaveResponse(ctx, epReq, old, at.Add(-time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	ep, err := r.TMDb.SubjectIdentity(ctx, "episode", 40)
	if err != nil {
		t.Fatal(err)
	}
	_, n, err = r.TMDb.Credits(ctx, ep.ID, false, 1, 100)
	if err != nil || n != 0 {
		t.Fatalf("empty season episode credits regressed: %d, %v", n, err)
	}
	// 反向覆盖也受保护：较旧的整季数据不能覆盖较新单集演职员。
	if err := r.TMDb.SaveResponse(ctx, epReq, old, at.Add(time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	seasonReq.Language = "und"
	if err := r.TMDb.SaveResponse(ctx, seasonReq, season, at, ""); err != nil {
		t.Fatal(err)
	}
	_, n, err = r.TMDb.Credits(ctx, ep.ID, false, 1, 100)
	if err != nil || n != 1 {
		t.Fatalf("new episode credits cleared by older season: %d, %v", n, err)
	}
}
