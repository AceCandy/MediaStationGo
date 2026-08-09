package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (e *EmbyService) seriesPayload(ctx context.Context, group embySeriesGroup, userID string) map[string]any {
	if metadata, err := e.repo.Metadata.FindByID(ctx, group.ID); err == nil && metadata != nil {
		group.Name = metadata.Title
		group.Overview = metadata.Overview
		group.Rating = metadata.Rating
		group.Year = metadata.Year
		group.ReleaseDate = metadata.ReleaseDate
	}
	group.PosterURL = e.metadataArtworkURL(ctx, group.ID, model.ArtworkTypePoster)
	group.BackdropURL = e.metadataArtworkURL(ctx, group.ID, model.ArtworkTypeBackdrop)
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
		"People":             e.peopleForMetadata(ctx, group.ID),
		"ProviderIds":        e.metadataProviderIDs(ctx, group.ID),
		"UserData":           userData,
	}
	if premiered, ok := embyPremiereDate(group.ReleaseDate); ok {
		item["PremiereDate"] = premiered
	}
	return item
}

func (e *EmbyService) seasonPayload(ctx context.Context, season embySeasonGroup, userID string) map[string]any {
	var overview string
	var rating float32
	if metadata, err := e.repo.Metadata.FindByID(ctx, season.ID); err == nil && metadata != nil {
		season.Name = metadata.Title
		overview = metadata.Overview
		rating = metadata.Rating
	}
	seasonPoster := e.metadataArtworkURL(ctx, season.ID, model.ArtworkTypePoster)
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
	if seasonPoster != "" {
		imageTags["Primary"] = season.ID
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
		"Overview":          overview,
		"CommunityRating":   rating,
		"ChildCount":        len(season.Episodes),
		"ImageTags":         imageTags,
		"BackdropImageTags": []string{},
		"People":            e.peopleForMetadata(ctx, season.ID),
		"ProviderIds":       e.metadataProviderIDs(ctx, season.ID),
		"UserData":          userData,
	}
}

func (e *EmbyService) metadataProviderIDs(ctx context.Context, metadataID string) map[string]string {
	out := map[string]string{}
	if e == nil || e.repo == nil || e.repo.Metadata == nil {
		return out
	}
	identifiers, err := e.repo.Metadata.ListIdentifiers(ctx, metadataID)
	if err != nil {
		return out
	}
	for _, identifier := range identifiers {
		switch identifier.Provider {
		case "tmdb":
			out["Tmdb"] = identifier.ExternalID
		case "imdb":
			out["Imdb"] = identifier.ExternalID
		case "thetvdb":
			out["Tvdb"] = identifier.ExternalID
		case "bangumi":
			out["Bangumi"] = identifier.ExternalID
		}
	}
	return out
}
