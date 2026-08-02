package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// FavoriteRepository persists model.Favorite records.
type FavoriteRepository struct{ db *gorm.DB }

// Toggle flips the favourite flag for (user, media). Returns the new state.
func (r *FavoriteRepository) Toggle(ctx context.Context, userID, mediaID string) (bool, error) {
	media, err := r.findMedia(ctx, mediaID)
	if err != nil {
		return false, err
	}
	var f model.Favorite
	metadataID := mediaMetadataID(media)
	err = r.targetQuery(r.db.WithContext(ctx), userID, metadataID, mediaID).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.Set(ctx, userID, mediaID, true)
	}
	if err != nil {
		return false, err
	}
	return false, r.db.WithContext(ctx).Delete(&f).Error
}

// Set makes a media favorite state idempotent while storing canonical metadata identity.
func (r *FavoriteRepository) Set(ctx context.Context, userID, mediaID string, favorite bool) (bool, error) {
	media, err := r.findMedia(ctx, mediaID)
	if err != nil {
		return false, err
	}
	return r.SetByIdentity(ctx, userID, mediaMetadataID(media), mediaID, favorite)
}

// SetByIdentity 保存作品身份，并保留一个具体媒体版本。
func (r *FavoriteRepository) SetByIdentity(ctx context.Context, userID, metadataID, mediaID string, favorite bool) (bool, error) {
	if strings.TrimSpace(metadataID) == "" {
		return false, errors.New("metadata id is required")
	}
	q := r.targetQuery(r.db.WithContext(ctx), userID, metadataID, mediaID)
	if !favorite {
		return false, q.Delete(&model.Favorite{}).Error
	}
	var existing model.Favorite
	err := q.First(&existing).Error
	if err == nil {
		return true, r.db.WithContext(ctx).Model(&existing).Update("media_id", mediaID).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	err = q.Unscoped().Where("deleted_at IS NOT NULL").First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, r.db.WithContext(ctx).Create(&model.Favorite{
			UserID: userID, MetadataID: metadataID, MediaID: mediaID,
		}).Error
	}
	if err != nil {
		return false, err
	}
	return true, r.db.WithContext(ctx).Unscoped().Model(&existing).Updates(map[string]any{
		"metadata_id": metadataID,
		"media_id":    mediaID,
		"deleted_at":  nil,
	}).Error
}

func (r *FavoriteRepository) IsFavorite(ctx context.Context, userID, mediaID string) (bool, error) {
	media, err := r.findMedia(ctx, mediaID)
	if err != nil {
		return false, err
	}
	return r.IsFavoriteByIdentity(ctx, userID, mediaMetadataID(media), mediaID)
}

func (r *FavoriteRepository) IsFavoriteByIdentity(ctx context.Context, userID, metadataID, mediaID string) (bool, error) {
	if strings.TrimSpace(metadataID) == "" {
		return false, errors.New("metadata id is required")
	}
	var count int64
	err := r.targetQuery(r.db.WithContext(ctx).Model(&model.Favorite{}), userID, metadataID, mediaID).Count(&count).Error
	return count > 0, err
}

func (r *FavoriteRepository) findMedia(ctx context.Context, mediaID string) (*model.Media, error) {
	var media model.Media
	err := r.db.WithContext(ctx).Where("id = ?", mediaID).First(&media).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("media not found")
	}
	return &media, err
}

func (r *FavoriteRepository) targetQuery(q *gorm.DB, userID, metadataID, mediaID string) *gorm.DB {
	return q.Where("user_id = ? AND metadata_id = ?", userID, metadataID)
}

func mediaMetadataID(media *model.Media) string {
	if media != nil {
		return media.MetadataID
	}
	return ""
}

// ListByUser returns all favourite media IDs for a user.
func (r *FavoriteRepository) ListByUser(ctx context.Context, userID string) ([]model.Favorite, error) {
	var rows []model.Favorite
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at desc").Find(&rows).Error
	return rows, err
}
