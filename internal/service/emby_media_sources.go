package service

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func (e *EmbyService) mediaSourcesForItem(ctx context.Context, m *model.Media, asEmbedded, directOnly bool) []map[string]any {
	if m == nil {
		return nil
	}
	view, err := e.repo.MediaView.FindByID(ctx, m.ID)
	if err != nil || view == nil {
		return []map[string]any{e.mediaSource(ctx, m, embyMediaVersionName(m, m.Title), asEmbedded, directOnly)}
	}
	return e.mediaSourcesForView(ctx, view, asEmbedded, directOnly)
}

func (e *EmbyService) mediaSourcesForView(ctx context.Context, m *model.MediaView, asEmbedded, directOnly bool) []map[string]any {
	siblings := e.mediaVersionSiblings(ctx, m)
	if len(siblings) == 0 {
		return []map[string]any{e.mediaSource(ctx, &m.Media, embyMediaVersionName(&m.Media, m.Title), asEmbedded, directOnly)}
	}
	return e.mediaSourcesFromViews(ctx, siblings, asEmbedded, directOnly)
}

func (e *EmbyService) mediaSourcesFromViews(ctx context.Context, siblings []model.MediaView, asEmbedded, directOnly bool) []map[string]any {
	sources := make([]map[string]any, 0, len(siblings))
	for i := range siblings {
		name := embyMediaVersionName(&siblings[i].Media, siblings[i].Title)
		sources = append(sources, e.mediaSource(ctx, &siblings[i].Media, name, asEmbedded, directOnly))
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

func (e *EmbyService) mediaVersionSiblings(ctx context.Context, m *model.MediaView) []model.MediaView {
	if e == nil || e.repo == nil || e.repo.DB == nil || m == nil || strings.TrimSpace(m.ID) == "" {
		return nil
	}
	libraryIDs := e.mergedLibraryIDs(ctx, m.LibraryID)
	if len(libraryIDs) == 0 {
		libraryIDs = []string{m.LibraryID}
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("media.library_id IN ?", libraryIDs).
		Where("media.season_num = ? AND media.episode_num = ?", m.SeasonNum, m.EpisodeNum)
	if strings.TrimSpace(m.MetadataID) != "" {
		q = q.Where("media.metadata_id = ?", m.MetadataID)
	} else {
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
	views, err := e.repo.MediaView.FindByIDs(ctx, ids, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || len(views) == 0 {
		return []model.MediaView{*m}
	}
	views = collapseExactPathViews(views)
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].ID == m.ID {
			return true
		}
		if views[j].ID == m.ID {
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
	candidateCloud := strings.TrimSpace(candidate.STRMURL) != "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(candidate.Path)), "cloud://")
	currentCloud := strings.TrimSpace(current.STRMURL) != "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(current.Path)), "cloud://")
	if candidateCloud != currentCloud {
		return !candidateCloud
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

func (e *EmbyService) mediaStreams(m *model.Media) []map[string]any {
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
