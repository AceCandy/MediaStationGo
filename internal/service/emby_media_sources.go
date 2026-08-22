package service

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (e *EmbyService) mediaSourcesForView(ctx context.Context, m *model.MediaView, userID string, asEmbedded, completeStreams bool) []map[string]any {
	siblings := e.mediaVersionSiblings(ctx, m, userID)
	if len(siblings) == 0 {
		siblings = []model.MediaView{*m}
	}
	return e.mediaSourcesForViews(ctx, siblings, asEmbedded, completeStreams)
}

func (e *EmbyService) mediaSourcesForViews(ctx context.Context, siblings []model.MediaView, asEmbedded, completeStreams bool) []map[string]any {
	if completeStreams {
		for i := range siblings {
			e.ensureTrackMetadata(ctx, &siblings[i].Media)
		}
		return e.mediaSourcesFromViews(ctx, siblings, asEmbedded)
	}
	sources := make([]map[string]any, 0, len(siblings))
	for i := range siblings {
		name := embyMediaVersionName(&siblings[i].Media, siblings[i].Title)
		sources = append(sources, e.mediaSourceWithSelection(ctx, &siblings[i].Media, name, asEmbedded, nil, PlaybackSelection{}, false, nil))
	}
	return sources
}

func (e *EmbyService) mediaSourcesFromViews(ctx context.Context, siblings []model.MediaView, asEmbedded bool) []map[string]any {
	return e.mediaSourcesFromViewsWithSelection(ctx, siblings, asEmbedded, PlaybackSelection{})
}

func (e *EmbyService) mediaSourcesFromViewsWithSelection(ctx context.Context, siblings []model.MediaView, asEmbedded bool, selection PlaybackSelection) []map[string]any {
	ids := make([]string, 0, len(siblings))
	for i := range siblings {
		ids = append(ids, siblings[i].ID)
	}
	documents := map[string]*ProbeDocument{}
	if e.mediaProbe != nil {
		documents = e.mediaProbe.LoadMany(ctx, ids)
	}
	return e.mediaSourcesFromViewsWithData(ctx, siblings, asEmbedded, selection, documents, nil)
}

func (e *EmbyService) mediaSourcesFromViewsWithData(ctx context.Context, siblings []model.MediaView, asEmbedded bool, selection PlaybackSelection, documents map[string]*ProbeDocument, subtitles map[string][]SubtitleSelection) []map[string]any {
	sources := make([]map[string]any, 0, len(siblings))
	for i := range siblings {
		name := embyMediaVersionName(&siblings[i].Media, siblings[i].Title)
		sources = append(sources, e.mediaSourceWithSelection(ctx, &siblings[i].Media, name, asEmbedded, documents[siblings[i].ID], selection, true, subtitles[siblings[i].ID]))
	}
	return sources
}

