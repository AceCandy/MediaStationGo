package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type authenticatedUserKey struct{}

type authenticatedUserVisibility struct {
	id        string
	hideAdult bool
}

// WithAuthenticatedUser 仅在实时认证成功后传递本次请求的可见性快照，不保存凭据。
func WithAuthenticatedUser(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, authenticatedUserKey{}, authenticatedUserVisibility{
		id: user.ID, hideAdult: user.HideAdult,
	})
}
