package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var errCloudMediaPathUnsupported = errors.New("cloud media paths are no longer supported")

// Upsert inserts or updates a media row keyed by Path (unique index).
//
// 重要：当一条行已经存在时，scanner 重扫只应该刷新扫描指纹、路径提示和关联字段，
// 不能把刮削器维护的字段（标题改写、海报、provider ID、scrape_status 等）覆盖回零值。
//
// 之前用 Assign(*m).FirstOrCreate(m) 会把整张零值结构体写回，导致：
//  1. scrape_status 从 'matched' / 'no_match' 被清空成 ”；
//  2. 新建行使 GORM `default:pending` 也得不到应用（因为 zero value 被
//     显式写入）。这两个问题都让 EnrichLibrary(WHERE scrape_status='pending')
//     永远捞不到数据。
func (r *MediaRepository) Upsert(ctx context.Context, m *model.Media) error {
	if m != nil && m.CatalogSource == model.TaskSystemHongGuo {
		return r.upsertHongGuoMedia(ctx, m)
	}
	if m != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Path)), "cloud://") {
		return errCloudMediaPathUnsupported
	}
	var previousMetadataID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := &MediaRepository{db: tx}
		if err := txRepo.ResolveMetadata(ctx, m); err != nil {
			return err
		}
		var err error
		previousMetadataID, err = txRepo.upsert(ctx, m)
		return err
	})
	if err != nil {
		return err
	}
	r.refreshMetadataBestEffort(ctx, previousMetadataID, m.MetadataID)
	return nil
}

// ResolveMetadata 只按已入库的精确 provider 标识补充 metadata 关联，不创建占位元数据。
func (r *MediaRepository) ResolveMetadata(ctx context.Context, media *model.Media) error {
	if media == nil {
		return errors.New("media is required")
	}
	if media.CatalogSource != "" {
		return errors.New("独立资料媒体不能绑定现有资料体系")
	}
	if strings.TrimSpace(media.MetadataID) == "" {
		var existing model.Media
		err := r.db.WithContext(ctx).Where("path = ?", media.Path).First(&existing).Error
		if err == nil && existing.CatalogSource != "" {
			return errors.New("独立资料媒体不能绑定现有资料体系")
		} else if err == nil && strings.TrimSpace(existing.MetadataID) != "" {
			media.MetadataID = existing.MetadataID
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if strings.TrimSpace(media.MetadataID) == "" {
		metadata, err := r.findExistingMediaMetadata(ctx, media)
		if err != nil {
			return err
		}
		if metadata != nil {
			media.MetadataID = metadata.ID
			media.ScrapeStatus = "matched"
		}
	}
	if strings.TrimSpace(media.MetadataID) == "" {
		return nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.MetadataItem{}).Where("id = ?", media.MetadataID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errors.New("media metadata not found")
	}
	return nil
}

// FindExactMetadata 只按媒体当前的 provider 标识解析已有 canonical metadata，
// 不读取或复用媒体行已有的 metadata_id。
func (r *MediaRepository) FindExactMetadata(ctx context.Context, media *model.Media) (*model.MetadataItem, error) {
	if media == nil {
		return nil, errors.New("media is required")
	}
	return r.findExistingMediaMetadata(ctx, media)
}

func (r *MediaRepository) findExistingMediaMetadata(ctx context.Context, media *model.Media) (*model.MetadataItem, error) {
	metadataRepo := &MetadataRepository{db: r.db}
	entityKind := model.MetadataKindMovie
	if media.EpisodeNum > 0 {
		entityKind = model.MetadataKindSeries
	}
	identifiers := mediaMetadataIdentifiers(media, entityKind)
	if len(identifiers) == 0 {
		return nil, nil
	}
	var series *model.MetadataItem
	for _, identifier := range identifiers {
		item, err := metadataRepo.FindByIdentifier(ctx, identifier.Provider, identifier.EntityKind, identifier.ExternalID)
		if err != nil {
			return nil, err
		}
		if item == nil {
			continue
		}
		if series != nil && series.ID != item.ID {
			return nil, errors.New("media identifiers resolve to different metadata items")
		}
		series = item
	}
	if series == nil || entityKind == model.MetadataKindMovie {
		return series, nil
	}
	if media.SeasonNum < 0 || media.EpisodeNum <= 0 {
		return nil, nil
	}
	episode, err := metadataRepo.FindEpisode(ctx, series.ID, media.SeasonNum, media.EpisodeNum)
	if err != nil || episode == nil {
		return episode, err
	}
	media.SeriesID = series.ID
	return episode, nil
}

func mediaMetadataIdentifiers(media *model.Media, entityKind string) []model.MetadataIdentifier {
	identifiers := make([]model.MetadataIdentifier, 0, 4)
	if media.TMDbID > 0 {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "tmdb", EntityKind: entityKind, ExternalID: strconv.Itoa(media.TMDbID)})
	}
	if media.BangumiID > 0 {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "bangumi", EntityKind: entityKind, ExternalID: strconv.Itoa(media.BangumiID)})
	}
	if id := strings.TrimSpace(media.DoubanID); id != "" {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "douban", EntityKind: entityKind, ExternalID: id})
	}
	if id := strings.TrimSpace(media.TheTVDBID); id != "" {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "thetvdb", EntityKind: entityKind, ExternalID: id})
	}
	return identifiers
}

