package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestEmbyMediaStreamsMapCompleteProbeAndLiveSidecar(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.zh.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\n你好\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{MetadataID: metadata.ID, LibraryID: "library", Title: "Movie", Path: mediaPath, VideoCodec: "hevc", AudioCodec: "aac"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Streams: []ProbeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc", Profile: "Main 10", Level: 156, TimeBase: "1/1000", Width: 3840, Height: 2160, PixelFormat: "yuv420p10le", BitDepth: 10, ColorTransfer: "smpte2084", AverageFrameRate: "25/1"},
		{Index: 2, CodecType: "audio", CodecName: "aac", Profile: "LC", TimeBase: "1/1000", SampleRate: 48000, Channels: 2, ChannelLayout: "stereo", Tags: ProbeTags{Language: "chi"}, Disposition: ProbeDisposition{Default: true}},
		{Index: 4, CodecType: "subtitle", CodecName: "ass", Tags: ProbeTags{Language: "chi", Title: "Signs"}, Disposition: ProbeDisposition{Forced: true}},
	}}
	probeJSON, _ := MarshalProbeDocument(doc)
	if err := db.Create(&model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: probeJSON, SchemaVersion: ProbeDocumentSchemaVersion}).Error; err != nil {
		t.Fatal(err)
	}
	mediaProbe := NewMediaProbeService(repos, nil)
	subtitles := NewSubtitleService(zap.NewNop(), repos)
	subtitles.SetMediaProbe(mediaProbe)
	emby := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	emby.SetMediaProbe(mediaProbe)
	emby.SetSubtitle(subtitles)
	streams := emby.mediaStreams(t.Context(), &media, doc, true, nil)
	if len(streams) != 4 {
		t.Fatalf("streams = %#v", streams)
	}
	if streams[0]["Index"] != 0 || streams[0]["VideoRange"] != "HDR 10" || streams[0]["ExtendedVideoType"] != "Hdr10" || streams[0]["Level"] != 156 || streams[0]["TimeBase"] != "1/1000" || streams[0]["DisplayTitle"] != "4K HDR 10 HEVC" || streams[0]["AverageFrameRate"] != float64(25) {
		t.Fatalf("embedded streams mapped incorrectly: %#v", streams)
	}
	if streams[1]["Index"] != 2 || streams[1]["DisplayLanguage"] != "Chinese" || streams[1]["DisplayTitle"] != "Chinese AAC stereo (默认)" || streams[1]["Profile"] != "LC" {
		t.Fatalf("audio stream mapped incorrectly: %#v", streams)
	}
	if streams[2]["Index"] != 4 {
		t.Fatalf("embedded subtitle index changed: %#v", streams)
	}
	if streams[2]["IsExternal"] != false || streams[2]["DeliveryUrl"] != nil {
		t.Fatalf("embedded subtitle should remain an in-file track without extraction URL: %#v", streams[2])
	}
	if streams[3]["Index"] != 5 || streams[3]["IsExternal"] != true || streams[3]["DeliveryUrl"] != embySubtitleDeliveryURL(media.ID, 5) {
		t.Fatalf("sidecar stream mapped incorrectly: %#v", streams[3])
	}
	var out bytes.Buffer
	singleMediaReads := 0
	if err := db.Callback().Query().Before("gorm:query").Register("test:count-subtitle-media", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.Media); ok {
			singleMediaReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := subtitles.ServeByIndex(t.Context(), media.ID, 5, &out); err != nil {
		t.Fatal(err)
	}
	if singleMediaReads != 1 {
		t.Fatalf("subtitle service loaded media %d times, want once", singleMediaReads)
	}
	if got := out.String(); !strings.HasPrefix(got, "WEBVTT") {
		t.Fatalf("subtitle output = %q", got)
	}
	if err := subtitles.Serve(t.Context(), media.ID, filepath.Join(dir, "..", "escape.srt"), &bytes.Buffer{}); err == nil {
		t.Fatal("subtitle path escape was accepted")
	}
	if err := os.Remove(filepath.Join(dir, "movie.zh.srt")); err != nil {
		t.Fatal(err)
	}
	if err := subtitles.ServeByIndex(t.Context(), media.ID, 5, &bytes.Buffer{}); err == nil {
		t.Fatal("deleted sidecar remained addressable")
	}
}

