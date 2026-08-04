package service

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ServeHLSPlaylist makes sure a transcode is running and writes the m3u8.
// We block (with a 30s timeout) until the playlist file shows up.
func (s *StreamService) ServeHLSPlaylist(w http.ResponseWriter, r *http.Request, mediaID string) error {
	// 「客户端直连解码」模式下宿主机不提供转码，HLS 一律拒绝，
	// 迫使播放器走 direct play 本地解码。
	if s.directPlayOnly(r.Context()) {
		return ErrTranscodeDisabled
	}
	key, err := s.hlsKey(r, mediaID)
	if err != nil {
		return err
	}
	if _, err := s.transcoder.EnsureJob(r.Context(), key); err != nil {
		return err
	}
	s.transcoder.TouchJob(key)
	if !s.transcoder.WaitReady(r.Context(), key, 30*time.Second) {
		return errors.New("hls playlist not ready")
	}
	playlist := s.transcoder.PlaylistPath(key)
	f, err := os.Open(playlist) // #nosec G304 -- playlist path is generated under the transcoder cache directory for this media ID.
	if err != nil {
		return err
	}
	defer f.Close()
	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Disposition", "inline")
	segmentQuery := resolvedHLSQuery(r.URL.RawQuery, key)
	if segmentQuery != "" {
		data, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		playlist := appendQueryToHLSSegments(string(data), segmentQuery)
		_, err = io.WriteString(w, playlist)
		return err
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return nil
}

func appendQueryToHLSSegments(playlist, rawQuery string) string {
	rawQuery = allowedHLSQuery(rawQuery)
	if rawQuery == "" {
		return playlist
	}
	lines := strings.SplitAfter(playlist, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.Contains(trimmed, "?") {
			continue
		}
		if strings.HasSuffix(strings.ToLower(trimmed), ".ts") {
			lineEnding := ""
			if strings.HasSuffix(line, "\r\n") {
				lineEnding = "\r\n"
			} else if strings.HasSuffix(line, "\n") {
				lineEnding = "\n"
			}
			lines[i] = strings.TrimRight(line, "\r\n") + "?" + rawQuery + lineEnding
		}
	}
	return strings.Join(lines, "")
}

func allowedHLSQuery(rawQuery string) string {
	values, err := url.ParseQuery(strings.TrimSpace(rawQuery))
	if err != nil {
		return ""
	}
	allowed := url.Values{}
	for _, key := range []string{"api_key", "apiKey", "token", "X-Emby-Token", "X-MediaBrowser-Token", "AudioStreamIndex", "audioStreamIndex", "SubtitleStreamIndex", "subtitleStreamIndex", "_hls_audio_fallback"} {
		if entries, ok := values[key]; ok {
			for _, value := range entries {
				allowed.Add(key, value)
			}
		}
	}
	return allowed.Encode()
}

func resolvedHLSQuery(rawQuery string, key TranscodeKey) string {
	values, _ := url.ParseQuery(allowedHLSQuery(rawQuery))
	values.Del("AudioStreamIndex")
	values.Del("audioStreamIndex")
	values.Del("_hls_audio_fallback")
	values.Set("AudioStreamIndex", strconv.Itoa(key.AudioStreamIndex))
	if key.AudioStreamIndex < 0 {
		values.Set("_hls_audio_fallback", "1")
	}
	return values.Encode()
}

// ServeHLSSegment writes a single .ts segment from the on-disk cache.
func (s *StreamService) ServeHLSSegment(w http.ResponseWriter, r *http.Request, mediaID, segment string) error {
	key, err := s.hlsKey(r, mediaID)
	if err != nil {
		return err
	}
	s.transcoder.TouchJob(key)
	// Only allow segments that look like seg_NNNNN.ts so we cannot be tricked
	// into reading arbitrary files via path traversal.
	if !strings.HasPrefix(segment, "seg_") || !strings.HasSuffix(segment, ".ts") {
		return errors.New("bad segment")
	}
	full := filepath.Join(s.transcoder.HLSDir(key), segment)
	abs, err := filepath.Abs(full)
	if err != nil {
		return err
	}
	dir, _ := filepath.Abs(s.transcoder.HLSDir(key))
	if !pathWithin(abs, dir) {
		return errors.New("path escape")
	}
	f, err := os.Open(abs) // #nosec G304 -- abs is constrained to the HLS cache directory with pathWithin.
	if err != nil {
		return err
	}
	defer f.Close()
	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return nil
}

func (s *StreamService) hlsKey(r *http.Request, mediaID string) (TranscodeKey, error) {
	selection, err := PlaybackSelectionFromQuery(r.URL.Query())
	if err != nil {
		return TranscodeKey{}, err
	}
	if selection.AudioStreamIndex != nil && *selection.AudioStreamIndex == -1 && r.URL.Query().Get("_hls_audio_fallback") == "1" {
		return TranscodeKey{MediaID: mediaID, AudioStreamIndex: -1}, nil
	}
	var doc *ProbeDocument
	if s.mediaProbe != nil {
		doc, _ = s.mediaProbe.Load(r.Context(), mediaID)
	}
	audioIndex, err := resolveAudioStreamIndex(doc, selection.AudioStreamIndex)
	if err != nil {
		return TranscodeKey{}, err
	}
	return TranscodeKey{MediaID: mediaID, AudioStreamIndex: audioIndex}, nil
}

func (s *StreamService) StopHLS(r *http.Request, mediaID string) error {
	_, explicitAudio := queryValue(r.URL.Query(), "AudioStreamIndex", "audioStreamIndex")
	key, err := s.hlsKey(r, mediaID)
	if err != nil {
		return err
	}
	s.transcoder.StopJob(key)
	if !explicitAudio && key.AudioStreamIndex >= 0 {
		s.transcoder.StopJob(TranscodeKey{MediaID: mediaID, AudioStreamIndex: -1})
	}
	return nil
}
