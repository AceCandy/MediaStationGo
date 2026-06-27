package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (c *Container) NormalizeCloudLibraryTypes(ctx context.Context) error {
	if c == nil || c.Repo == nil || c.Repo.Library == nil || c.Repo.DB == nil {
		return nil
	}
	libs, err := c.Repo.Library.List(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		info, ok := ParseCloudLibraryMount(lib.Path)
		if !ok {
			continue
		}
		want := InferCloudMountMediaType(info.DisplayDir, lib.Name)
		if want == "" || want == lib.Type {
			continue
		}
		if err := c.Repo.DB.WithContext(ctx).
			Model(&model.Library{}).
			Where("id = ?", lib.ID).
			Update("type", want).Error; err != nil {
			return err
		}
	}
	return nil
}
