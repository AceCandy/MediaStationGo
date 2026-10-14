package testdb

import (
	"encoding/json"
	"hash/crc32"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveProviderDataFixture 为消费端测试构造中心资料和来源关联，不创建本地正文副本。
// 正文可故意缺字段或格式错误，以验证消费端的坏资料隔离；接口保存校验由 repository 测试覆盖。
func SaveProviderDataFixture(db *gorm.DB, data model.ProviderData) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var item model.MetadataItem
		if err := tx.First(&item, "id=?", data.MetadataID).Error; err != nil {
			return err
		}
		var body map[string]json.RawMessage
		_ = json.Unmarshal([]byte(data.Payload), &body)
		var rawID int
		_ = json.Unmarshal(body["id"], &rawID)
		var seed func(model.MetadataItem, int) (*model.TMDbSubject, error)
		seed = func(local model.MetadataItem, suggested int) (*model.TMDbSubject, error) {
			var binding model.MetadataIdentifier
			err := tx.Where("metadata_id=? AND provider='tmdb' AND entity_kind=?", local.ID, local.Kind).Order("id").Take(&binding).Error
			id := suggested
			if err == nil {
				id, err = strconv.Atoi(binding.ExternalID)
				if err != nil {
					return nil, err
				}
			} else if err != gorm.ErrRecordNotFound {
				return nil, err
			}
			if id <= 0 {
				id = int(crc32.ChecksumIEEE([]byte(local.ID))%2000000000) + 1
			}
			if binding.ID == "" {
				binding = model.MetadataIdentifier{MetadataID: local.ID, Provider: "tmdb", EntityKind: local.Kind, ExternalID: strconv.Itoa(id)}
				if err := tx.Create(&binding).Error; err != nil {
					return nil, err
				}
			}
			row := model.TMDbSubject{Kind: local.Kind, TMDbID: id, Fields: "{}"}
			if local.ParentID != nil {
				var parent model.MetadataItem
				if err := tx.First(&parent, "id=?", *local.ParentID).Error; err != nil {
					return nil, err
				}
				sourceParent, err := seed(parent, 0)
				if err != nil {
					return nil, err
				}
				row.ParentID = &sourceParent.ID
				number := local.SeasonNum
				if local.Kind == "episode" {
					number = local.EpisodeNum
				}
				row.Number = &number
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "kind"}, {Name: "tmdb_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
				return nil, err
			}
			var persisted model.TMDbSubject
			if err := tx.Where("kind=? AND tmdb_id=?", local.Kind, id).Take(&persisted).Error; err != nil {
				return nil, err
			}
			return &persisted, nil
		}
		owner, err := seed(item, rawID)
		if err != nil {
			return err
		}
		if body != nil && rawID <= 0 {
			body["id"], _ = json.Marshal(owner.TMDbID)
			encoded, err := json.Marshal(body)
			if err != nil {
				return err
			}
			data.Payload = string(encoded)
		}
		payload := model.TMDbPayload{SubjectID: &owner.ID, Component: "detail", Language: "und", Body: data.Payload, Origin: "api", Quality: 3, FetchedAt: data.FetchedAt}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "subject_id"}, {Name: "component"}, {Name: "language"}}, TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "subject_id IS NOT NULL"}}}, DoUpdates: clause.AssignmentColumns([]string{"body", "fetched_at"})}).Create(&payload).Error
	})
}
