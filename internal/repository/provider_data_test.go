package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestProviderDataUsesCenterWithoutLocalCopies(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	local := model.MetadataItem{Kind: "movie", Title: "本地标题", Source: "manual"}
	if err := r.Metadata.Create(ctx, &local, []model.MetadataIdentifier{{Provider: "tmdb", EntityKind: "movie", ExternalID: "12"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Metadata.SaveDegradedProviderData(ctx, local.ID, "tmdb", []byte(`{"id":12,"title":"摘要"}`), at); err != nil {
		t.Fatal(err)
	}
	data, err := r.Metadata.FindProviderData(ctx, local.ID, "tmdb")
	if err != nil || data == nil || !data.Degraded {
		t.Fatalf("summary unavailable: %+v %v", data, err)
	}
	req := TMDbSourceRequest{Kind: "movie", TMDbID: 12, Component: "detail", Language: "zh-CN"}
	if err := r.TMDb.SaveResponse(ctx, req, []byte(`{"id":12,"title":"完整资料","external_ids":{"imdb_id":"tt12"},"vote_average":7}`), at.Add(time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	req.Component = "base"
	if err := r.TMDb.SaveResponse(ctx, req, []byte(`{"id":12,"title":"更新标题","vote_average":8}`), at.Add(2*time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	data, err = r.Metadata.FindProviderData(ctx, local.ID, "tmdb")
	if err != nil || data == nil || data.Degraded {
		t.Fatalf("detail unavailable: %+v %v", data, err)
	}
	var body struct {
		Title       string
		VoteAverage int `json:"vote_average"`
		ExternalIDs struct {
			IMDb string `json:"imdb_id"`
		} `json:"external_ids"`
	}
	if err = json.Unmarshal([]byte(data.Payload), &body); err != nil || body.Title != "更新标题" || body.VoteAverage != 8 || body.ExternalIDs.IMDb != "tt12" {
		t.Fatalf("center overlay: %+v %v", body, err)
	}
	unchanged, err := r.Metadata.FindByID(ctx, local.ID)
	if err != nil || unchanged.Title != "本地标题" {
		t.Fatalf("source overwrote local fields: %+v %v", unchanged, err)
	}
	if err := r.Metadata.SaveProviderData(ctx, local.ID, "unknown", []byte(`{"id":12}`), at); err == nil {
		t.Fatal("unsupported provider wrote a local copy")
	}
	for _, table := range []string{"metadata_provider_snapshots", "provider_data", "tmdb_migration_failures"} {
		if r.DB.Migrator().HasTable(table) {
			t.Fatalf("retired table exists: %s", table)
		}
	}
}

func TestTMDbCoordinatesStayStrictForEveryOrigin(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	for _, origin := range []string{"api", "legacy", "legacy_projection"} {
		for _, value := range []string{"", `,"season_number":null`, `,"season_number":"1"`, `,"season_number":2`} {
			req := TMDbSourceRequest{Kind: "season", TMDbID: 30, SeriesID: 20, Season: 1, Component: "detail", Language: "zh-CN", Origin: origin}
			if err := r.TMDb.SaveResponse(ctx, req, []byte(`{"id":30`+value+`}`), time.Now(), ""); err == nil {
				t.Fatalf("invalid coordinate accepted: origin=%s value=%s", origin, value)
			}
		}
	}
}
