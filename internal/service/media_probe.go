package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

var ErrMediaProbeSourceChanged = errors.New("media probe source changed")

const FFprobePathMappingsSettingKey = "ffprobe.path_mappings"

const ProbeSummaryVersion = 1

// ProbeBackfillResult 汇总一次完整轨道回填结果。
type ProbeBackfillResult struct {
	Total     int64
	Completed int64
	Skipped   int64
	Failed    int64
	Details   []string
}

func (r ProbeBackfillResult) Metrics() map[string]int64 {
	return map[string]int64{
		"total": r.Total, "completed": r.Completed,
		"skipped": r.Skipped, "failed": r.Failed,
	}
}

type mediaProbeRunner interface {
	Probe(ctx context.Context, path string) (*ProbeResult, error)
	ProbeHTTP(ctx context.Context, rawURL string) (*ProbeResult, error)
}

type mediaProbeSource struct {
	identity string
	path     string
	url      string
	local    bool
	size     int64
}

// MediaProbeService 是完整探测文档的唯一写入入口。
type MediaProbeService struct {
	repo  *repository.Container
	probe mediaProbeRunner
	cache *RuntimeCacheService
}

func NewMediaProbeService(repo *repository.Container, probe mediaProbeRunner) *MediaProbeService {
	return &MediaProbeService{repo: repo, probe: probe}
}

func (s *MediaProbeService) SetRuntimeCache(cache *RuntimeCacheService) *MediaProbeService {
	if s != nil {
		s.cache = cache
	}
	return s
}

func (s *MediaProbeService) ProbeMedia(ctx context.Context, mediaID string) (*ProbeResult, error) {
	if s == nil || s.repo == nil || s.repo.Media == nil || s.probe == nil {
		return nil, errors.New("media probe unavailable")
	}
	media, err := s.repo.Media.FindByID(ctx, strings.TrimSpace(mediaID))
	if err != nil || media == nil {
		if err != nil {
			return nil, err
		}
		return nil, ErrMediaNotFound
	}
	source, err := s.resolveSource(ctx, media)
	if err != nil {
		return nil, err
	}
	var result *ProbeResult
	if source.url != "" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(remoteMediaProbeDelay()):
		}
		result, err = s.probe.ProbeHTTP(ctx, source.url)
	} else {
		result, err = s.probe.Probe(ctx, source.path)
	}
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, media.ID, source, result); err != nil {
		return nil, err
	}
	if s.cache != nil {
		s.cache.DeletePrefix(ctx, "media:")
	}
	return result, nil
}

func remoteMediaProbeDelay() time.Duration {
	return time.Duration(secureRandomIntn(4)+2) * time.Second
}

func (s *MediaProbeService) Load(ctx context.Context, mediaID string) (*ProbeDocument, bool) {
	if s == nil || s.repo == nil || s.repo.MediaProbe == nil {
		return nil, false
	}
	row, err := s.repo.MediaProbe.FindByMediaID(ctx, mediaID)
	if err != nil || row == nil {
		return nil, false
	}
	doc, err := UnmarshalProbeDocument(row.ProbeJSON, row.SchemaVersion)
	return doc, err == nil
}

func (s *MediaProbeService) LoadMany(ctx context.Context, mediaIDs []string) map[string]*ProbeDocument {
	out := make(map[string]*ProbeDocument, len(mediaIDs))
	if s == nil || s.repo == nil || s.repo.MediaProbe == nil {
		return out
	}
	rows, err := s.repo.MediaProbe.ListByMediaIDs(ctx, mediaIDs)
	if err != nil {
		return out
	}
	for id, row := range rows {
		if doc, err := UnmarshalProbeDocument(row.ProbeJSON, row.SchemaVersion); err == nil {
			out[id] = doc
		}
	}
	return out
}

func (s *MediaProbeService) NeedsProbe(ctx context.Context, mediaID string) bool {
	_, ok := s.Load(ctx, mediaID)
	return !ok
}

