package testdb

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// SaveSourceCreditFixture 为消费端测试建立当前来源人物关系，不创建媒体关系副本。
func SaveSourceCreditFixture(db *gorm.DB, credit *model.MetadataCredit) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var item model.MetadataItem
		if err := tx.Table("metadata_presentations").First(&item, "id=?", credit.MetadataID).Error; err != nil {
			return err
		}
		if item.Kind == "episode" {
			if item.ParentID == nil {
				return errors.New("episode fixture missing season")
			}
			parentID := *item.ParentID
			item = model.MetadataItem{}
			if err := tx.Table("metadata_presentations").First(&item, "id=?", parentID).Error; err != nil {
				return err
			}
		}
		if err := SaveMetadataPresentationFixture(tx, item); err != nil {
			return err
		}
		var subject model.TMDbSubject
		if err := tx.Table("tmdb_subjects s").Select("s.*").Joins("JOIN metadata_identifiers b ON b.provider='tmdb' AND b.entity_kind=s.kind AND b.external_id=s.tmdb_id::text").Where("b.metadata_id=?", item.ID).Take(&subject).Error; err != nil {
			return err
		}
		var profile model.TMDbPersonProfile
		err := tx.Where("person_id=?", credit.PersonID).First(&profile).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var person model.Person
			if err := tx.First(&person, "id=?", credit.PersonID).Error; err != nil {
				return err
			}
			profile = model.TMDbPersonProfile{TMDbID: int(900000000 + presentationFixtureID.Add(1)), PersonID: person.ID, Name: person.Name, OriginalName: person.OriginalName, Biography: person.Overview}
			if err := tx.Create(&profile).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		row := model.TMDbCredit{PermanentBase: model.PermanentBase{ID: credit.ID}, SubjectID: subject.ID, PersonProfileID: profile.ID, Origin: "subject", Category: "cast", Character: credit.OriginalRole, SortOrder: credit.SortOrder}
		if row.Character == "" {
			row.Character = credit.Role
		}
		if credit.Type == model.CreditTypeDirector || credit.Type == model.CreditTypeWriter {
			row.Category = "crew"
			row.Job = credit.Type
		}
		if credit.Type == model.CreditTypeWriter {
			row.Department = "Writing"
		}
		row.CreditKey = fmt.Sprintf("%x", sha256.Sum256([]byte(credit.MetadataID+"\x00"+credit.Type+"\x00"+row.Character)))
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if credit.Role != "" && credit.Role != row.Character {
			cache := model.TranslationCache{Kind: "role", ContextKey: subject.ID, SourceText: row.Character, TargetLanguage: "zh-CN", PromptVersion: "fixture", TranslatedText: credit.Role}
			if err := tx.Create(&cache).Error; err != nil {
				return err
			}
		}
		credit.ID = row.ID
		credit.SourceSubjectID = subject.ID
		return nil
	})
}