func (r *MediaRepository) upsert(ctx context.Context, m *model.Media) (string, error) {
	existing, created, err := r.findOrCreateMediaByPath(ctx, m)
	if err != nil {
		return "", err
	}
	if created {
		return "", nil
	}
	// 冲突复用的行可能在来源入口检查之后才插入，更新前必须重验归属。
	if existing.CatalogSource != m.CatalogSource || (m.CatalogSource == model.CatalogSourceNFO && existing.LibraryID != m.LibraryID) {
		return "", errors.New("不能把现有文件自动改绑到其他资料体系或本地资料库")
	}

	updates := mediaUpsertUpdates(existing, *m)
	return existing.MetadataID, r.applyMediaUpsertUpdates(ctx, m, existing, updates)
}

func (r *MediaRepository) findOrCreateMediaByPath(ctx context.Context, m *model.Media) (model.Media, bool, error) {
	var existing model.Media
	err := r.db.WithContext(ctx).Where("path = ?", m.Path).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 新行：保证 scrape_status 走 GORM default:pending（即留空让数据库填）。
		if m.ScrapeStatus == "" {
			m.ScrapeStatus = "pending"
		}
		result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path"}}, DoNothing: true,
		}).Create(m)
		if result.Error != nil {
			return model.Media{}, false, result.Error
		}
		if result.RowsAffected > 0 {
			return *m, true, nil
		}
		// 不使用已由创建钩子生成 ID 的 m，避免附带未持久化的主键过滤。
		err = r.db.WithContext(ctx).Where("path = ?", m.Path).First(&existing).Error
	}
	if err != nil {
		return model.Media{}, false, err
	}
	return existing, false, nil
}

func mediaUpsertUpdates(existing, incoming model.Media) map[string]any {
	updates := map[string]any{}
	if existing.CatalogSource == model.TaskSystemHongGuo {
		setIfChanged(updates, "lookup_catalog_id", existing.LookupCatalogID, incoming.LookupCatalogID)
		setIfChanged(updates, "season_num", existing.SeasonNum, incoming.SeasonNum)
		setIfChanged(updates, "episode_num", existing.EpisodeNum, incoming.EpisodeNum)
	}
	addMediaFileScanUpdates(updates, existing, incoming)
	addMediaTitleUpdates(updates, existing, incoming)
	addMediaExternalIDUpdates(updates, existing, incoming)
	addMatchedMediaStatusUpdate(updates, existing, incoming)
	addMediaPlacementUpdates(updates, existing, incoming)
	addMediaPartUpdates(updates, existing, incoming)
	addMediaSTRMUpdate(updates, existing, incoming)
	return updates
}

func addMediaFileScanUpdates(updates map[string]any, existing, incoming model.Media) {
	// 已存在：仅刷新文件层面的字段。
	setIfChanged(updates, "scan_file_size_bytes", existing.ScanFileSizeBytes, incoming.ScanFileSizeBytes)
	setIfChanged(updates, "scan_file_mtime_ns", existing.ScanFileMTimeNS, incoming.ScanFileMTimeNS)
	setIfChanged(updates, "local_metadata_hint", existing.LocalMetadataHint, incoming.LocalMetadataHint)
	// 回填硬链接身份标识，便于后续扫描去重（避免重复识别/多倍占用）。
	if incoming.FileID != "" && incoming.FileID != existing.FileID {
		updates["file_id"] = incoming.FileID
	}
}

func addMediaTitleUpdates(updates map[string]any, existing, incoming model.Media) {
	if incoming.Title != "" {
		// scanner 给出的标题只是从路径推导，刮削后 title 已被替换为
		// 真实剧名。仅在 existing 还停留在 'pending'/'' 时回填扫描标题，
		// 避免覆盖刮削结果。
		if incoming.ScrapeStatus == "matched" || existing.ScrapeStatus == "pending" || existing.ScrapeStatus == "" || existing.ScrapeStatus == "no_match" {
			titleChanged := !strings.EqualFold(strings.TrimSpace(existing.Title), strings.TrimSpace(incoming.Title))
			yearChanged := incoming.Year > 0 && existing.Year != incoming.Year
			setIfChanged(updates, "scan_title", existing.Title, incoming.Title)
			if incoming.Year > 0 {
				setIfChanged(updates, "scan_year", existing.Year, incoming.Year)
			}
			if strings.TrimSpace(existing.ScrapeStatus) == "no_match" && incoming.ScrapeStatus != "matched" && (titleChanged || yearChanged) {
				updates["scrape_status"] = "pending"
			}
		}
	}
}