// BackfillSummaries projects existing valid probe documents without probing media again.
func (s *MediaProbeService) BackfillSummaries(ctx context.Context) error {
	if s == nil || s.repo == nil || s.repo.DB == nil || s.repo.MediaProbe == nil {
		return errors.New("media probe unavailable")
	}
	const pageSize = 100
	lastID := ""
	for {
		var rows []model.MediaProbeMetadata
		query := s.repo.DB.WithContext(ctx).
			Where("COALESCE(summary_version, 0) <> ?", ProbeSummaryVersion).
			Order("media_id").Limit(pageSize)
		if lastID != "" {
			query = query.Where("media_id > ?", lastID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for i := range rows {
			doc, err := UnmarshalProbeDocument(rows[i].ProbeJSON, rows[i].SchemaVersion)
			if err != nil {
				continue
			}
			projectProbeSummary(&rows[i], doc, 0)
			if err := s.repo.MediaProbe.Upsert(ctx, &rows[i]); err != nil {
				return err
			}
		}
		lastID = rows[len(rows)-1].MediaID
	}
}

// BackfillLibrary 回填指定媒体库中缺失或过期的完整探测文档，limit 为零时不限制探测数量。
func (s *MediaProbeService) BackfillLibrary(ctx context.Context, libraryID string, limit int, progress func(ProbeBackfillResult)) (ProbeBackfillResult, error) {
	return s.backfill(ctx, strings.TrimSpace(libraryID), limit, progress)
}

// BackfillAll 为所有缺少当前完整探测文档的媒体执行回填，limit 为零时不限制探测数量。
func (s *MediaProbeService) BackfillAll(ctx context.Context, limit int, progress func(ProbeBackfillResult)) (ProbeBackfillResult, error) {
	return s.backfill(ctx, "", limit, progress)
}

func (s *MediaProbeService) backfill(ctx context.Context, libraryID string, limit int, progress func(ProbeBackfillResult)) (ProbeBackfillResult, error) {
	var result ProbeBackfillResult
	if s == nil || s.repo == nil || s.repo.DB == nil {
		return result, errors.New("media probe unavailable")
	}
	countQuery := s.repo.DB.WithContext(ctx).Model(&model.Media{})
	if libraryID != "" {
		countQuery = countQuery.Where("library_id = ?", libraryID)
	}
	if limit == 0 {
		if err := countQuery.Count(&result.Total).Error; err != nil {
			return result, err
		}
	}
	type probeBackfillRow struct {
		MediaID       string
		Path          string
		ProbeJSON     string
		SchemaVersion int
	}
	const pageSize = 100
	lastID := ""
	probeAttempts := 0
	for {
		var rows []probeBackfillRow
		query := s.repo.DB.WithContext(ctx).Table("media AS m").
			Select("m.id AS media_id, m.path, p.probe_json, p.schema_version").
			Joins("LEFT JOIN media_probe_metadata AS p ON p.media_id = m.id").
			Order("m.id").Limit(pageSize)
		if libraryID != "" {
			query = query.Where("m.library_id = ?", libraryID)
		}
		if lastID != "" {
			query = query.Where("m.id > ?", lastID)
		}
		if err := query.Scan(&rows).Error; err != nil {
			return result, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			result.Details = nil
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if _, err := UnmarshalProbeDocument(row.ProbeJSON, row.SchemaVersion); err == nil {
				result.Skipped++
			} else {
				probeAttempts++
				if probed, err := s.ProbeMedia(ctx, row.MediaID); err != nil || probed == nil || probed.Document == nil {
					result.Failed++
					if err == nil {
						err = errors.New("complete probe document unavailable")
					}
					if pathErr := (*os.PathError)(nil); errors.As(err, &pathErr) {
						err = pathErr.Err
					}
					reason := sanitizeTaskLogError(err).Error()
					if row.Path != "" {
						reason = strings.ReplaceAll(reason, row.Path, "[redacted-path]")
					}
					result.Details = []string{fmt.Sprintf("❌️ %s %s", row.MediaID, reason)}
				} else {
					result.Completed++
					result.Details = []string{fmt.Sprintf("✅️ %s %s", row.MediaID, row.Path)}
				}
			}
			if limit > 0 {
				result.Total++
			}
			if progress != nil {
				progress(result)
			}
			if limit > 0 && probeAttempts >= limit {
				return result, nil
			}
		}
		lastID = rows[len(rows)-1].MediaID
	}
	return result, nil
}

func (s *MediaProbeService) resolveSource(ctx context.Context, media *model.Media) (mediaProbeSource, error) {
	if media == nil {
		return mediaProbeSource{}, ErrMediaNotFound
	}
	if target := localSTRMFileTarget(media); target != "" {
		return localMediaProbeSource(media, target)
	}
	if rawURL := strings.TrimSpace(media.STRMURL); isHTTPPlaybackTarget(rawURL) {
		return resolveRemoteProbeSource(media, rawURL, s.probePathMappings(ctx)), nil
	}
	if strings.EqualFold(filepath.Ext(media.Path), ".strm") {
		return mediaProbeSource{}, errors.New("media probe source unavailable")
	}
	return localMediaProbeSource(media, media.Path)
}

// currentSourceIdentity 用探测结束时的映射快照重新计算媒体源身份。
func currentSourceIdentity(media *model.Media, rawMappings string) (string, error) {
	if target := localSTRMFileTarget(media); target != "" {
		source, err := localMediaProbeSource(media, target)
		return source.identity, err
	}
	if rawURL := strings.TrimSpace(media.STRMURL); isHTTPPlaybackTarget(rawURL) {
		return resolveRemoteProbeSource(media, rawURL, rawMappings).identity, nil
	}
	source, err := localMediaProbeSource(media, media.Path)
	return source.identity, err
}

// resolveRemoteProbeSource 仅将 STRM 远程地址映射为可访问的本地媒体文件。
func resolveRemoteProbeSource(media *model.Media, rawURL, rawMappings string) mediaProbeSource {
	remote := mediaProbeSource{identity: remoteProbeSourceIdentity(media, "http", rawURL), url: rawURL}
	if !strings.EqualFold(filepath.Ext(media.Path), ".strm") {
		return remote
	}
	target := mapRemoteProbePath(rawMappings, rawURL)
	if target == "" {
		return remote
	}
	local, err := localMediaProbeSource(media, target)
	if err != nil {
		return remote
	}
	return local
}

func (s *MediaProbeService) probePathMappings(ctx context.Context) string {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return ""
	}
	rawMappings, err := s.repo.Setting.Get(ctx, FFprobePathMappingsSettingKey)
	if err != nil {
		return ""
	}
	return rawMappings
}

// mapRemoteProbePath 将 URL 路径剩余部分拼接到本地前缀，并拒绝跨出前缀目录的结果。
func mapRemoteProbePath(rawMappings, rawURL string) string {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Host == "" || !isHTTPPlaybackTarget(rawURL) {
		return ""
	}
	bestPrefixLength, bestPath := -1, ""
	for _, line := range strings.Split(rawMappings, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=>", 2)
		if len(parts) != 2 {
			continue
		}
		prefix, err := url.Parse(strings.TrimSpace(parts[0]))
		localPrefix := filepath.Clean(strings.TrimSpace(parts[1]))
		if err != nil || prefix.Host == "" || prefix.User != nil || !isHTTPPlaybackTarget(prefix.String()) || prefix.RawQuery != "" || prefix.Fragment != "" || !filepath.IsAbs(localPrefix) {
			continue
		}
		if !strings.EqualFold(prefix.Scheme, target.Scheme) || !strings.EqualFold(prefix.Host, target.Host) {
			continue
		}
		relativePath, prefixLength, ok := relativeURLPath(prefix.Path, target.Path)
		if !ok || prefixLength <= bestPrefixLength {
			continue
		}
		candidate := filepath.Join(localPrefix, filepath.FromSlash(relativePath))
		relativeLocalPath, err := filepath.Rel(localPrefix, candidate)
		if err != nil || relativeLocalPath == ".." || strings.HasPrefix(relativeLocalPath, ".."+string(filepath.Separator)) {
			continue
		}
		bestPrefixLength, bestPath = prefixLength, candidate
	}
	return bestPath
}

// relativeURLPath 只在完整 URL 路径段边界上匹配前缀。
func relativeURLPath(prefixPath, targetPath string) (string, int, bool) {
	prefixPath = strings.TrimSuffix(prefixPath, "/")
	if prefixPath == "" {
		prefixPath = "/"
	}
	if targetPath == "" {
		targetPath = "/"
	}
	if prefixPath == "/" {
		return strings.TrimPrefix(targetPath, "/"), 1, true
	}
	if targetPath == prefixPath {
		return "", len(prefixPath), true
	}
	if !strings.HasPrefix(targetPath, prefixPath+"/") {
		return "", 0, false
	}
	return strings.TrimPrefix(targetPath[len(prefixPath):], "/"), len(prefixPath), true
}

func localMediaProbeSource(media *model.Media, target string) (mediaProbeSource, error) {
	stat, err := os.Stat(target)
	if err != nil {
		return mediaProbeSource{}, err
	}
	target = filepath.Clean(target)
	return mediaProbeSource{
		identity: fmt.Sprintf("local\x00%s\x00%s\x00%d\x00%d", media.Path, target, stat.Size(), stat.ModTime().UnixNano()),
		path:     target, local: true, size: stat.Size(),
	}, nil
}

func remoteProbeSourceIdentity(media *model.Media, kind, stableRef string) string {
	return strings.Join([]string{kind, media.Path, media.STRMURL, stableRef}, "\x00")
}

func projectProbeSummary(row *model.MediaProbeMetadata, doc *ProbeDocument, targetSize int64) {
	row.SummaryVersion = ProbeSummaryVersion
	row.DurationMS, row.SizeBytes, row.BitRate = 0, 0, 0
	row.Width, row.Height = 0, 0
	row.Container, row.VideoCodec, row.AudioCodec = "", "", ""
	if doc.Format.Duration > 0 {
		row.DurationMS = int64(math.Round(doc.Format.Duration * 1000))
	}
	row.SizeBytes = doc.Format.Size
	if targetSize > 0 {
		row.SizeBytes = targetSize
	}
	row.Container = strings.TrimSpace(doc.Format.Name)
	row.BitRate = doc.Format.BitRate
	for _, stream := range doc.Streams {
		switch stream.CodecType {
		case "video":
			if row.VideoCodec == "" {
				row.VideoCodec, row.Width, row.Height = stream.CodecName, stream.Width, stream.Height
			}
		case "audio":
			if row.AudioCodec == "" {
				row.AudioCodec = stream.CodecName
			}
		}
	}
}

func (s *MediaProbeService) persist(ctx context.Context, mediaID string, source mediaProbeSource, result *ProbeResult) error {
	var probeJSON string
	if result != nil && result.Document != nil {
		var err error
		probeJSON, err = MarshalProbeDocument(result.Document)
		if err != nil {
			return err
		}
	}
	rawMappings := s.probePathMappings(ctx)
	return s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.Media
		if err := tx.Where("id = ?", mediaID).First(&current).Error; err != nil {
			return err
		}
		identity, err := currentSourceIdentity(&current, rawMappings)
		if err != nil || identity != source.identity {
			return ErrMediaProbeSourceChanged
		}
		if probeJSON == "" {
			return nil
		}
		row := model.MediaProbeMetadata{
			MediaID: mediaID, ProbeJSON: probeJSON,
			SchemaVersion: ProbeDocumentSchemaVersion, ProbedAt: time.Now().UTC(),
		}
		targetSize := int64(0)
		if source.local {
			targetSize = source.size
		}
		projectProbeSummary(&row, result.Document, targetSize)
		return repository.New(tx).MediaProbe.Upsert(ctx, &row)
	})
}
