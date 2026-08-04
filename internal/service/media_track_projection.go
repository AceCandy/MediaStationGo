package service

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func projectProbeTracks(doc *ProbeDocument) []model.MediaTrack {
	if doc == nil {
		return nil
	}
	tracks := make([]model.MediaTrack, 0, len(doc.Streams))
	for _, stream := range doc.Streams {
		if track, ok := projectProbeTrack(stream); ok {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

func projectProbeTrack(stream ProbeStream) (model.MediaTrack, bool) {
	if stream.CodecType != "video" && stream.CodecType != "audio" && stream.CodecType != "subtitle" {
		return model.MediaTrack{}, false
	}
	track := model.MediaTrack{
		Index:             stream.Index,
		Type:              stream.CodecType,
		Codec:             stream.CodecName,
		Profile:           stream.Profile,
		Level:             stream.Level,
		TimeBase:          stream.TimeBase,
		Language:          stream.Tags.Language,
		DisplayLanguage:   probeDisplayLanguage(stream.Tags.Language),
		Title:             stream.Tags.Title,
		DisplayTitle:      probeStreamDisplayTitle(stream),
		BitRate:           stream.BitRate,
		IsDefault:         stream.Disposition.Default,
		IsForced:          stream.Disposition.Forced,
		IsHearingImpaired: stream.Disposition.HearingImpaired,
		IsVisualImpaired:  stream.Disposition.VisualImpaired,
	}
	if track.BitRate < 0 {
		track.BitRate = 0
	}
	if stream.CodecType == "video" {
		track.Width = stream.Width
		track.Height = stream.Height
		track.AspectRatio = firstNonEmpty(stream.DisplayAspectRatio, stream.SampleAspectRatio)
		track.PixelFormat = stream.PixelFormat
		track.BitDepth = probeBitDepth(stream)
		track.ColorRange = stream.ColorRange
		track.ColorSpace = stream.ColorSpace
		track.ColorTransfer = stream.ColorTransfer
		track.ColorPrimaries = stream.ColorPrimaries
		track.VideoRange = probeVideoRange(stream)
		if rate, ok := probeFrameRate(stream.AverageFrameRate); ok {
			track.AverageFrameRate = rate
		}
		if rate, ok := probeFrameRate(stream.RealFrameRate); ok {
			track.RealFrameRate = rate
		}
	} else if stream.CodecType == "audio" {
		track.Channels = stream.Channels
		track.SampleRate = stream.SampleRate
		track.ChannelLayout = stream.ChannelLayout
		track.SampleFormat = stream.SampleFormat
		track.BitsPerSample = stream.BitsPerSample
	} else {
		track.IsTextSubtitle = isTextSubtitleCodec(stream.CodecName)
	}
	return track, true
}

func probeBitDepth(stream ProbeStream) int {
	if stream.BitDepth > 0 {
		return stream.BitDepth
	}
	return pixelFormatBitDepth(stream.PixelFormat)
}

func probeDisplayLanguage(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || strings.EqualFold(code, "und") {
		return ""
	}
	tag, err := language.Parse(code)
	if err != nil {
		return ""
	}
	return display.English.Languages().Name(tag)
}

func isTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa", "srt", "subrip", "webvtt", "mov_text", "text", "ttml":
		return true
	default:
		return false
	}
}