func addMediaExternalIDUpdates(updates map[string]any, existing, incoming model.Media) {
	status := strings.TrimSpace(existing.ScrapeStatus)
	if !mediaCanRefreshExternalIDs(status, incoming) {
		return
	}
	changedExternalID := addIncomingMediaProviderIDs(updates, existing, incoming)
	if incoming.Year > 0 && existing.Year <= 0 {
		updates["scan_year"] = incoming.Year
	}
	if changedExternalID && (status == "no_match" || status == "matched") && incoming.ScrapeStatus != "matched" {
		updates["scrape_status"] = "pending"
	}
}

func mediaCanRefreshExternalIDs(existingStatus string, incoming model.Media) bool {
	return existingStatus == "pending" || existingStatus == "" ||
		existingStatus == "no_match" || existingStatus == "matched" ||
		incoming.ScrapeStatus == "matched"
}

func addMatchedMediaStatusUpdate(updates map[string]any, existing, incoming model.Media) {
	if incoming.ScrapeStatus == "matched" && incoming.MetadataID != "" {
		if existing.MetadataID != incoming.MetadataID {
			updates["metadata_id"] = incoming.MetadataID
		}
		setIfChanged(updates, "scrape_status", existing.ScrapeStatus, incoming.ScrapeStatus)
	}
}

func addMediaPlacementUpdates(updates map[string]any, existing, incoming model.Media) {
	if incoming.LibraryID != "" && incoming.LibraryID != existing.LibraryID && existing.LibraryID == "" {
		updates["library_id"] = incoming.LibraryID
	}
	if incoming.LibraryRootID != "" && incoming.LibraryRootID != existing.LibraryRootID {
		updates["library_root_id"] = incoming.LibraryRootID
	}
	if incoming.RelativePath != "" && incoming.RelativePath != existing.RelativePath {
		updates["relative_path"] = incoming.RelativePath
	}
	seasonChanged := (incoming.SeasonNum > 0 || incoming.EpisodeNum > 0) && existing.SeasonNum != incoming.SeasonNum
	episodeChanged := incoming.EpisodeNum > 0 && existing.EpisodeNum != incoming.EpisodeNum
	if seasonChanged {
		updates["season_num"] = incoming.SeasonNum
	}
	if episodeChanged {
		updates["episode_num"] = incoming.EpisodeNum
	}
	if strings.TrimSpace(existing.ScrapeStatus) == "no_match" && incoming.ScrapeStatus != "matched" && (seasonChanged || episodeChanged) {
		updates["scrape_status"] = "pending"
	}
	if seasonChanged && existing.SeasonNum < 0 && incoming.SeasonNum >= 0 && incoming.EpisodeNum > 0 && existing.ScrapeStatus == "error" && incoming.ScrapeStatus != "matched" {
		updates["scrape_status"] = "pending"
		updates["scrape_error"] = ""
	}
}

func addMediaPartUpdates(updates map[string]any, existing, incoming model.Media) {
	setIfChanged(updates, "part_group_key", existing.PartGroupKey, incoming.PartGroupKey)
	setIfChanged(updates, "part_index", existing.PartIndex, incoming.PartIndex)
}

func addMediaSTRMUpdate(updates map[string]any, existing, incoming model.Media) {
	if incoming.STRMURL != "" {
		setIfChanged(updates, "strm_url", existing.STRMURL, incoming.STRMURL)
	}
}

func addIncomingMediaProviderIDs(updates map[string]any, existing, incoming model.Media) bool {
	changed := false
	if incoming.TMDbID > 0 && existing.TMDbID != incoming.TMDbID {
		updates["lookup_tmdb_id"] = incoming.TMDbID
		changed = true
	}
	if incoming.BangumiID > 0 && existing.BangumiID != incoming.BangumiID {
		updates["lookup_bangumi_id"] = incoming.BangumiID
		changed = true
	}
	if incoming.DoubanID != "" && strings.TrimSpace(existing.DoubanID) != strings.TrimSpace(incoming.DoubanID) {
		updates["lookup_douban_id"] = incoming.DoubanID
		changed = true
	}
	if incoming.TheTVDBID != "" && strings.TrimSpace(existing.TheTVDBID) != strings.TrimSpace(incoming.TheTVDBID) {
		updates["lookup_thetvdb_id"] = incoming.TheTVDBID
		changed = true
	}
	return changed
}

func (r *MediaRepository) applyMediaUpsertUpdates(ctx context.Context, m *model.Media, existing model.Media, updates map[string]any) error {
	if len(updates) == 0 {
		*m = existing
		return nil
	}
	if err := r.db.WithContext(ctx).Model(&model.Media{}).
		Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return err
	}
	// 回写 ID / 不可变字段，让 caller 拿到完整的现有行。
	*m = existing
	if fresh, err := r.FindByID(ctx, existing.ID); err == nil && fresh != nil {
		*m = *fresh
	}
	return nil
}

func setIfChanged[T comparable](updates map[string]any, key string, current, next T) {
	if current != next {
		updates[key] = next
	}
}
