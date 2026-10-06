package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/logger"
)

type detailQueryLog struct {
	logger.Interface
	views int
}

func (l *detailQueryLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.Contains(sql, "AS view_series_id") {
		l.views++
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func TestEmbyDetailReusesViewsAndPreservesPayload(t *testing.T) {
	e := newTestEmbyService(t)
	db := e.repo.DB
	lib := model.Library{Name: "Movies", Type: "movie"}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "movie", Title: "Movie", Source: "local"},
		model.MetadataIdentifier{Provider: "imdb", EntityKind: "movie", ExternalID: "tt-fixture"})
	otherMetadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "movie", Title: "Part 2", Source: "local"})
	for _, row := range []model.Media{
		{PermanentBase: model.PermanentBase{ID: "detail-part-1"}, MetadataID: metadata.ID, Path: "/fixture/movie-part1.mkv", LibraryID: lib.ID, PartGroupKey: "parts", PartIndex: 1, Width: 1920},
		{PermanentBase: model.PermanentBase{ID: "detail-part-2"}, MetadataID: metadata.ID, Path: "/fixture/movie-part2.mkv", LibraryID: lib.ID, PartGroupKey: "parts", PartIndex: 2, Width: 1920},
		{PermanentBase: model.PermanentBase{ID: "detail-cross-part"}, MetadataID: otherMetadata.ID, Path: "/fixture/movie-part3.mkv", LibraryID: lib.ID, PartGroupKey: "parts", PartIndex: 3, Width: 1920},
		{PermanentBase: model.PermanentBase{ID: "detail-version"}, MetadataID: metadata.ID, Path: "/fixture/movie.mkv", LibraryID: lib.ID, Width: 1280},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := e.repo.MediaProbe.Upsert(t.Context(), &model.MediaProbeMetadata{
			MediaID: row.ID, ProbeJSON: `{"schema_version":1,"format":{"duration":120},"streams":[]}`,
			SchemaVersion: ProbeDocumentSchemaVersion, SummaryVersion: ProbeSummaryVersion,
			DurationMS: 120_000, Width: row.Width, ProbedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Favorite{UserID: "viewer", MetadataID: metadata.ID}).Error; err != nil {
		t.Fatal(err)
	}
	log := &detailQueryLog{Interface: db.Logger}
	db.Logger = log
	for _, id := range []string{metadata.ID, "detail-part-1", "detail-part-2", "detail-version"} {
		t.Run(id, func(t *testing.T) {
			// 原路径分别解析媒体、用户状态目标、版本和 Part，作为完整响应对照。
			log.views = 0
			m, err := e.mediaViewForItemID(t.Context(), id, "viewer")
			if err != nil || m == nil {
				t.Fatalf("media=%v err=%v", m, err)
			}
			target, err := e.itemTarget(t.Context(), id, "viewer")
			if err != nil {
				t.Fatal(err)
			}
			fav, pos, played := e.userDataForTarget(t.Context(), "viewer", target)
			want := e.itemPayloadWithRelations(t.Context(), m, "viewer", fav, pos, played, true, nil)
			before := log.views
			log.views = 0
			got, err := e.Item(t.Context(), id, "viewer")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("detail changed: err=%v\ngot=%#v\nwant=%#v", err, got, want)
			}
			firstSource := "detail-part-1"
			if id == "detail-version" {
				firstSource = id
			}
			sources := got["MediaSources"].([]map[string]any)
			if len(sources) != 2 || sources[0]["Id"] != firstSource {
				t.Fatalf("preferred/concrete source changed: %#v", sources)
			}
			if log.views >= before {
				t.Fatalf("view queries=%d want less than %d", log.views, before)
			}
			t.Logf("view queries: %d -> %d", before, log.views)
			if m.PartGroupKey != "" && got["PartCount"] != 3 {
				t.Fatalf("cross-metadata parts lost: %v", got["PartCount"])
			}
		})
	}
}
