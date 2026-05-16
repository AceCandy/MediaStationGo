package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// AssistantRepository persists model.AssistantSession + AssistantMessage records.
type AssistantRepository struct{ db *gorm.DB }

// ─── Session ────────────────────────────────────────────────────────────

// CreateSession inserts a new chat session.
func (r *AssistantRepository) CreateSession(ctx context.Context, s *model.AssistantSession) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// FindSession returns a session by ID, or (nil, nil).
func (r *AssistantRepository) FindSession(ctx context.Context, id string) (*model.AssistantSession, error) {
	var s model.AssistantSession
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSessions returns sessions for a user, or all when userID is empty.
func (r *AssistantRepository) ListSessions(ctx context.Context, userID string) ([]model.AssistantSession, error) {
	q := r.db.WithContext(ctx).Model(&model.AssistantSession{})
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	var rows []model.AssistantSession
	err := q.Order("created_at desc").Find(&rows).Error
	return rows, err
}

// DeleteSession soft-deletes a session (cascade handled by GORM hooks if set).
func (r *AssistantRepository) DeleteSession(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.AssistantSession{}, "id = ?", id).Error
}

// ─── Message ────────────────────────────────────────────────────────────

// AppendMessage inserts a new message into a session.
func (r *AssistantRepository) AppendMessage(ctx context.Context, m *model.AssistantMessage) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// ListMessages returns all messages for a session in chronological order.
func (r *AssistantRepository) ListMessages(ctx context.Context, sessionID string) ([]model.AssistantMessage, error) {
	var rows []model.AssistantMessage
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).
		Order("created_at asc").Find(&rows).Error
	return rows, err
}
