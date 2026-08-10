package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// PlaybackInfo returns a PlaybackInfoResponse usable by Emby clients.
func (e *EmbyService) PlaybackInfo(ctx context.Context, mediaID, userID string) (map[string]any, error) {
	return e.PlaybackInfoWithOptions(ctx, mediaID, userID, PlaybackSelection{})
}

func (e *EmbyService) PlaybackInfoWithOptions(ctx context.Context, mediaID, userID string, selection PlaybackSelection) (map[string]any, error) {
	m, err := e.playableMedia(ctx, mediaID, userID)
	if err != nil || m == nil {
		return nil, err
	}
	siblings := e.mediaVersionSiblings(ctx, m, userID)
	if len(siblings) == 0 {
		siblings = []model.MediaView{*m}
	}
	if strings.TrimSpace(selection.MediaSourceID) == "" {
		selection.MediaSourceID = m.ID
	}
	for i := range siblings {
		e.ensureTrackMetadata(ctx, &siblings[i].Media)
	}
	if err := e.validatePlaybackSelection(ctx, siblings, m.ID, selection); err != nil {
		return nil, err
	}
	return map[string]any{
		"MediaSources":  e.mediaSourcesFromViewsWithSelection(ctx, siblings, false, selection),
		"PlaySessionId": fmt.Sprintf("%s-%d", m.ID, time.Now().Unix()),
		"DateCreated":   formatEmbyDateTime(m.CreatedAt),
	}, nil
}

func (e *EmbyService) validatePlaybackSelection(ctx context.Context, siblings []model.MediaView, fallbackID string, selection PlaybackSelection) error {
	if err := validatePlaybackSelectionValues(selection); err != nil {
		return err
	}
	targetID := strings.TrimSpace(selection.MediaSourceID)
	if targetID == "" {
		targetID = fallbackID
	}
	var target *model.Media
	for i := range siblings {
		if siblings[i].ID == targetID {
			target = &siblings[i].Media
			break
		}
	}
	if target == nil {
		return ErrInvalidStreamIndex
	}
	var doc *ProbeDocument
	if e.mediaProbe != nil {
		doc, _ = e.mediaProbe.Load(ctx, targetID)
	}
	if selection.AudioStreamIndex != nil && *selection.AudioStreamIndex >= 0 {
		if _, err := resolveAudioStreamIndex(doc, selection.AudioStreamIndex); err != nil {
			return err
		}
	}
	if selection.SubtitleStreamIndex != nil && *selection.SubtitleStreamIndex >= 0 &&
		!e.hasSubtitleSelection(ctx, target, doc, *selection.SubtitleStreamIndex) {
		return ErrInvalidStreamIndex
	}
	return nil
}

func (e *EmbyService) hasSubtitleSelection(ctx context.Context, media *model.Media, doc *ProbeDocument, index int) bool {
	if e.subtitle != nil {
		for _, selection := range e.subtitle.Selections(ctx, media.ID, doc) {
			if selection.Index == index {
				return true
			}
		}
		return false
	}
	if doc != nil {
		for _, stream := range doc.Streams {
			if stream.Index == index && stream.CodecType == "subtitle" {
				return true
			}
		}
	}
	return false
}

// ensureTrackMetadata 在后台补齐本地、HTTP/HTTPS 或 STRM 媒体的轨道元数据。
func (e *EmbyService) ensureTrackMetadata(ctx context.Context, m *model.Media) {
	if e != nil && m != nil && e.mediaProbe != nil {
		if !e.mediaProbe.NeedsProbe(ctx, m.ID) || !e.reserveTrackProbe(m.ID) {
			return
		}
		go e.probeTrackMetadata(m.ID)
	}
}

func (e *EmbyService) probeTrackMetadata(mediaID string) {
	defer e.releaseTrackProbe(mediaID)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := e.mediaProbe.ProbeMedia(ctx, mediaID); err != nil && e.log != nil && !errors.Is(err, ErrMediaProbeSourceChanged) {
		e.log.Debug("playback media probe failed", zap.String("media_id", mediaID), zap.Error(err))
	}
}

