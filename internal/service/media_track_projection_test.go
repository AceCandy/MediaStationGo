package service

import (
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestGetMediaAddsCompleteTracksButListsStayScalar(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	media := model.Media{PermanentBase: model.PermanentBase{ID: "media-track-detail"}, MetadataID: metadata.ID, LibraryID: lib.ID, Title: "Movie", Path: "/media/movies/movie.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Streams: []ProbeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc", Width: 3840, Height: 2160, PixelFormat: "yuv420p10le", ColorTransfer: "smpte2084"},
		{Index: 3, CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, ChannelLayout: "stereo", Tags: ProbeTags{Language: "chi", Title: "标准"}},
		{Index: 5, CodecType: "subtitle", CodecName: "ass", Tags: ProbeTags{Language: "chi", Title: "简体"}},
	}}
	probeJSON, err := MarshalProbeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: probeJSON, SchemaVersion: ProbeDocumentSchemaVersion}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos).SetMediaProbe(NewMediaProbeService(repos, nil))
	detail, err := svc.GetMedia(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Tracks) != 3 || detail.Tracks[1].Index != 3 || detail.Tracks[1].DisplayLanguage != "Chinese" || detail.Tracks[0].BitDepth != 10 {
		t.Fatalf("detail tracks = %#v", detail.Tracks)
	}
	items, _, err := svc.ListMediaVisible(t.Context(), lib.ID, 1, 10, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Tracks != nil {
		t.Fatalf("list tracks = %#v, want nil", items[0].Tracks)
	}
}

func TestGetMediaAddsProviderSnapshotStateAndSeriesTMDbID(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	lib := model.Library{Name: "Series", Path: "/media/series", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	episode := createServiceTestEpisodeMetadata(t, db,
		model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"},
		model.MetadataItem{Kind: model.MetadataKindEpisode, SeasonNum: 1, EpisodeNum: 2, Title: "Episode", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "100"},
	)
	if err := db.Create(&model.MetadataIdentifier{MetadataID: episode.ID, Provider: "tmdb", EntityKind: model.MetadataKindEpisode, ExternalID: "200"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: episode.ID, Provider: "tmdb", Payload: `{"id":200}`, FetchedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{PermanentBase: model.PermanentBase{ID: "media-provider-detail"}, MetadataID: episode.ID, LibraryID: lib.ID, Title: "Episode", Path: "/media/series/s01e02.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	detail, err := NewMediaService(&config.Config{}, zap.NewNop(), repos).GetMedia(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.MetadataKind != model.MetadataKindEpisode || detail.TMDbID != 200 || detail.SeriesTMDbID != 100 || !detail.TMDbSnapshot || detail.DoubanSnapshot {
		t.Fatalf("provider detail = %#v", detail)
	}

	movie := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1295644"},
	)
	if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: movie.ID, Provider: "douban", Payload: `{"subject":{}}`, FetchedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	movieMedia := model.Media{PermanentBase: model.PermanentBase{ID: "movie-provider-detail"}, MetadataID: movie.ID, LibraryID: lib.ID, Title: "Movie", Path: "/media/movie.mkv"}
	if err := db.Create(&movieMedia).Error; err != nil {
		t.Fatal(err)
	}
	movieDetail, err := NewMediaService(&config.Config{}, zap.NewNop(), repos).GetMedia(t.Context(), movieMedia.ID)
	if err != nil || movieDetail == nil || !movieDetail.DoubanSnapshot || movieDetail.DoubanID != "1295644" {
		t.Fatalf("douban provider detail = %#v, err = %v", movieDetail, err)
	}
}
