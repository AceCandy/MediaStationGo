package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// PlaybackInfo returns a PlaybackInfoResponse usable by Emby clients.
func (e *EmbyService) PlaybackInfo(ctx context.Context, mediaID, userID string) (map[string]any, error) {
	return e.PlaybackInfoWithOptions(ctx, mediaID, userID, PlaybackSelection{})
}

func (e *EmbyService) PlaybackInfoWithOptions(ctx context.Context, mediaID, userID string, selection PlaybackSelection) (map[string]any, error) {
	m, siblings, err := e.playableMediaWithSiblings(ctx, mediaID, userID)
	if err != nil || m == nil {
		return nil, err
	}
	if len(siblings) == 0 {
		siblings = []model.MediaView{*m}
	}
	if strings.TrimSpace(selection.MediaSourceID) == "" {
		selection.MediaSourceID = m.ID
	}
	ids := make([]string, 0, len(siblings))
	for i := range siblings {
		ids = append(ids, siblings[i].ID)
	}
	documents := map[string]*ProbeDocument{}
	if e.mediaProbe != nil {
		documents = e.mediaProbe.LoadMany(ctx, ids)
	}
	subtitles := map[string][]SubtitleSelection{}
	for i := range siblings {
		e.ensureTrackMetadataLoaded(&siblings[i].Media, documents[siblings[i].ID])
		if e.subtitle != nil {
			subtitles[siblings[i].ID] = e.subtitle.selectionsForMedia(&siblings[i].Media, documents[siblings[i].ID])
		}
	}
	if err := e.validatePlaybackSelection(siblings, m.ID, selection, documents, subtitles); err != nil {
		return nil, err
	}
	return map[string]any{
		"MediaSources":  e.mediaSourcesFromViewsWithData(ctx, siblings, false, selection, documents, subtitles),
		"PlaySessionId": uuid.NewString(),
		"DateCreated":   formatEmbyDateTime(m.CreatedAt),
	}, nil
}

func (e *EmbyService) validatePlaybackSelection(siblings []model.MediaView, fallbackID string, selection PlaybackSelection, documents map[string]*ProbeDocument, subtitles map[string][]SubtitleSelection) error {
	if err := validatePlaybackSelectionValues(selection); err != nil {
		return err
	}
	targetID := strings.TrimSpace(selection.MediaSourceID)
	if targetID == "" {
		targetID = fallbackID
	}
	found := false
	for i := range siblings {
		if siblings[i].ID == targetID {
			found = true
			break
		}
	}
	if !found {
		return ErrInvalidStreamIndex
	}
	doc := documents[targetID]
	if selection.AudioStreamIndex != nil && *selection.AudioStreamIndex >= 0 {
		if _, err := resolveAudioStreamIndex(doc, selection.AudioStreamIndex); err != nil {
			return err
		}
	}
	if selection.SubtitleStreamIndex != nil && *selection.SubtitleStreamIndex >= 0 &&
		!hasSubtitleSelection(doc, subtitles[targetID], *selection.SubtitleStreamIndex) {
		return ErrInvalidStreamIndex
	}
	return nil
}

