package service

import (
	"testing"

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
	media := model.Media{Base: model.Base{ID: "media-track-detail"}, MetadataID: metadata.ID, LibraryID: lib.ID, Title: "Movie", Path: "/media/movies/movie.mkv"}
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