func (e *EmbyService) reserveTrackProbe(mediaID string) bool {
	e.trackProbeMu.Lock()
	defer e.trackProbeMu.Unlock()
	if e.trackProbeInFlight == nil {
		e.trackProbeInFlight = make(map[string]struct{})
	}
	if _, busy := e.trackProbeInFlight[mediaID]; busy {
		return false
	}
	e.trackProbeInFlight[mediaID] = struct{}{}
	return true
}

func (e *EmbyService) releaseTrackProbe(mediaID string) {
	e.trackProbeMu.Lock()
	delete(e.trackProbeInFlight, mediaID)
	e.trackProbeMu.Unlock()
}

func (e *EmbyService) persistTrackMetadata(ctx context.Context, mediaID string, updates map[string]any) {
	if len(updates) == 0 {
		return
	}
	if err := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", mediaID).Updates(updates).Error; err != nil && e.log != nil {
		e.log.Debug("persist playback track probe failed", zap.String("media_id", mediaID), zap.Error(err))
	}
}

func mediaTrackMetadataMissing(m *model.Media) bool {
	return m.DurationSec <= 0 ||
		m.Width <= 0 ||
		m.Height <= 0 ||
		strings.TrimSpace(m.VideoCodec) == "" ||
		strings.TrimSpace(m.AudioCodec) == ""
}

func applyProbeResultToMediaValue(m *model.Media, probe *ProbeResult) {
	if m == nil || probe == nil {
		return
	}
	if probe.DurationSec > 0 {
		m.DurationSec = probe.DurationSec
	}
	if probe.Width > 0 {
		m.Width = probe.Width
	}
	if probe.Height > 0 {
		m.Height = probe.Height
	}
	if strings.TrimSpace(probe.VideoCodec) != "" {
		m.VideoCodec = probe.VideoCodec
	}
	if strings.TrimSpace(probe.AudioCodec) != "" {
		m.AudioCodec = probe.AudioCodec
	}
	if strings.TrimSpace(probe.Container) != "" {
		m.Container = probe.Container
	}
}

func (e *EmbyService) playableMedia(ctx context.Context, id, userID string) (*model.MediaView, error) {
	if season, ok, err := e.findSeasonGroup(ctx, id, userID); err != nil {
		return nil, err
	} else if ok && len(season.Episodes) > 0 {
		return &season.Episodes[0], nil
	}
	if series, ok, err := e.findSeriesGroup(ctx, id, userID); err != nil {
		return nil, err
	} else if ok && len(series.Episodes) > 0 {
		return &series.Episodes[0], nil
	}
	return e.mediaViewForItemID(ctx, id, userID)
}

// mediaSource 是 /Items 与 /PlaybackInfo 共享的 MediaSource 结构。
//
// asEmbedded=true：嵌在 /Items 列表里，不包含完整 stream URL（避免暴露
// 直链给搜索接口）。/PlaybackInfo 走 false 路径，URL 指向 Emby 兼容
// /Videos/{id}/stream（客户端会继续携带 X-Emby-Token 或 append api_key）。
func (e *EmbyService) mediaSource(ctx context.Context, m *model.Media, displayName string, asEmbedded bool) map[string]any {
	var doc *ProbeDocument
	if e.mediaProbe != nil {
		doc, _ = e.mediaProbe.Load(ctx, m.ID)
	}
	return e.mediaSourceWithProbe(ctx, m, displayName, asEmbedded, doc)
}

func (e *EmbyService) mediaSourceWithProbe(ctx context.Context, m *model.Media, displayName string, asEmbedded bool, doc *ProbeDocument) map[string]any {
	return e.mediaSourceWithSelection(ctx, m, displayName, asEmbedded, doc, PlaybackSelection{}, true)
}

