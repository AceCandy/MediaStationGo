package service

import (
	"errors"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

var ErrSTRMURLInvalid = errors.New("invalid strm url")

// STRMService STRM 文件管理服务。
type STRMService struct {
	log  *zap.Logger
	repo *repository.Container
	cfg  *config.Config
}

// NewSTRMService 创建 STRM 服务。
func NewSTRMService(log *zap.Logger, repo *repository.Container, cfg *config.Config) *STRMService {
	return &STRMService{log: log, repo: repo, cfg: cfg}
}