var (
	embyVersionBoundaryRE = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(\{])((?:\d{3,4}p|\d{2,3}fps|4k|8k|uhd|fhd|ds4k|blu[._-]?ray|b[dr]rip|web[._-]?dl|web[._-]?rip|hdtv|remux|dvd[._-]?rip|hdr10?|dovi|sdr|hevc|avc|av1|vvc|[hx][._-]?26[456]|ddp?\d*|eac3|truehd|dts|aac\d*|flac|atmos))(?:$|[\s._\-\]\)\}])`)
	embyNameSeparatorRE   = regexp.MustCompile(`[\s._\-:：·]+`)
)

func embyMediaVersionName(m *model.Media, fallback string) string {
	if m == nil {
		return strings.TrimSpace(fallback)
	}
	source := m.Path
	if target := localSTRMFileTarget(m); target != "" {
		source = target
	}
	base := pathBaseSlash(source)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if match := embyVersionBoundaryRE.FindStringSubmatchIndex(name); len(match) >= 4 {
		return cleanEmbyVersionName(name[match[2]:], fallback)
	}
	return cleanEmbyVersionName(name, fallback, m.Title, m.OriginalName)
}

func cleanEmbyVersionName(name, fallback string, titles ...string) string {
	name = yearPattern.ReplaceAllString(name, " ")
	for _, pattern := range []*regexp.Regexp{patSEnE, patDanglingSE, patNxE, patEP, patCNRange, patCN, patSeasonOnly, patCNSeason} {
		name = pattern.ReplaceAllString(name, " ")
	}
	for _, title := range append(titles, fallback) {
		parts := embyNameSeparatorRE.Split(strings.TrimSpace(title), -1)
		kept := parts[:0]
		for _, part := range parts {
			if part != "" {
				kept = append(kept, regexp.QuoteMeta(part))
			}
		}
		if len(kept) == 0 {
			continue
		}
		pattern := regexp.MustCompile(`(?i)(?:^|[\s._\-:：·]+)` + strings.Join(kept, `[\s._\-:：·]+`) + `(?:$|[\s._\-:：·]+)`)
		name = pattern.ReplaceAllString(name, " ")
	}
	name = strings.Trim(name, " ._-:：·[](){}")
	if name == "" {
		return "默认版本"
	}
	return name
}

func (e *EmbyService) mediaVersionSiblings(ctx context.Context, m *model.MediaView, userID string) []model.MediaView {
	if e == nil || e.repo == nil || e.repo.DB == nil || m == nil || strings.TrimSpace(m.ID) == "" {
		return nil
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{})
	if strings.TrimSpace(m.MetadataID) != "" {
		q = q.Where("media.metadata_id = ?", m.MetadataID)
	} else {
		libraryIDs := e.mergedLibraryIDs(ctx, m.LibraryID)
		if len(libraryIDs) == 0 {
			libraryIDs = []string{m.LibraryID}
		}
		q = q.Where("media.library_id IN ?", libraryIDs).
			Where("media.season_num = ? AND media.episode_num = ?", m.SeasonNum, m.EpisodeNum)
		title := strings.TrimSpace(m.Title)
		if title == "" {
			title = strings.TrimSpace(m.OriginalName)
		}
		if title == "" {
			return []model.MediaView{*m}
		}
		q = q.Where("LOWER(media.scan_title) = ?", strings.ToLower(title))
		if m.Year > 0 {
			q = q.Where("media.scan_year = ?", m.Year)
		}
	}
	var rows []model.Media
	if err := q.Find(&rows).Error; err != nil || len(rows) == 0 {
		return []model.MediaView{*m}
	}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	views, err := e.repo.MediaView.FindByIDs(ctx, ids, e.mediaQueryFilter(ctx, userID))
	if err != nil || len(views) == 0 {
		return []model.MediaView{*m}
	}
	return orderMediaVersionSiblings(views, m.ID)
}

func orderMediaVersionSiblings(views []model.MediaView, currentID string) []model.MediaView {
	views = collapseExactPathViews(views)
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].ID == currentID {
			return true
		}
		if views[j].ID == currentID {
			return false
		}
		return preferMediaVersion(views[i].Media, views[j].Media)
	})
	return views
}

func collapseExactPathViews(rows []model.MediaView) []model.MediaView {
	if len(rows) < 2 {
		return rows
	}
	out := rows[:0]
	seen := map[string]struct{}{}
	for _, row := range rows {
		path := strings.TrimSpace(row.Path)
		if path != "" {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
		}
		out = append(out, row)
	}
	return out
}

func (e *EmbyService) mediaVersionKey(ctx context.Context, m *model.MediaView) string {
	if e == nil || m == nil {
		return ""
	}
	ids := e.mergedLibraryIDs(ctx, m.LibraryID)
	sort.Strings(ids)
	libraryGroup := strings.Join(ids, ",")
	if libraryGroup == "" {
		libraryGroup = strings.TrimSpace(m.LibraryID)
	}
	if strings.TrimSpace(m.MetadataID) != "" {
		return fmt.Sprintf("%s|metadata:%s", libraryGroup, strings.TrimSpace(m.MetadataID))
	}
	if m.TMDbID > 0 {
		return fmt.Sprintf("%s|tmdb:%d|s:%d|e:%d", libraryGroup, m.TMDbID, m.SeasonNum, m.EpisodeNum)
	}
	if m.BangumiID > 0 {
		return fmt.Sprintf("%s|bangumi:%d|s:%d|e:%d", libraryGroup, m.BangumiID, m.SeasonNum, m.EpisodeNum)
	}
	title := strings.ToLower(strings.TrimSpace(m.Title))
	if title == "" {
		title = strings.ToLower(strings.TrimSpace(m.OriginalName))
	}
	if title == "" {
		return ""
	}
	return fmt.Sprintf("%s|title:%s|y:%d|s:%d|e:%d", libraryGroup, title, m.Year, m.SeasonNum, m.EpisodeNum)
}

func preferMediaVersion(candidate, current model.Media) bool {
	candidateRemote := strings.TrimSpace(candidate.STRMURL) != ""
	currentRemote := strings.TrimSpace(current.STRMURL) != ""
	if candidateRemote != currentRemote {
		return !candidateRemote
	}
	if candidate.Width != current.Width {
		return candidate.Width > current.Width
	}
	if candidate.SizeBytes != current.SizeBytes {
		return candidate.SizeBytes > current.SizeBytes
	}
	return candidate.CreatedAt.After(current.CreatedAt)
}

func embySTRMStreamURL(mediaID string) string {
	return "/api/stream/" + url.PathEscape(strings.TrimSpace(mediaID))
}

func embyDirectStreamURL(mediaID, container string) string {
	mediaID = strings.TrimSpace(mediaID)
	container = strings.Trim(strings.ToLower(container), ". ")
	if container == "" || container == "strm" {
		return "/Videos/" + mediaID + "/stream"
	}
	return "/Videos/" + mediaID + "/stream." + container
}

func (e *EmbyService) mediaStreams(ctx context.Context, m *model.Media, doc *ProbeDocument, liveSubtitles bool, subtitles []SubtitleSelection) []map[string]any {
	if !liveSubtitles {
		subtitles = nil
	} else if e.subtitle != nil && subtitles == nil {
		subtitles = e.subtitle.Selections(ctx, m.ID, doc)
	}
	if doc == nil {
		return e.scalarMediaStreams(m, subtitles)
	}
	streams := []map[string]any{}
	for _, stream := range doc.Streams {
		if stream.CodecType == "subtitle" {
			continue
		}
		mapped := mapProbeStream(stream)
		if mapped != nil {
			streams = append(streams, mapped)
		}
	}
	for _, subtitle := range subtitles {
		streams = append(streams, mapSubtitleSelection(m.ID, subtitle))
	}
	if len(streams) == 0 {
		return e.scalarMediaStreams(m, subtitles)
	}
	return streams
}

func (e *EmbyService) scalarMediaStreams(m *model.Media, subtitles []SubtitleSelection) []map[string]any {
	streams := []map[string]any{}
	if m.VideoCodec != "" || m.Width > 0 {
		streams = append(streams, map[string]any{
			"Codec":        m.VideoCodec,
			"Type":         "Video",
			"Index":        0,
			"Width":        m.Width,
			"Height":       m.Height,
			"AspectRatio":  "",
			"IsDefault":    true,
			"IsForced":     false,
			"IsExternal":   false,
			"DisplayTitle": fmt.Sprintf("%dx%d %s", m.Width, m.Height, m.VideoCodec),
		})
	}
	if m.AudioCodec != "" {
		streams = append(streams, map[string]any{
			"Codec":      m.AudioCodec,
			"Type":       "Audio",
			"Index":      1,
			"IsDefault":  true,
			"IsForced":   false,
			"IsExternal": false,
		})
	}
	for _, subtitle := range subtitles {
		streams = append(streams, mapSubtitleSelection(m.ID, subtitle))
	}
	if len(streams) == 0 {
		streams = append(streams, map[string]any{
			"Codec":        "unknown",
			"Type":         "Video",
			"Index":        0,
			"IsDefault":    true,
			"IsForced":     false,
			"IsExternal":   false,
			"DisplayTitle": "Video",
		})
	}
	return streams
}

func mapProbeStream(stream ProbeStream) map[string]any {
	track, ok := projectProbeTrack(stream)
	if !ok || track.Type == "subtitle" {
		return nil
	}
	mapped := map[string]any{
		"Codec": track.Codec, "Type": strings.ToUpper(track.Type[:1]) + track.Type[1:], "Index": track.Index,
		"IsDefault": track.IsDefault, "IsForced": track.IsForced,
		"IsExternal": false, "Protocol": "File",
		"IsTextSubtitleStream": false, "SupportsExternalStream": false,
	}
	setStringValue(mapped, "Language", track.Language)
	setStringValue(mapped, "DisplayLanguage", track.DisplayLanguage)
	setStringValue(mapped, "Title", track.Title)
	setStringValue(mapped, "DisplayTitle", track.DisplayTitle)
	setStringValue(mapped, "Profile", track.Profile)
	setStringValue(mapped, "TimeBase", track.TimeBase)
	if track.Level > 0 {
		mapped["Level"] = track.Level
	}
	if track.BitRate > 0 {
		mapped["BitRate"] = track.BitRate
	}
	if track.IsHearingImpaired {
		mapped["IsHearingImpaired"] = true
	}
	if track.IsVisualImpaired {
		mapped["IsVisualImpaired"] = true
	}
	if track.Type == "video" {
		if track.Width > 0 {
			mapped["Width"] = track.Width
		}
		if track.Height > 0 {
			mapped["Height"] = track.Height
		}
		setStringValue(mapped, "AspectRatio", track.AspectRatio)
		setStringValue(mapped, "PixelFormat", track.PixelFormat)
		setStringValue(mapped, "ColorRange", track.ColorRange)
		setStringValue(mapped, "ColorSpace", track.ColorSpace)
		setStringValue(mapped, "ColorTransfer", track.ColorTransfer)
		setStringValue(mapped, "ColorPrimaries", track.ColorPrimaries)
		if track.BitDepth > 0 {
			mapped["BitDepth"] = track.BitDepth
		}
		setStringValue(mapped, "VideoRange", track.VideoRange)
		if track.AverageFrameRate > 0 {
			mapped["AverageFrameRate"] = track.AverageFrameRate
		}
		if track.RealFrameRate > 0 {
			mapped["RealFrameRate"] = track.RealFrameRate
		}
		if videoType, videoSubType, description := probeExtendedVideoType(track.VideoRange); videoType != "" {
			mapped["ExtendedVideoType"] = videoType
			mapped["ExtendedVideoSubType"] = videoSubType
			mapped["ExtendedVideoSubTypeDescription"] = description
		}
	} else {
		if track.Channels > 0 {
			mapped["Channels"] = track.Channels
		}
		if track.SampleRate > 0 {
			mapped["SampleRate"] = track.SampleRate
		}
		setStringValue(mapped, "ChannelLayout", track.ChannelLayout)
		setStringValue(mapped, "SampleFormat", track.SampleFormat)
		if track.BitsPerSample > 0 {
			mapped["BitsPerSample"] = track.BitsPerSample
		}
	}
	return mapped
}

func mapSubtitleSelection(mediaID string, subtitle SubtitleSelection) map[string]any {
	mapped := map[string]any{
		"Codec": subtitle.Codec, "Type": "Subtitle", "Language": subtitle.Language,
		"DisplayLanguage": probeDisplayLanguage(subtitle.Language), "Title": subtitle.Title,
		"DisplayTitle": subtitleDisplayTitle(subtitle), "Index": subtitle.Index,
		"IsDefault": subtitle.Default, "IsForced": subtitle.Forced,
		"IsHearingImpaired": subtitle.HearingImpaired, "IsVisualImpaired": subtitle.VisualImpaired,
		"IsExternal": subtitle.External, "IsTextSubtitleStream": isTextSubtitleCodec(subtitle.Codec),
		"SupportsExternalStream": subtitle.External, "Protocol": "File",
	}
	if subtitle.External {
		mapped["DeliveryMethod"] = "External"
		mapped["DeliveryUrl"] = embySubtitleDeliveryURL(mediaID, subtitle.Index)
	}
	return mapped
}

func setStringValue(target map[string]any, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		target[key] = value
	}
}

func probeFrameRate(value string) (float64, bool) {
	parts := strings.SplitN(strings.TrimSpace(value), "/", 2)
	numerator, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || numerator <= 0 {
		return 0, false
	}
	if len(parts) == 1 {
		return numerator, true
	}
	denominator, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || denominator <= 0 {
		return 0, false
	}
	return numerator / denominator, true
}

func embySubtitleDeliveryURL(mediaID string, index int) string {
	return fmt.Sprintf("/Videos/%s/Subtitles/%d/Stream.vtt", url.PathEscape(strings.TrimSpace(mediaID)), index)
}

func subtitleDisplayTitle(subtitle SubtitleSelection) string {
	parts := make([]string, 0, 3)
	if subtitle.Title != "" && subtitle.Title != "und" {
		parts = append(parts, subtitle.Title)
	}
	if language := probeDisplayLanguage(subtitle.Language); language != "" && language != subtitle.Title {
		parts = append(parts, language)
	}
	if subtitle.Codec != "" {
		parts = append(parts, strings.ToUpper(subtitle.Codec))
	}
	if subtitle.Forced {
		parts = append(parts, "(强制)")
	}
	if len(parts) == 0 {
		return "Subtitle"
	}
	return strings.Join(parts, " ")
}

func probeStreamDisplayTitle(stream ProbeStream) string {
	parts := make([]string, 0, 4)
	switch stream.CodecType {
	case "video":
		if resolution := probeResolutionLabel(stream.Width, stream.Height); resolution != "" {
			parts = append(parts, resolution)
		}
		if videoRange := probeVideoRange(stream); videoRange != "" && videoRange != "SDR" {
			parts = append(parts, videoRange)
		}
	case "audio":
		if language := probeDisplayLanguage(stream.Tags.Language); language != "" {
			parts = append(parts, language)
		}
		if codec := strings.TrimSpace(stream.CodecName); codec != "" {
			parts = append(parts, strings.ToUpper(codec))
		}
		if layout := probeChannelLayout(stream); layout != "" {
			parts = append(parts, layout)
		}
	case "subtitle":
		if title := strings.TrimSpace(stream.Tags.Title); title != "" {
			parts = append(parts, title)
		}
		if language := probeDisplayLanguage(stream.Tags.Language); language != "" {
			parts = append(parts, language)
		}
	}
	if stream.CodecType != "audio" {
		codec := strings.TrimSpace(stream.CodecName)
		if codec != "" {
			parts = append(parts, strings.ToUpper(codec))
		}
	}
	if stream.CodecType == "audio" {
		if stream.Disposition.Default {
			parts = append(parts, "(默认)")
		}
		if stream.Disposition.Forced {
			parts = append(parts, "(强制)")
		}
	}
	return strings.Join(parts, " ")
}

func probeResolutionLabel(width, height int) string {
	switch {
	case width >= 7000 || height >= 4000:
		return "8K"
	case width >= 3800 || height >= 2000:
		return "4K"
	case height >= 1080:
		return "1080p"
	case height >= 720:
		return "720p"
	case width > 0 && height > 0:
		return fmt.Sprintf("%dx%d", width, height)
	default:
		return ""
	}
}

func probeChannelLayout(stream ProbeStream) string {
	if layout := strings.TrimSpace(stream.ChannelLayout); layout != "" {
		return layout
	}
	switch stream.Channels {
	case 1:
		return "mono"
	case 2:
		return "stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		if stream.Channels > 0 {
			return fmt.Sprintf("%dch", stream.Channels)
		}
		return ""
	}
}

func probeVideoRange(stream ProbeStream) string {
	transfer := strings.ToLower(stream.ColorTransfer)
	for _, side := range stream.SideData {
		if strings.Contains(strings.ToLower(side.Type), "dovi") || strings.Contains(strings.ToLower(side.Type), "dolby vision") {
			return "Dolby Vision"
		}
		if strings.Contains(strings.ToLower(side.Type), "hdr10+") || strings.Contains(strings.ToLower(side.Type), "smpte2094") {
			return "HDR 10+"
		}
	}
	switch transfer {
	case "smpte2084":
		return "HDR 10"
	case "arib-std-b67":
		return "HLG"
	case "bt709", "smpte170m", "bt470m", "bt470bg", "iec61966-2-1":
		return "SDR"
	default:
		return ""
	}
}

func probeExtendedVideoType(videoRange string) (string, string, string) {
	switch videoRange {
	case "HDR 10":
		return "Hdr10", "Hdr10", "HDR 10"
	case "HDR 10+":
		return "Hdr10Plus", "Hdr10Plus0", "HDR 10+"
	case "Dolby Vision":
		return "DolbyVision", "None", "Dolby Vision"
	case "HLG":
		return "HyperLogGamma", "HyperLogGamma", "Hybrid Log-Gamma"
	default:
		return "", "", ""
	}
}