func (e *EmbyService) mediaSourceWithSelection(ctx context.Context, m *model.Media, displayName string, asEmbedded bool, doc *ProbeDocument, selection PlaybackSelection, liveSubtitles bool) map[string]any {
	container := embyMediaContainer(m)
	isLocalSTRM := localSTRMFileTarget(m) != ""
	isRemote := strings.TrimSpace(m.STRMURL) != "" && !isLocalSTRM
	playURL := embyDirectStreamURL(m.ID, container)
	if strings.TrimSpace(selection.MediaSourceID) == m.ID {
		playURL = appendPlaybackSelection(playURL, selection.AudioStreamIndex, selection.SubtitleStreamIndex)
	}
	src := e.baseMediaSource(ctx, m, displayName, container, isRemote, playURL, doc, liveSubtitles)
	if !asEmbedded && playURL != "" {
		src["DirectStreamUrl"] = playURL
	}
	if (isRemote || isLocalSTRM) && playURL != "" {
		src["IsRemote"] = isRemote
		src["Path"] = embyMediaSourcePath(m, playURL, isLocalSTRM, isRemote)
	}
	return src
}

func appendPlaybackSelection(raw string, audioIndex, subtitleIndex *int) string {
	if audioIndex == nil && subtitleIndex == nil {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if audioIndex != nil {
		q.Set("AudioStreamIndex", fmt.Sprintf("%d", *audioIndex))
	}
	if subtitleIndex != nil {
		q.Set("SubtitleStreamIndex", fmt.Sprintf("%d", *subtitleIndex))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (e *EmbyService) baseMediaSource(ctx context.Context, m *model.Media, displayName, container string, isRemote bool, playURL string, doc *ProbeDocument, liveSubtitles bool) map[string]any {
	if strings.TrimSpace(displayName) == "" {
		displayName = m.Title
	}
	size := m.SizeBytes
	if doc != nil && doc.Format.Size > 0 {
		size = doc.Format.Size
	}
	src := map[string]any{
		"Id":                    m.ID,
		"Name":                  displayName,
		"Path":                  m.Path,
		"Container":             container,
		"Size":                  size,
		"DateCreated":           formatEmbyDateTime(m.CreatedAt),
		"Protocol":              "Http",
		"Type":                  "Default",
		"IsRemote":              isRemote,
		"RequiresOpening":       false,
		"RequiresClosing":       false,
		"ReadAtNativeFramerate": false,
		"SupportsTranscoding":   false,
		"SupportsDirectStream":  !isRemote || playURL != "",
		"SupportsDirectPlay":    !isRemote || playURL != "",
		"SupportsProbing":       true,
		"RunTimeTicks":          int64(m.DurationSec) * 10_000_000,
		"MediaStreams":          e.mediaStreams(ctx, m, doc, liveSubtitles),
	}
	if doc != nil && doc.Format.BitRate > 0 {
		src["Bitrate"] = doc.Format.BitRate
	}
	if bitrate := embyAverageBitrate(m); bitrate > 0 {
		if _, ok := src["Bitrate"]; !ok {
			src["Bitrate"] = bitrate
		}
	}
	return src
}

func embyMediaSourcePath(m *model.Media, playURL string, isLocalSTRM, isRemote bool) string {
	if m == nil {
		return ""
	}
	if (isRemote || isLocalSTRM) && strings.TrimSpace(playURL) != "" {
		return playURL
	}
	return m.Path
}

func embyAverageBitrate(m *model.Media) int64 {
	if m == nil || m.SizeBytes <= 0 || m.DurationSec <= 0 {
		return 0
	}
	return m.SizeBytes * 8 / int64(m.DurationSec)
}

func embyMediaContainer(m *model.Media) string {
	container := strings.Trim(strings.ToLower(m.Container), ". ")
	if target := localSTRMFileTarget(m); target != "" {
		if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(target)), "."); ext != "" {
			return ext
		}
	}
	if target := embyRemoteSTRMContainer(m.STRMURL); target != "" {
		return target
	}
	if container == "" {
		container = strings.TrimPrefix(strings.ToLower(filepath.Ext(m.Path)), ".")
	}
	if container == "" && strings.TrimSpace(m.STRMURL) != "" {
		return "strm"
	}
	return container
}

func embyRemoteSTRMContainer(raw string) string {
	target := strings.TrimSpace(raw)
	if target == "" {
		return ""
	}
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	target = u.Path
	ext := strings.ToLower(filepath.Ext(target))
	if ext == ".strm" {
		return ""
	}
	if _, ok := videoExtensions[ext]; !ok {
		return ""
	}
	return strings.TrimPrefix(ext, ".")
}
