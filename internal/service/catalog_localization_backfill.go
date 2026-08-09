package service

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const catalogSnapshotLocalizationBatchSize = 200

type tmdbCatalogLocalizedSnapshot struct {
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Translations struct {
		Translations []tmdbTranslation `json:"translations"`
	} `json:"translations"`
}

func (s *ScraperService) localizeTMDbCatalogSnapshots(ctx context.Context) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil {
		return nil
	}
	afterID := ""
	for {
		snapshots, err := s.repo.Metadata.ListProviderSnapshotsAfter(ctx, "tmdb", []string{model.MetadataKindSeason, model.MetadataKindEpisode}, afterID, catalogSnapshotLocalizationBatchSize)
		if err != nil {
			return err
		}
		for i := range snapshots {
			if err := s.localizeTMDbCatalogSnapshot(ctx, &snapshots[i]); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if s.log != nil {
					s.log.Warn("catalog snapshot localization item failed", zap.String("metadata_id", snapshots[i].MetadataID), zap.Error(err))
				}
			}
		}
		if len(snapshots) < catalogSnapshotLocalizationBatchSize {
			return nil
		}
		afterID = snapshots[len(snapshots)-1].ID
	}
}

func (s *ScraperService) localizeTMDbCatalogSnapshot(ctx context.Context, snapshot *model.MetadataProviderSnapshot) error {
	if snapshot == nil || snapshot.Metadata.ID == "" {
		return nil
	}
	var payload tmdbCatalogLocalizedSnapshot
	if err := json.Unmarshal([]byte(snapshot.Payload), &payload); err != nil {
		return fmt.Errorf("decode tmdb %s snapshot %s: %w", snapshot.Metadata.Kind, snapshot.MetadataID, err)
	}
	item := &snapshot.Metadata
	number := item.SeasonNum
	if item.Kind == model.MetadataKindEpisode {
		number = item.EpisodeNum
	}
	title := preferredTMDbEntityTitle(payload.Name, payload.Translations.Translations, item.Kind, number)
	overview := preferredTMDbEntityOverview(payload.Overview, payload.Translations.Translations)
	changed := false
	if item.Title != title {
		item.Title = title
		changed = true
	}
	if item.Kind == model.MetadataKindEpisode && item.OriginalName != "" {
		item.OriginalName = ""
		changed = true
	}
	if item.Overview != overview {
		item.Overview = overview
		changed = true
	}
	if !changed {
		return nil
	}
	return s.repo.Metadata.Update(ctx, item)
}
