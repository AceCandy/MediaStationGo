package repository

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NFORepository 仅保存本地资料，不解析外部标识或访问共享元数据写入器。
type NFORepository struct{ db *gorm.DB }

// NFOIngest 按根到叶排列层级，文件快照独立于同组其他版本。
type NFOIngest struct {
	Items              []model.NFOItem
	Binding            model.NFOMediaBinding
	Artwork            []NFOArtwork
	PreserveItemFields []bool
}

// NFOArtwork 是入库事务待保存的图片资产，不关联普通元数据。
type NFOArtwork struct {
	ItemIndex  int
	Type       string
	SourcePath string
	Asset      *model.ArtworkAsset
}

// Ingest 将文件事实、条目层级和资料绑定作为一次事务保存。
// 无有效资料时仅更新文件状态，保留已有资料绑定。
func (r *NFORepository) Ingest(ctx context.Context, media *model.Media, input *NFOIngest) (bool, error) {
	if media == nil || media.CatalogSource != model.CatalogSourceNFO || media.MetadataID != "" {
		return false, errors.New("本地资料文件归属无效")
	}
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		status, reason := media.ScrapeStatus, media.ScrapeError
		// 同路径首次入库也需要串行化，不能依赖尚不存在的行锁。
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "nfo:"+media.Path).Error; err != nil {
			return err
		}
		var library model.Library
		if err := tx.First(&library, "id = ? AND type IN ?", media.LibraryID, []string{model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV}).Error; err != nil {
			return err
		}
		var existing model.Media
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("path = ?", media.Path).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing.ID != "" && (existing.CatalogSource != model.CatalogSourceNFO || existing.LibraryID != media.LibraryID) {
			return errors.New("文件尚未迁入本地资料体系或属于其他媒体库")
		}
		fileChanged := existing.ID != "" && (existing.ScanFileSizeBytes != media.ScanFileSizeBytes || existing.ScanFileMTimeNS != media.ScanFileMTimeNS)
		if existing.ID != "" && !fileChanged && existing.ScrapeStatus == status && existing.ScrapeError == reason && existing.LibraryRootID == media.LibraryRootID && existing.RelativePath == media.RelativePath {
			var binding model.NFOMediaBinding
			if err := tx.Where("media_id = ?", existing.ID).Limit(1).Find(&binding).Error; err != nil {
				return err
			}
			if input == nil || binding.MediaID != "" && binding.Fingerprint == input.Binding.Fingerprint {
				*media = existing
				return nil
			}
		}
		if fileChanged {
			if err := (&MediaProbeRepository{db: tx}).DeleteByMediaID(ctx, existing.ID); err != nil {
				return err
			}
		}
		if _, err := (&MediaRepository{db: tx}).upsert(ctx, media); err != nil {
			return err
		}
		if input != nil {
			if len(input.Items) == 0 {
				return errors.New("本地资料缺少条目")
			}
			for _, artwork := range input.Artwork {
				if artwork.Asset == nil || artwork.ItemIndex < 0 || artwork.ItemIndex >= len(input.Items) {
					return errors.New("本地图片资产无效")
				}
				if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sha256"}}, DoNothing: true}).Create(artwork.Asset).Error; err != nil {
					return err
				}
				var saved model.ArtworkAsset
				if err := tx.Where("sha256 = ?", artwork.Asset.SHA256).First(&saved).Error; err != nil {
					return err
				}
				if artwork.Type == model.ArtworkTypePoster {
					input.Items[artwork.ItemIndex].PosterAssetID = saved.ID
				} else {
					input.Items[artwork.ItemIndex].BackdropAssetID = saved.ID
				}
			}
			parentID := ""
			filePoster := input.Items[len(input.Items)-1].PosterAssetID
			fileBackdrop := input.Items[len(input.Items)-1].BackdropAssetID
			for i := range input.Items {
				item := &input.Items[i]
				item.LibraryID = media.LibraryID
				if parentID != "" {
					id := parentID
					item.ParentID = &id
				}
				updates := clause.AssignmentColumns([]string{"title", "original_name", "overview", "year", "release_date", "rating", "genres", "countries", "languages", "nsfw", "external_ids", "people", "updated_at"})
				updates = append(updates, clause.Assignment{Column: clause.Column{Name: "poster_asset_id"}, Value: gorm.Expr("COALESCE(NULLIF(EXCLUDED.poster_asset_id,''),nfo_items.poster_asset_id)")}, clause.Assignment{Column: clause.Column{Name: "backdrop_asset_id"}, Value: gorm.Expr("COALESCE(NULLIF(EXCLUDED.backdrop_asset_id,''),nfo_items.backdrop_asset_id)")})
				conflict := clause.OnConflict{
					Columns:   []clause.Column{{Name: "library_id"}, {Name: "local_key"}},
					DoUpdates: updates,
				}
				if i < len(input.PreserveItemFields) && input.PreserveItemFields[i] {
					conflict.DoUpdates = nil
					conflict.DoNothing = true
				}
				if err := tx.Clauses(conflict).Omit("Parent").Create(item).Error; err != nil {
					return err
				}
				var saved model.NFOItem
				if err := tx.Where("library_id = ? AND local_key = ?", item.LibraryID, item.LocalKey).First(&saved).Error; err != nil {
					return err
				}
				*item = saved
				parentID = item.ID
			}
			binding := &input.Binding
			binding.PosterAssetID, binding.BackdropAssetID = filePoster, fileBackdrop
			if existing.ID != "" && (filePoster == "" || fileBackdrop == "") {
				var old model.NFOMediaBinding
				if err := tx.Where("media_id = ?", existing.ID).Limit(1).Find(&old).Error; err != nil {
					return err
				}
				if filePoster == "" {
					binding.PosterAssetID = old.PosterAssetID
				}
				if fileBackdrop == "" {
					binding.BackdropAssetID = old.BackdropAssetID
				}
			}
			binding.MediaID, binding.ItemID, binding.UpdatedAt = media.ID, parentID, time.Now()
			if err := tx.Omit("Media", "Item").Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "media_id"}}, UpdateAll: true,
			}).Create(binding).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.Media{}).Where("id = ?", media.ID).Updates(map[string]any{
			"scrape_status": status, "scrape_error": reason,
			"local_metadata_hint": "", "lookup_tmdb_id": 0, "lookup_bangumi_id": 0,
			"lookup_douban_id": "", "lookup_thetvdb_id": "",
		}).Error; err != nil {
			return err
		}
		media.ScrapeStatus, media.ScrapeError = status, reason
		media.LocalMetadataHint = ""
		media.TMDbID, media.BangumiID = 0, 0
		media.DoubanID, media.TheTVDBID = "", ""
		changed = true
		return nil
	})
	return changed && err == nil, err
}
