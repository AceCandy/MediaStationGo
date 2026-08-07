package service

import (
	"context"
)

func (e *EmbyService) seriesPayload(ctx context.Context, group embySeriesGroup, userID string) map[string]any {
	e.rememberSeriesGroup(group)
	target := embyItemTarget{ItemID: group.ID, MetadataID: group.ID}
	if len(group.Episodes) > 0 {
		target.MediaID = group.Episodes[0].ID
	}
	favorite, positionMs := e.userDataForTarget(ctx, userID, target)
	userData := emptyUserData()
	userData["IsFavorite"] = favorite
	userData["PlaybackPositionTicks"] = positionMs * 10_000
	imageTags := map[string]string{}
	backdropTags := []string{}
	if group.PosterURL != "" {
		imageTags["Primary"] = group.ID
	}
	if group.BackdropURL != "" {
		backdropTags = append(backdropTags, group.ID+"-bd")
	}
	item := map[string]any{
		"Id":                 group.ID,
		"Name":               group.Name,
		"ServerId":           embyServerID,
		"Type":               "Series",
		"MediaType":          "Video",
		"IsFolder":           true,
		"ParentId":           group.LibraryID,
		"ProductionYear":     group.Year,
		"Overview":           group.Overview,
		"CommunityRating":    group.Rating,
		"RecursiveItemCount": len(group.Episodes),
		"ChildCount":         len(e.seasonsForSeries(group)),
		"DateCreated":        formatEmbyDateTime(group.CreatedAt),
		"ImageTags":          imageTags,
		"BackdropImageTags":  backdropTags,
		"ProviderIds": map[string]string{
			"Tmdb":    intToStr(group.TMDbID),
			"Bangumi": intToStr(group.BangumiID),
		},
		"UserData": userData,
	}
	if premiered, ok := embyPremiereDate(group.ReleaseDate); ok {
		item["PremiereDate"] = premiered
	}
	return item
}

func (e *EmbyService) seasonPayload(ctx context.Context, season embySeasonGroup, userID string) map[string]any {
	e.rememberSeasonGroup(season)
	mediaID := ""
	if len(season.Episodes) > 0 {
		mediaID = season.Episodes[0].ID
	}
	favorite, positionMs := e.userDataForTarget(ctx, userID, embyItemTarget{ItemID: season.ID, MetadataID: season.ID, MediaID: mediaID})
	userData := emptyUserData()
	userData["IsFavorite"] = favorite
	userData["PlaybackPositionTicks"] = positionMs * 10_000
	imageTags := map[string]string{}
	backdropTags := []string{}
	if season.Series.PosterURL != "" {
		imageTags["Primary"] = season.ID
	}
	if season.Series.BackdropURL != "" {
		backdropTags = append(backdropTags, season.ID+"-bd")
	}
	return map[string]any{
		"Id":                season.ID,
		"Name":              season.Name,
		"ServerId":          embyServerID,
		"Type":              "Season",
		"MediaType":         "Video",
		"IsFolder":          true,
		"ParentId":          season.SeriesID,
		"SeriesId":          season.SeriesID,
		"SeriesName":        season.Series.Name,
		"IndexNumber":       season.SeasonNum,
		"ChildCount":        len(season.Episodes),
		"ImageTags":         imageTags,
		"BackdropImageTags": backdropTags,
		"UserData":          userData,
	}
}
