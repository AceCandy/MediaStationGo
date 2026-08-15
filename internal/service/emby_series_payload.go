package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (e *EmbyService) seriesPayload(ctx context.Context, group embySeriesGroup, userID string) map[string]any {
	return e.seriesPayloadWithRelations(ctx, group, userID, nil)
}

type embyMetadataRelations struct {
	metadataByID            map[string]model.MetadataItem
	artworkByMetadataID     map[string]map[string]string
	peopleByMetadataID      map[string][]model.EmbyPerson
	providerIDsByMetadataID map[string]map[string]string
	favoriteByMetadataID    map[string]bool
	positionByMetadataID    map[string]int64
	fields                  embyListFields
}

func (e *EmbyService) metadataRelationsForIDs(ctx context.Context, metadataIDs []string, userID string, fields embyListFields) *embyMetadataRelations {
	relations := &embyMetadataRelations{
		metadataByID:            map[string]model.MetadataItem{},
		artworkByMetadataID:     map[string]map[string]string{},
		peopleByMetadataID:      map[string][]model.EmbyPerson{},
		providerIDsByMetadataID: map[string]map[string]string{},
		fields:                  fields,
	}
	if e == nil || e.repo == nil || len(metadataIDs) == 0 {
		return relations
	}
	if e.repo.Metadata != nil {
		if rows, err := e.repo.Metadata.FindByIDs(ctx, metadataIDs); err == nil {
			for _, row := range rows {
				relations.metadataByID[row.ID] = row
			}
		}
		if fields.providerIDs {
			if rows, err := e.repo.Metadata.ListIdentifiersByMetadataIDs(ctx, metadataIDs); err == nil {
				grouped := make(map[string][]model.MetadataIdentifier)
				for _, row := range rows {
					grouped[row.MetadataID] = append(grouped[row.MetadataID], row)
				}
				for id, identifiers := range grouped {
					relations.providerIDsByMetadataID[id] = metadataProviderIDsFromIdentifiers(identifiers)
				}
			}
		}
	}
	if e.repo.Artwork != nil {
		if rows, err := e.repo.Artwork.ListSelectionsByMetadataIDs(ctx, metadataIDs); err == nil {
			for _, row := range rows {
				if relations.artworkByMetadataID[row.MetadataID] == nil {
					relations.artworkByMetadataID[row.MetadataID] = map[string]string{}
				}
				relations.artworkByMetadataID[row.MetadataID][row.ArtworkType] = ArtworkURL(row.AssetID)
			}
		}
	}
	if fields.people && e.repo.Person != nil {
		if rows, err := e.repo.Person.ListCreditsWithPeopleByMetadataIDs(ctx, metadataIDs); err == nil {
			grouped := make(map[string][]model.MetadataCredit)
			for _, row := range rows {
				grouped[row.MetadataID] = append(grouped[row.MetadataID], row)
			}
			for id, credits := range grouped {
				relations.peopleByMetadataID[id] = embyPeopleFromCredits(credits)
			}
		}
	}
	relations.favoriteByMetadataID, relations.positionByMetadataID = e.userDataForMetadataIDs(ctx, userID, metadataIDs)
	return relations
}

func (e *EmbyService) seriesPayloadsWithFields(ctx context.Context, groups []embySeriesGroup, userID string, requestedFields []string) []map[string]any {
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	relations := e.metadataRelationsForIDs(ctx, ids, userID, newEmbyListFields(requestedFields))
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, e.seriesPayloadWithRelations(ctx, group, userID, relations))
	}
	return items
}

