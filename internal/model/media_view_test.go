package model

import "testing"

func TestMediaViewNormalizeUsesProbeSummary(t *testing.T) {
	view := MediaView{
		Media:           Media{DurationSec: 9, SizeBytes: 9, Container: "legacy"},
		ProbeDurationMS: 5_930_112,
		ProbeSizeBytes:  42,
		ProbeContainer:  "matroska",
		ProbeWidth:      3840,
		ProbeHeight:     2160,
		ProbeVideoCodec: "hevc",
		ProbeAudioCodec: "eac3",
	}
	view.Normalize()
	if view.DurationSec != 5930 || view.SizeBytes != 42 || view.Container != "matroska" ||
		view.Width != 3840 || view.Height != 2160 || view.VideoCodec != "hevc" || view.AudioCodec != "eac3" {
		t.Fatalf("probe summary not projected: %#v", view.Media)
	}
}