func hasSubtitleSelection(doc *ProbeDocument, subtitles []SubtitleSelection, index int) bool {
	if subtitles != nil {
		for _, selection := range subtitles {
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
	if e == nil || m == nil || e.mediaProbe == nil {
		return
	}
	doc, _ := e.mediaProbe.Load(ctx, m.ID)
	e.ensureTrackMetadataLoaded(m, doc)
}

func (e *EmbyService) ensureTrackMetadataLoaded(m *model.Media, doc *ProbeDocument) {
	if e == nil || m == nil || e.mediaProbe == nil || doc != nil || !e.reserveTrackProbe(m.ID) {
		return
	}
	go e.probeTrackMetadata(m.ID)
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

func (e *EmbyService) playableMedia(ctx context.Context, id, userID string) (*model.MediaView, error) {
	m, _, err := e.playableMediaWithSiblings(ctx, id, userID)
	return m, err
}

func (e *EmbyService) playableMediaWithSiblings(ctx context.Context, id, userID string) (*model.MediaView, []model.MediaView, error) {
	views, err := e.mediaViewsForItemID(ctx, id, userID)
	if err != nil {
		return nil, nil, err
	}
	if len(views) > 0 {
		if len(views) == 1 && views[0].ID == id {
			siblings := e.mediaVersionSiblings(ctx, &views[0], userID)
			for i := range siblings {
				if siblings[i].ID == id {
					return &views[0], siblings, nil
				}
			}
			return &views[0], []model.MediaView{views[0]}, nil
		}
		m := e.preferredPlayableView(ctx, userID, views)
		if m.PartGroupKey != "" {
			for i := range views {
				if views[i].PartGroupKey == m.PartGroupKey && views[i].PartIndex > 0 && (m.PartIndex <= 0 || views[i].PartIndex < m.PartIndex) {
					m = &views[i]
				}
			}
		}
		siblings := orderMediaVersionSiblings(views, m.ID)
		for i := range siblings {
			if siblings[i].ID == m.ID {
				return &siblings[i], siblings, nil
			}
		}
		return m, siblings, nil
	}
	if season, ok, err := e.findSeasonGroup(ctx, id, userID); err != nil {
		return nil, nil, err
	} else if ok && len(season.Episodes) > 0 {
		m := e.preferredPlayableView(ctx, userID, season.Episodes)
		return m, e.mediaVersionSiblings(ctx, m, userID), nil
	}
	if series, ok, err := e.findSeriesGroup(ctx, id, userID); err != nil {
		return nil, nil, err
	} else if ok && len(series.Episodes) > 0 {
		m := e.preferredPlayableView(ctx, userID, series.Episodes)
		return m, e.mediaVersionSiblings(ctx, m, userID), nil
	}
	return nil, nil, nil
}

func (e *EmbyService) preferredPlayableView(ctx context.Context, userID string, views []model.MediaView) *model.MediaView {
	if len(views) == 0 {
		return nil
	}
	mediaIDs := make([]string, 0, len(views))
	for _, view := range views {
		mediaIDs = append(mediaIDs, view.ID)
	}
	var history model.PlaybackHistory
	if userID != "" && e.repo.DB.WithContext(ctx).Where("user_id = ? AND media_id IN ?", userID, mediaIDs).
		Order("watched_at DESC").Limit(1).Find(&history).Error == nil && history.ID != "" {
		for i := range views {
			if views[i].ID == history.MediaID {
				return &views[i]
			}
		}
	}
	preferred := 0
	for i := 1; i < len(views); i++ {
		if preferMediaVersion(views[i].Media, views[preferred].Media) {
			preferred = i
		}
	}
	return &views[preferred]
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
	return e.mediaSourceWithSelection(ctx, m, displayName, asEmbedded, doc, PlaybackSelection{}, true, nil)
}

func (e *EmbyService) mediaSourceWithSelection(ctx context.Context, m *model.Media, displayName string, asEmbedded bool, doc *ProbeDocument, selection PlaybackSelection, liveSubtitles bool, subtitles []SubtitleSelection) map[string]any {
	probeContainer := ""
	if doc != nil {
		probeContainer = doc.Format.Name
	}
	container := embyMediaContainer(m, probeContainer)
	isLocalSTRM := localSTRMFileTarget(m) != ""
	isRemote := strings.TrimSpace(m.STRMURL) != "" && !isLocalSTRM
	playURL := embyDirectStreamURL(m.ID, container)
	if strings.TrimSpace(selection.MediaSourceID) == m.ID {
		playURL = appendPlaybackSelection(playURL, selection.AudioStreamIndex, selection.SubtitleStreamIndex)
	}
	src := e.baseMediaSource(ctx, m, displayName, container, isRemote, playURL, doc, liveSubtitles, subtitles)
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

func (e *EmbyService) baseMediaSource(ctx context.Context, m *model.Media, displayName, container string, isRemote bool, playURL string, doc *ProbeDocument, liveSubtitles bool, subtitles []SubtitleSelection) map[string]any {
	if strings.TrimSpace(displayName) == "" {
		displayName = m.Title
	}
	size := int64(0)
	runTimeTicks := int64(0)
	if doc != nil && doc.Format.Size > 0 {
		size = doc.Format.Size
	}
	if doc != nil && doc.Format.Duration > 0 {
		runTimeTicks = int64(math.Round(doc.Format.Duration * 10_000_000))
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
		"RunTimeTicks":          runTimeTicks,
		"MediaStreams":          e.mediaStreams(ctx, m, doc, liveSubtitles, subtitles),
	}
	if doc != nil && doc.Format.BitRate > 0 {
		src["Bitrate"] = doc.Format.BitRate
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

func embyMediaContainer(m *model.Media, probeContainer string) string {
	container := strings.Trim(strings.ToLower(probeContainer), ". ")
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
	target := normalizeSTRMHTTPURL(raw)
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