func (e *EmbyService) seriesPayloadWithRelations(ctx context.Context, group embySeriesGroup, userID string, relations *embyMetadataRelations) map[string]any {
	var people []model.EmbyPerson
	var providerIDs map[string]string
	var favorite bool
	var positionMs int64
	if relations == nil {
		metadata, _ := e.repo.Metadata.FindByID(ctx, group.ID)
		if metadata != nil {
			group.Name = metadata.Title
			group.Overview = metadata.Overview
			group.Rating = metadata.Rating
			group.Year = metadata.Year
			group.ReleaseDate = metadata.ReleaseDate
		}
		group.PosterURL = e.metadataArtworkURL(ctx, group.ID, model.ArtworkTypePoster)
		group.BackdropURL = e.metadataArtworkURL(ctx, group.ID, model.ArtworkTypeBackdrop)
		target := embyItemTarget{ItemID: group.ID, MetadataID: group.ID}
		if len(group.Episodes) > 0 {
			target.MediaID = group.Episodes[0].ID
		}
		favorite, positionMs = e.userDataForTarget(ctx, userID, target)
		people = e.peopleForMetadata(ctx, group.ID)
		providerIDs = e.metadataProviderIDs(ctx, group.ID)
	} else {
		if metadata, ok := relations.metadataByID[group.ID]; ok {
			group.Name = metadata.Title
			group.Overview = metadata.Overview
			group.Rating = metadata.Rating
			group.Year = metadata.Year
			group.ReleaseDate = metadata.ReleaseDate
		}
		group.PosterURL = relations.artworkByMetadataID[group.ID][model.ArtworkTypePoster]
		group.BackdropURL = relations.artworkByMetadataID[group.ID][model.ArtworkTypeBackdrop]
		favorite = relations.favoriteByMetadataID[group.ID]
		positionMs = relations.positionByMetadataID[group.ID]
		if relations.fields.people {
			people = []model.EmbyPerson{}
			if loaded, ok := relations.peopleByMetadataID[group.ID]; ok {
				people = loaded
			}
		}
		if relations.fields.providerIDs {
			providerIDs = map[string]string{}
			if loaded, ok := relations.providerIDsByMetadataID[group.ID]; ok {
				providerIDs = loaded
			}
		}
	}
	e.rememberSeriesGroup(group)
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
		"UserData":           userData,
	}
	if relations == nil || relations.fields.people {
		item["People"] = people
	}
	if relations == nil || relations.fields.providerIDs {
		item["ProviderIds"] = providerIDs
	}
	if premiered, ok := embyPremiereDate(group.ReleaseDate); ok {
		item["PremiereDate"] = premiered
	}
	return item
}

func (e *EmbyService) seasonPayload(ctx context.Context, season embySeasonGroup, userID string) map[string]any {
	return e.seasonPayloadWithRelations(ctx, season, userID, nil)
}

func (e *EmbyService) seasonPayloadsWithFields(ctx context.Context, seasons []embySeasonGroup, userID string, requestedFields []string) []map[string]any {
	ids := make([]string, 0, len(seasons))
	for _, season := range seasons {
		ids = append(ids, season.ID)
	}
	relations := e.metadataRelationsForIDs(ctx, ids, userID, newEmbyListFields(requestedFields))
	items := make([]map[string]any, 0, len(seasons))
	for _, season := range seasons {
		items = append(items, e.seasonPayloadWithRelations(ctx, season, userID, relations))
	}
	return items
}

func (e *EmbyService) seasonPayloadWithRelations(ctx context.Context, season embySeasonGroup, userID string, relations *embyMetadataRelations) map[string]any {
	var overview string
	var rating float32
	var seasonPoster string
	var people []model.EmbyPerson
	var providerIDs map[string]string
	var favorite bool
	var positionMs int64
	if relations == nil {
		if metadata, err := e.repo.Metadata.FindByID(ctx, season.ID); err == nil && metadata != nil {
			season.Name = metadata.Title
			overview = metadata.Overview
			rating = metadata.Rating
		}
		seasonPoster = e.metadataArtworkURL(ctx, season.ID, model.ArtworkTypePoster)
		mediaID := ""
		if len(season.Episodes) > 0 {
			mediaID = season.Episodes[0].ID
		}
		favorite, positionMs = e.userDataForTarget(ctx, userID, embyItemTarget{ItemID: season.ID, MetadataID: season.ID, MediaID: mediaID})
		people = e.peopleForMetadata(ctx, season.ID)
		providerIDs = e.metadataProviderIDs(ctx, season.ID)
	} else {
		if metadata, ok := relations.metadataByID[season.ID]; ok {
			season.Name = metadata.Title
			overview = metadata.Overview
			rating = metadata.Rating
		}
		seasonPoster = relations.artworkByMetadataID[season.ID][model.ArtworkTypePoster]
		favorite = relations.favoriteByMetadataID[season.ID]
		positionMs = relations.positionByMetadataID[season.ID]
		if relations.fields.people {
			people = []model.EmbyPerson{}
			if loaded, ok := relations.peopleByMetadataID[season.ID]; ok {
				people = loaded
			}
		}
		if relations.fields.providerIDs {
			providerIDs = map[string]string{}
			if loaded, ok := relations.providerIDsByMetadataID[season.ID]; ok {
				providerIDs = loaded
			}
		}
	}
	e.rememberSeasonGroup(season)
	userData := emptyUserData()
	userData["IsFavorite"] = favorite
	userData["PlaybackPositionTicks"] = positionMs * 10_000
	imageTags := map[string]string{}
	if seasonPoster != "" {
		imageTags["Primary"] = season.ID
	}
	item := map[string]any{
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
		"UserData":          userData,
	}
	if relations == nil || relations.fields.people {
		item["People"] = people
	}
	if relations == nil || relations.fields.providerIDs {
		item["ProviderIds"] = providerIDs
	}
	return item
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
	return metadataProviderIDsFromIdentifiers(identifiers)
}

func metadataProviderIDsFromIdentifiers(identifiers []model.MetadataIdentifier) map[string]string {
	out := map[string]string{}
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
