// Package service contains the business logic of MediaStationGo. Handlers
// deserialize the HTTP request, call into a Service method, then serialize
// the response. Services own all cross-cutting policy (auth, scanning,
// transcoding, etc.) and never deal with HTTP types directly.
package service

import (
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Container holds every service initialized at startup. Handlers receive a
// pointer to it and pick the relevant fields.
type Container struct {
	Cfg    *config.Config
	Log    *zap.Logger
	Repo   *repository.Container
	WSHub  *Hub
	Auth   *AuthService
	Media  *MediaService
	Scan   *ScannerService
	Stream *StreamService
}

// New builds the service container.
func New(cfg *config.Config, log *zap.Logger, repos *repository.Container) *Container {
	hub := NewHub(log)
	go hub.Run()
	return &Container{
		Cfg:    cfg,
		Log:    log,
		Repo:   repos,
		WSHub:  hub,
		Auth:   NewAuthService(cfg, log, repos),
		Media:  NewMediaService(cfg, log, repos),
		Scan:   NewScannerService(cfg, log, repos, hub),
		Stream: NewStreamService(cfg, log, repos),
	}
}

// Close releases any resources held by services (e.g. the websocket hub).
func (c *Container) Close() {
	if c.WSHub != nil {
		c.WSHub.Stop()
	}
}
