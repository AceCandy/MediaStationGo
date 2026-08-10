package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *MediaService) attachLibraryMetadata(ctx context.Context, items []model.Media) {
	if s == nil || s.repo == nil || s.repo.Library == nil || len(items) == 0 {
		return
	}
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		return
	}
	byID := make(map[string]model.Library, len(libs))
	for i := range libs {
		libs[i] = normalizeLocalLibraryPathForDisplay(libs[i])
		lib := libs[i]
		byID[lib.ID] = lib
	}
	for i := range items {
		if lib, ok := byID[items[i].LibraryID]; ok {
			items[i].LibraryName = lib.Name
			items[i].LibraryPath = lib.Path
			items[i].DisplayLibraryID = lib.ID
			items[i].DisplayLibraryName = lib.Name
			items[i].DisplayLibraryPath = lib.Path
		}
	}
}

func (s *MediaService) attachLibraryMetadataViews(ctx context.Context, items []model.MediaView) {
	rows := mediaViewsAsMedia(items)
	s.attachLibraryMetadata(ctx, rows)
	for i := range items {
		items[i].LibraryName = rows[i].LibraryName
		items[i].LibraryPath = rows[i].LibraryPath
		items[i].DisplayLibraryID = rows[i].DisplayLibraryID
		items[i].DisplayLibraryName = rows[i].DisplayLibraryName
		items[i].DisplayLibraryPath = rows[i].DisplayLibraryPath
	}
}

func mediaViewsAsMedia(items []model.MediaView) []model.Media {
	rows := make([]model.Media, len(items))
	for i := range items {
		rows[i] = items[i].Media
		rows[i].SeriesID = items[i].SeriesID
		rows[i].SeriesTitle = items[i].SeriesTitle
		rows[i].Title = items[i].Title
		rows[i].OriginalName = items[i].OriginalName
		rows[i].PosterURL = items[i].PosterURL
		rows[i].BackdropURL = items[i].BackdropURL
		rows[i].Overview = items[i].Overview
		rows[i].Rating = items[i].Rating
		rows[i].Year = items[i].Year
		rows[i].ReleaseDate = items[i].ReleaseDate
		rows[i].TMDbID = items[i].TMDbID
		rows[i].BangumiID = items[i].BangumiID
		rows[i].DoubanID = items[i].DoubanID
		rows[i].TheTVDBID = items[i].TheTVDBID
		rows[i].Languages = items[i].Languages
		rows[i].Countries = items[i].Countries
		rows[i].Genres = items[i].Genres
		rows[i].NSFW = items[i].NSFW
	}
	return rows
}

func normalizeLocalLibraryPathForDisplay(lib model.Library) model.Library {
	lib.Path = resolveMappedDestinationPath(lib.Path)
	for i := range lib.Roots {
		lib.Roots[i].Path = resolveMappedDestinationPath(lib.Roots[i].Path)
	}
	return lib
}
