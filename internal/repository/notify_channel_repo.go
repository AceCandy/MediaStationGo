// Package repository 实现通知渠道配置的数据访问层。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// NotifyChannelRepository persists model.NotifyChannel records.
type NotifyChannelRepository struct{ db *gorm.DB }

// Create inserts a new notification channel.
func (r *NotifyChannelRepository) Create(ctx context.Context, c *model.NotifyChannel) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// FindByID returns the notification channel by ID, or (nil, nil) when absent.
func (r *NotifyChannelRepository) FindByID(ctx context.Context, id string) (*model.NotifyChannel, error) {
	var c model.NotifyChannel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// List returns all notification channels ordered by creation time.
func (r *NotifyChannelRepository) List(ctx context.Context) ([]model.NotifyChannel, error) {
	var rows []model.NotifyChannel
	err := r.db.WithContext(ctx).Order("created_at asc").Find(&rows).Error
	return rows, err
}

// ListEnabled returns all enabled notification channels.
func (r *NotifyChannelRepository) ListEnabled(ctx context.Context) ([]model.NotifyChannel, error) {
	var rows []model.NotifyChannel
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("created_at asc").Find(&rows).Error
	return rows, err
}

// ListByEvent returns all enabled channels that subscribe to the given event type.
func (r *NotifyChannelRepository) ListByEvent(ctx context.Context, eventType string) ([]model.NotifyChannel, error) {
	var rows []model.NotifyChannel
	// Events is a JSON array stored as text; use LIKE for simple matching.
	// This works for exact event type matches within the JSON array.
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND events LIKE ?", true, "%\""+eventType+"\"%").
		Find(&rows).Error
	return rows, err
}

// ListByType returns all channels of a given type (telegram/wechat/bark/webhook/email).
func (r *NotifyChannelRepository) ListByType(ctx context.Context, channelType string) ([]model.NotifyChannel, error) {
	var rows []model.NotifyChannel
	err := r.db.WithContext(ctx).Where("type = ?", channelType).Find(&rows).Error
	return rows, err
}

// Update persists changes to a notification channel.
func (r *NotifyChannelRepository) Update(ctx context.Context, c *model.NotifyChannel) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// Delete removes a notification channel (soft-delete).
func (r *NotifyChannelRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.NotifyChannel{}, "id = ?", id).Error
}
