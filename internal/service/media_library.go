package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ListLibraries returns every library configured on the server.
func (s *MediaService) ListLibraries(ctx context.Context) ([]model.Library, error) {
	return s.repo.Library.List(ctx)
}

// DeleteLibrary removes a library and its media rows. The on-disk files are
// left untouched.
func (s *MediaService) DeleteLibrary(ctx context.Context, id string) error {
	lib, err := s.repo.Library.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if lib != nil {
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			if err := s.repo.Media.PurgeByLibrary(ctx, id); err != nil {
				return err
			}
			_ = s.repo.DB.WithContext(ctx).Where("library_id = ?", id).Delete(&model.LibraryRoot{}).Error
			err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Library{}).Error
			if err == nil {
				s.invalidateMediaCache(ctx)
			}
			return err
		}
	}
	_ = s.repo.DB.WithContext(ctx).Where("library_id = ?", id).Delete(&model.LibraryRoot{}).Error
	if err := s.repo.Media.DeleteByLibrary(ctx, id); err != nil {
		return err
	}
	err = s.repo.Library.Delete(ctx, id)
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}
