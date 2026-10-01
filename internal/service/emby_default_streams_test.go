package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyMediaSourceDefaultStreamIndices(t *testing.T) {
	index := func(value int) *int { return &value }
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Streams: []ProbeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc"},
		{Index: 1, CodecType: "audio", CodecName: "aac"},
		{Index: 3, CodecType: "audio", CodecName: "aac", Disposition: ProbeDisposition{Default: true}},
	}}
	media := model.Media{PermanentBase: model.PermanentBase{ID: "selected"}, Path: "/fixture/video.mkv"}
	for _, tc := range []struct {
		name            string
		doc             *ProbeDocument
		audioCodec      string
		subtitles       []SubtitleSelection
		selection       PlaybackSelection
		audio, subtitle int
	}{
		{name: "default_audio", doc: doc, audio: 3, subtitle: -1},
		{name: "first_audio", doc: &ProbeDocument{Streams: doc.Streams[:2]}, audio: 1, subtitle: -1},
		{name: "no_tracks", audio: -1, subtitle: -1},
		{name: "scalar_audio", audioCodec: "aac", audio: 1, subtitle: -1},
		{name: "scalar_audio_empty_probe", doc: &ProbeDocument{}, audioCodec: "aac", audio: 1, subtitle: -1},
		{name: "default_subtitle", doc: doc, subtitles: []SubtitleSelection{{Index: 5, Codec: "srt", Default: true}}, audio: 3, subtitle: 5},
		{name: "unselected_subtitle", doc: doc, subtitles: []SubtitleSelection{{Index: 5, Codec: "srt"}}, audio: 3, subtitle: -1},
		{name: "explicit_audio", doc: doc, selection: PlaybackSelection{MediaSourceID: "selected", AudioStreamIndex: index(1)}, audio: 1, subtitle: -1},
		{name: "audio_minus_one_uses_default", doc: doc, selection: PlaybackSelection{MediaSourceID: "selected", AudioStreamIndex: index(-1)}, audio: 3, subtitle: -1},
		{name: "explicit_subtitle", doc: doc, subtitles: []SubtitleSelection{{Index: 5, Codec: "srt"}}, selection: PlaybackSelection{MediaSourceID: "selected", SubtitleStreamIndex: index(5)}, audio: 3, subtitle: 5},
		{name: "subtitle_disabled", doc: doc, subtitles: []SubtitleSelection{{Index: 5, Codec: "srt", Default: true}}, selection: PlaybackSelection{MediaSourceID: "selected", SubtitleStreamIndex: index(-1)}, audio: 3, subtitle: -1},
		{name: "other_version", doc: doc, selection: PlaybackSelection{MediaSourceID: "other", AudioStreamIndex: index(1), SubtitleStreamIndex: index(5)}, audio: 3, subtitle: -1},
		{name: "audio_index_zero", doc: &ProbeDocument{Streams: []ProbeStream{{Index: 0, CodecType: "audio", CodecName: "aac"}}}, selection: PlaybackSelection{MediaSourceID: "selected", AudioStreamIndex: index(0)}, audio: 0, subtitle: -1},
		{name: "subtitle_index_zero", doc: &ProbeDocument{Streams: doc.Streams[1:]}, subtitles: []SubtitleSelection{{Index: 0, Codec: "srt"}}, selection: PlaybackSelection{MediaSourceID: "selected", SubtitleStreamIndex: index(0)}, audio: 3, subtitle: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := media
			m.AudioCodec = tc.audioCodec
			e := &EmbyService{}
			for _, embedded := range []bool{false, true} {
				source := e.mediaSourceWithSelection(t.Context(), &m, "fixture", embedded, tc.doc, tc.selection, true, tc.subtitles)
				if source["DefaultAudioStreamIndex"] != tc.audio || source["DefaultSubtitleStreamIndex"] != tc.subtitle {
					t.Fatalf("embedded=%v defaults=%v/%v, want %d/%d", embedded, source["DefaultAudioStreamIndex"], source["DefaultSubtitleStreamIndex"], tc.audio, tc.subtitle)
				}
			}
		})
	}
}
