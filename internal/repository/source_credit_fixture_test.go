package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"strconv"
	"time"
)

// SaveSourceCreditsFixture 为读取测试构造 TMDb 来源关系。
func (r *PersonRepository) SaveSourceCreditsFixture(ctx context.Context, metadataID string, loaded []string, inputs []CreditInput) error {
	if len(loaded) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.MetadataItem
		if err := tx.First(&item, "id=?", metadataID).Error; err != nil {
			return err
		}
		if item.Kind == "episode" {
			return nil
		}
		if err := testdb.SaveMetadataPresentationFixture(tx, item); err != nil {
			return err
		}
		var subject model.TMDbSubject
		if err := tx.Table("tmdb_subjects s").Select("s.*").Joins("JOIN metadata_identifiers b ON b.provider='tmdb' AND b.entity_kind=s.kind AND b.external_id=s.tmdb_id::text").Where("b.metadata_id=?", metadataID).Take(&subject).Error; err != nil {
			return err
		}
		body := map[string]any{}
		for _, typ := range loaded {
			if typ == model.CreditTypeActor {
				body["cast"] = []any{}
			} else {
				body["crew"] = []any{}
			}
		}
		for _, input := range inputs {
			id, err := strconv.Atoi(input.ExternalID)
			if err != nil {
				return fmt.Errorf("source person ID: %w", err)
			}
			row := map[string]any{"id": id, "name": input.Name, "character": input.OriginalRole, "order": input.SortOrder}
			group := "cast"
			if input.Type != model.CreditTypeActor {
				group = "crew"
				row["job"] = input.Type
				if input.Type == model.CreditTypeWriter {
					row["department"] = "Writing"
				}
			}
			body[group] = append(body[group].([]any), row)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		var doc tmdbObject
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		return saveTMDbCredits(tx, &subject, nil, doc, "subject", time.Now().UTC(), false)
	})
}