func TestProbeExtendedVideoTypeUsesEmbyEnumValues(t *testing.T) {
	for _, test := range []struct {
		videoRange  string
		wantType    string
		wantSubType string
	}{
		{videoRange: "HDR 10", wantType: "Hdr10", wantSubType: "Hdr10"},
		{videoRange: "HDR 10+", wantType: "Hdr10Plus", wantSubType: "Hdr10Plus0"},
		{videoRange: "Dolby Vision", wantType: "DolbyVision", wantSubType: "None"},
		{videoRange: "HLG", wantType: "HyperLogGamma", wantSubType: "HyperLogGamma"},
	} {
		t.Run(test.videoRange, func(t *testing.T) {
			gotType, gotSubType, _ := probeExtendedVideoType(test.videoRange)
			if gotType != test.wantType || gotSubType != test.wantSubType {
				t.Fatalf("probeExtendedVideoType(%q) = %q, %q", test.videoRange, gotType, gotSubType)
			}
		})
	}
}

func TestEmbyItemsUseScalarStreamsWhileItemUsesCompleteProbeDocument(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.DB.AutoMigrate(&model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	mediaProbe := NewMediaProbeService(svc.repo, nil)
	svc.SetMediaProbe(mediaProbe)
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.zh.srt"), []byte("subtitle"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		PermanentBase: model.PermanentBase{ID: "media-stream-list"}, MetadataID: metadata.ID, LibraryID: lib.ID,
		Title: "Movie", Path: mediaPath, VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Streams: []ProbeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc", Width: 3840, Height: 2160},
		{Index: 3, CodecType: "audio", CodecName: "eac3", Disposition: ProbeDisposition{Default: true}},
	}}
	probeJSON, err := MarshalProbeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DB.Create(&model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: probeJSON, SchemaVersion: ProbeDocumentSchemaVersion}).Error; err != nil {
		t.Fatal(err)
	}
	subtitles := NewSubtitleService(zap.NewNop(), svc.repo)
	subtitles.SetMediaProbe(mediaProbe)
	svc.SetSubtitle(subtitles)

	listed, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Recursive: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	listStreams := listed["Items"].([]map[string]any)[0]["MediaSources"].([]map[string]any)[0]["MediaStreams"].([]map[string]any)
	if len(listStreams) != 2 || listStreams[1]["Index"] != 1 || listStreams[1]["Codec"] != "aac" {
		t.Fatalf("list streams = %#v, want scalar projection", listStreams)
	}

	detail, err := svc.Item(t.Context(), media.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	detailStreams := detail["MediaSources"].([]map[string]any)[0]["MediaStreams"].([]map[string]any)
	if len(detailStreams) != 3 || detailStreams[1]["Index"] != 3 || detailStreams[1]["Codec"] != "eac3" || detailStreams[2]["Type"] != "Subtitle" {
		t.Fatalf("detail streams = %#v, want complete probe document", detailStreams)
	}
}

func TestEmbyItemsDoNotScheduleLazyProbe(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.DB.AutoMigrate(&model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{PermanentBase: model.PermanentBase{ID: "media-list-probe"}, MetadataID: metadata.ID, LibraryID: lib.ID, Title: "Movie", Path: path}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	runner := &stubMediaProbeRunner{result: probeResultFixture(), onProbe: func() {
		close(started)
		<-release
	}}
	svc.SetMediaProbe(NewMediaProbeService(svc.repo, runner))

	if _, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Recursive: true, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	svc.trackProbeMu.Lock()
	pending := len(svc.trackProbeInFlight)
	svc.trackProbeMu.Unlock()
	if pending != 0 {
		t.Fatal("list request reserved a lazy probe")
	}
	select {
	case <-started:
		t.Fatal("list request scheduled a lazy probe")
	default:
	}

	if _, err := svc.Item(t.Context(), media.ID, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("item detail did not schedule a lazy probe")
	}
	close(release)
	released = true
}
