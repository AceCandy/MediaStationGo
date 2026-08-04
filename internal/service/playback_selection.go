package service

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var ErrInvalidStreamIndex = errors.New("invalid media stream index")

// PlaybackSelection 保留缺省、0 与 -1 的区别，供 PlaybackInfo 和 HLS 共用。
type PlaybackSelection struct {
	MediaSourceID       string
	AudioStreamIndex    *int
	SubtitleStreamIndex *int
}

func PlaybackSelectionFromQuery(values url.Values) (PlaybackSelection, error) {
	selection := PlaybackSelection{MediaSourceID: firstQueryValue(values, "MediaSourceId", "MediaSourceID", "mediaSourceId")}
	var err error
	if raw, ok := queryValue(values, "AudioStreamIndex", "audioStreamIndex"); ok {
		selection.AudioStreamIndex, err = parseStreamIndex(raw)
		if err != nil {
			return PlaybackSelection{}, err
		}
	}
	if raw, ok := queryValue(values, "SubtitleStreamIndex", "subtitleStreamIndex"); ok {
		selection.SubtitleStreamIndex, err = parseStreamIndex(raw)
		if err != nil {
			return PlaybackSelection{}, err
		}
	}
	return selection, nil
}

func parseStreamIndex(raw string) (*int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < -1 {
		return nil, ErrInvalidStreamIndex
	}
	return &value, nil
}

func queryValue(values url.Values, names ...string) (string, bool) {
	for _, name := range names {
		if raw, ok := values[name]; ok && len(raw) > 0 {
			return raw[len(raw)-1], true
		}
	}
	return "", false
}

func firstQueryValue(values url.Values, names ...string) string {
	raw, _ := queryValue(values, names...)
	return strings.TrimSpace(raw)
}

func validatePlaybackSelectionValues(selection PlaybackSelection) error {
	for _, index := range []*int{selection.AudioStreamIndex, selection.SubtitleStreamIndex} {
		if index != nil && *index < -1 {
			return ErrInvalidStreamIndex
		}
	}
	return nil
}

func resolveAudioStreamIndex(doc *ProbeDocument, requested *int) (int, error) {
	if doc == nil {
		if requested != nil && *requested >= 0 {
			return -1, ErrInvalidStreamIndex
		}
		return -1, nil
	}
	audio := make([]ProbeStream, 0)
	for _, stream := range doc.Streams {
		if stream.CodecType == "audio" {
			audio = append(audio, stream)
		}
	}
	if len(audio) == 0 {
		if requested != nil && *requested >= 0 {
			return -1, ErrInvalidStreamIndex
		}
		return -1, nil
	}
	if requested != nil && *requested >= 0 {
		for _, stream := range audio {
			if stream.Index == *requested {
				return stream.Index, nil
			}
		}
		return -1, ErrInvalidStreamIndex
	}
	for _, stream := range audio {
		if stream.Disposition.Default {
			return stream.Index, nil
		}
	}
	return audio[0].Index, nil
}
