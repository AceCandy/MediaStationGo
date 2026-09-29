package service

import (
	"context"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const libraryBasicCacheTTL = 30 * time.Second

// FindLibraryBasic 缓存只读展示所需的库基础信息；目录、事务和写后读取应直接使用仓储。
func FindLibraryBasic(ctx context.Context, repo *repository.Container, cache *RuntimeCacheService, id string) (*model.Library, error) {
	key := repo.ReadCacheKey() + "library:" + id
	var cached model.Library
	if cache.GetJSON(ctx, key, &cached) {
		return &cached, nil
	}
	library, err := repo.Library.FindBasicByID(ctx, id)
	if err == nil && library != nil {
		cache.SetJSON(ctx, key, library, libraryBasicCacheTTL)
	}
	return library, err
}

// ListLibrariesBasic 不加载目录；权限计算须直接读仓储，不能叠加库缓存的陈旧窗口。
func ListLibrariesBasic(ctx context.Context, repo *repository.Container, cache *RuntimeCacheService) ([]model.Library, error) {
	key := repo.ReadCacheKey() + "libraries"
	var cached []model.Library
	if cache.GetJSON(ctx, key, &cached) {
		return cached, nil
	}
	libraries, err := repo.Library.ListBasic(ctx)
	if err == nil {
		cache.SetJSON(ctx, key, libraries, libraryBasicCacheTTL)
	}
	return libraries, err
}
