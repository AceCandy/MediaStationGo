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
	siblings := e.mediaVersionSiblings(ctx, m)
	if len(siblings) == 0 {
		siblings = []model.MediaView{*m}
	}
	for i := range siblings {
		e.ensureTrackMetadata(ctx, &siblings[i].Media)
	}
	if err := e.validatePlaybackSelection(ctx, siblings, m.ID, selection); err != nil {
		return nil, err
	}
	return map[string]any{
		"MediaSources":  e.mediaSourcesFromViewsWithSelection(ctx, siblings, false, e.directPlayOnly(ctx), selection),
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

// ensureTrackMetadata 在后台补齐云盘或本地 STRM 媒体的轨道元数据。
//
// 注意必须是异步的：此前这里在 PlaybackInfo 请求路径上同步执行
// CloudResolve + ffprobe(HTTP)（最长 8 秒），既把第三方播放器的起播时间
// 拖长到秒级，又让每一次点开详情/起播都可能触发一次云盘数据下载，是
// Docker 部署下 CPU/带宽长期居高的来源之一。探测结果落库后，下一次
// 请求自然能读到完整元数据。
func (e *EmbyService) ensureTrackMetadata(ctx context.Context, m *model.Media) {
	if e != nil && m != nil && e.mediaProbe != nil {
		if !e.mediaProbe.NeedsProbe(ctx, m.ID) || !e.reserveTrackProbe(m.ID) {
			return
		}
		go e.probeTrackMetadata(m.ID)
		return
	}
	if e == nil || m == nil || e.probe == nil || !mediaTrackMetadataMissing(m) {
		return
	}
	if target := localSTRMFileTarget(m); target != "" {
		if e.reserveTrackProbe(m.ID) {
			go e.probeLocalSTRMTrackMetadata(m.ID, target)
		}
		return
	}
	if e.storage == nil {
		return
	}
	typ, ref, ok := parseCloudMediaPlaybackURL(m.STRMURL)
	if !ok || !e.reserveTrackProbe(m.ID) {
		return
	}
	go e.probeCloudTrackMetadata(m.ID, typ, ref)
}

func (e *EmbyService) probeTrackMetadata(mediaID string) {
	defer e.releaseTrackProbe(mediaID)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := e.mediaProbe.ProbeMedia(ctx, mediaID); err != nil && e.log != nil && !errors.Is(err, ErrMediaProbeSourceChanged) {
		e.log.Debug("playback media probe failed", zap.String("media_id", mediaID), zap.Error(err))
	}
}

func (e *EmbyService) probeCloudTrackMetadata(mediaID, typ, ref string) {
	defer e.releaseTrackProbe(mediaID)
	probeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	link, err := e.storage.CloudResolve(probeCtx, typ, ref, "")
	if err != nil {
		if e.log != nil {
			e.log.Debug("resolve cloud media for playback probe failed", zap.String("media_id", mediaID), zap.Error(err))
		}
		return
	}
	probe, err := e.probe.ProbeHTTP(probeCtx, link.URL, link.Headers)
	cancel()
	if err != nil {
		if e.log != nil {
			e.log.Debug("playback cloud ffprobe failed", zap.String("media_id", mediaID), zap.Error(err))
		}
		return
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	current, err := e.repo.Media.FindByID(writeCtx, mediaID)
	if err != nil || current == nil {
		return
	}
	currentType, currentRef, currentOK := parseCloudMediaPlaybackURL(current.STRMURL)
	if !currentOK || currentType != typ || currentRef != ref {
		return
	}
	e.persistTrackMetadata(writeCtx, mediaID, probeResultUpdates(probe))
}

func (e *EmbyService) probeLocalSTRMTrackMetadata(mediaID, target string) {
	defer e.releaseTrackProbe(mediaID)
	probeCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	probe, err := e.probe.Probe(probeCtx, target)
	cancel()
	if err != nil {
		if e.log != nil {
			e.log.Debug("playback local strm ffprobe failed", zap.String("media_id", mediaID), zap.Error(err))
		}
		return
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	current, err := e.repo.Media.FindByID(writeCtx, mediaID)
	if err != nil || current == nil || localSTRMFileTarget(current) != target {
		return
	}
	e.persistTrackMetadata(writeCtx, mediaID, localProbeResultUpdates(probe, target))
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

func parseCloudMediaPlaybackURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	const prefix = "api/cloud/play/"
	idx := strings.Index(strings.ToLower(path), prefix)
	if idx < 0 {
		return "", "", false
	}
	typ := strings.TrimSpace(path[idx+len(prefix):])
	ref := strings.TrimSpace(u.Query().Get("ref"))
	return typ, ref, typ != "" && ref != ""
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

// directPlayOnly reports whether the admin enabled「客户端直连解码」mode.
// In that mode the host never transcodes; clients must direct-play.
func (e *EmbyService) directPlayOnly(ctx context.Context) bool {
	if e.repo == nil || e.repo.Setting == nil {
		return false
	}
	v, err := e.repo.Setting.Get(ctx, PlaybackDirectOnlySettingKey)
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
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
func (e *EmbyService) mediaSource(ctx context.Context, m *model.Media, displayName string, asEmbedded, directOnly bool) map[string]any {
	var doc *ProbeDocument
	if e.mediaProbe != nil {
		doc, _ = e.mediaProbe.Load(ctx, m.ID)
	}
	return e.mediaSourceWithProbe(ctx, m, displayName, asEmbedded, directOnly, doc)
}

func (e *EmbyService) mediaSourceWithProbe(ctx context.Context, m *model.Media, displayName string, asEmbedded, directOnly bool, doc *ProbeDocument) map[string]any {
	return e.mediaSourceWithSelection(ctx, m, displayName, asEmbedded, directOnly, doc, PlaybackSelection{}, true)
}

func (e *EmbyService) mediaSourceWithSelection(ctx context.Context, m *model.Media, displayName string, asEmbedded, directOnly bool, doc *ProbeDocument, selection PlaybackSelection, liveSubtitles bool) map[string]any {
	container := embyMediaContainer(m)
	isLocalSTRM := localSTRMFileTarget(m) != ""
	isCloud := strings.TrimSpace(m.STRMURL) != "" && !isLocalSTRM
	playURL := e.embyMediaPlayURL(ctx, m, container, isCloud)
	if isCloud {
		// Cloud/WebDAV media is already a direct/proxy stream. Advertising HLS
		// transcoding makes some Emby clients pick /master.m3u8, forcing this
		// lightweight server to pull remote bytes through ffmpeg and often
		// surfacing as "network/playback failed". Keep cloud media direct-only.
		directOnly = true
	}
	src := e.baseMediaSource(ctx, m, displayName, container, isCloud, playURL, directOnly, doc, liveSubtitles)
	if !asEmbedded && playURL != "" {
		src["DirectStreamUrl"] = playURL
		// 直连解码模式下不下发 TranscodingUrl，迫使客户端本地解码直连，
		// 宿主机不参与转码。
		if !directOnly {
			transcodingURL := "/Videos/" + m.ID + "/master.m3u8"
			requested := selection.AudioStreamIndex
			if selection.MediaSourceID != "" && selection.MediaSourceID != m.ID {
				requested = nil
			}
			if audioIndex, err := resolveAudioStreamIndex(doc, requested); err == nil && audioIndex >= 0 {
				transcodingURL = appendPlaybackSelection(transcodingURL, audioIndex, selection.SubtitleStreamIndex)
			}
			src["TranscodingUrl"] = transcodingURL
		}
	}
	if (isCloud || isLocalSTRM) && playURL != "" {
		// Never expose the backing .strm text path. Cloud STRM sources use the
		// configured token-aware endpoint; local STRM sources use /Videos.
		src["IsRemote"] = isCloud
		src["Path"] = embyMediaSourcePath(m, playURL, isLocalSTRM, isCloud)
	}
	return src
}

func appendPlaybackSelection(raw string, audioIndex int, subtitleIndex *int) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("AudioStreamIndex", fmt.Sprintf("%d", audioIndex))
	if subtitleIndex != nil {
		q.Set("SubtitleStreamIndex", fmt.Sprintf("%d", *subtitleIndex))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (e *EmbyService) baseMediaSource(ctx context.Context, m *model.Media, displayName, container string, isCloud bool, playURL string, directOnly bool, doc *ProbeDocument, liveSubtitles bool) map[string]any {
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
		"IsRemote":              isCloud,
		"RequiresOpening":       false,
		"RequiresClosing":       false,
		"ReadAtNativeFramerate": false,
		"SupportsTranscoding":   !directOnly,
		"SupportsDirectStream":  !isCloud || playURL != "",
		"SupportsDirectPlay":    !isCloud || playURL != "",
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

func embyMediaSourcePath(m *model.Media, playURL string, isLocalSTRM, isCloud bool) string {
	if m == nil {
		return ""
	}
	if (isCloud || isLocalSTRM) && strings.TrimSpace(playURL) != "" {
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
	if _, ref, ok := parseCloudMediaPlaybackURL(target); ok {
		target = ref
	} else {
		u, err := url.Parse(target)
		if err != nil {
			return ""
		}
		target = u.Path
	}
	ext := strings.ToLower(filepath.Ext(target))
	if ext == ".strm" {
		return ""
	}
	if _, ok := videoExtensions[ext]; !ok {
		return ""
	}
	return strings.TrimPrefix(ext, ".")
}

func (e *EmbyService) embyMediaPlayURL(ctx context.Context, m *model.Media, container string, isCloud bool) string {
	if !isCloud {
		return embyDirectStreamURL(m.ID, container)
	}
	switch CloudPlaybackMode(ctx, e.repo) {
	case CloudPlaybackModeSTRM:
		return embySTRMStreamURL(m.ID)
	case CloudPlaybackModeRedirectProxy:
		return embyDirectStreamURL(m.ID, container)
	default:
		return ""
	}
}
