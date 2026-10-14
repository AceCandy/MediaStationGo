package testdb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var presentationFixtureID atomic.Int64

// SaveMetadataPresentationFixture 为展示测试显式建立来源资料；身份字段由调用方预先保存。
func SaveMetadataPresentationFixture(db *gorm.DB, item model.MetadataItem) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var identifiers []model.MetadataIdentifier
		if err := tx.Where("metadata_id=? AND provider='tmdb' AND entity_kind=?", item.ID, item.Kind).Find(&identifiers).Error; err != nil {
			return err
		}
		id := int(900000000 + presentationFixtureID.Add(1))
		if len(identifiers) == 1 {
			var err error
			id, err = strconv.Atoi(identifiers[0].ExternalID)
			if err != nil {
				return err
			}
		} else if len(identifiers) == 0 {
			if err := tx.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "tmdb", EntityKind: item.Kind, ExternalID: strconv.Itoa(id)}).Error; err != nil {
				return err
			}
		} else {
			return fmt.Errorf("ambiguous fixture source: %s", item.ID)
		}
		release := item.ReleaseDate
		if release == "" && item.Year > 0 {
			release = fmt.Sprintf("%04d", item.Year)
		}
		fields, err := json.Marshal(map[string]any{"runtime": item.RuntimeSec / 60, "spoken_languages": item.Languages, "production_countries": item.Countries, "genres": item.Genres})
		if err != nil {
			return err
		}
		rating := float64(item.Rating)
		subject := model.TMDbSubject{Kind: item.Kind, TMDbID: id, Title: item.Title, OriginalName: item.OriginalName, Overview: item.Overview, ReleaseDate: release, Rating: &rating, Fields: string(fields)}
		if item.ParentID != nil {
			var parent model.TMDbSubject
			lookup := func() error {
				return tx.Table("tmdb_subjects subject").Select("subject.*").Joins("JOIN metadata_identifiers binding ON binding.provider='tmdb' AND binding.entity_kind=subject.kind AND binding.external_id=subject.tmdb_id::text").Where("binding.metadata_id=?", *item.ParentID).Take(&parent).Error
			}
			if err := lookup(); err == gorm.ErrRecordNotFound {
				var local model.MetadataItem
				if err := tx.First(&local, "id=?", *item.ParentID).Error; err != nil {
					return err
				}
				if err := SaveMetadataPresentationFixture(tx, local); err != nil {
					return err
				}
				if err := lookup(); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			subject.ParentID = &parent.ID
			number := item.SeasonNum
			if item.Kind == "episode" {
				number = item.EpisodeNum
			}
			subject.Number = &number
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "kind"}, {Name: "tmdb_id"}}, DoUpdates: clause.AssignmentColumns([]string{"title", "original_name", "overview", "release_date", "rating", "fields"})}).Create(&subject).Error
	})
}

// SeedMetadataPresentationTitles 为大批量查询测试按 SELECT id,title 的结果建立真实来源层级。
func SeedMetadataPresentationTitles(db *gorm.DB, titleQuery string, args ...any) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`WITH titles AS (`+titleQuery+`)
INSERT INTO tmdb_subjects(id,kind,tmdb_id,title,parent_id,number,fields)
SELECT m.id,m.kind,GREATEST(900000000,COALESCE((SELECT MAX(tmdb_id) FROM tmdb_subjects),0))+ROW_NUMBER() OVER(ORDER BY m.id),titles.title,m.parent_id,
CASE WHEN m.kind='season' THEN COALESCE(m.season_num,0) WHEN m.kind='episode' THEN COALESCE(m.episode_num,0) END,'{}'
FROM metadata_items m JOIN titles ON titles.id=m.id`, args...).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO metadata_identifiers(id,metadata_id,provider,entity_kind,external_id)
SELECT m.id,m.id,'tmdb',m.kind,s.tmdb_id::text FROM metadata_items m JOIN tmdb_subjects s ON s.id=m.id
WHERE NOT EXISTS (SELECT 1 FROM metadata_identifiers b WHERE b.metadata_id=m.id AND b.provider='tmdb' AND b.entity_kind=m.kind)`).Error
	})
}

// MetadataFixtureSQL 同批建立身份和来源，供大库查询测试保留原始数据分布。
// columns 和 rows 仅接受测试中固定 SQL，运行时业务不得调用。
func MetadataFixtureSQL(columns, rows string) string {
	identity := make([]string, 0)
	for _, column := range strings.Split(columns, ",") {
		column = strings.TrimSpace(column)
		switch column {
		case "title", "original_name", "overview", "rating", "runtime_sec", "year", "release_date", "languages", "countries", "genres":
		default:
			identity = append(identity, column)
		}
	}
	fields := strings.Join(identity, ",")
	return `WITH fixture(` + columns + `) AS (` + rows + `),
identities AS (INSERT INTO metadata_items(` + fields + `) SELECT ` + fields + ` FROM fixture RETURNING id),
subjects AS (INSERT INTO tmdb_subjects(id,kind,tmdb_id,parent_id,number,title,original_name,overview,rating,release_date,fields)
SELECT f.id,f.kind,GREATEST(900000000,COALESCE((SELECT MAX(tmdb_id) FROM tmdb_subjects),0))+ROW_NUMBER() OVER(ORDER BY f.id),
 doc->>'parent_id',CASE WHEN f.kind='season' THEN COALESCE((doc->>'season_num')::integer,0)
 WHEN f.kind='episode' THEN COALESCE((doc->>'episode_num')::integer,0) END,
 COALESCE(doc->>'title',''),COALESCE(doc->>'original_name',''),COALESCE(doc->>'overview',''),
 COALESCE((doc->>'rating')::real,0),COALESCE(NULLIF(doc->>'release_date',''),NULLIF(doc->>'year','0'),''),'{}'
FROM fixture f CROSS JOIN LATERAL (SELECT to_jsonb(f) doc) parsed JOIN identities i ON i.id=f.id
RETURNING id,kind,tmdb_id)
INSERT INTO metadata_identifiers(id,metadata_id,provider,entity_kind,external_id)
SELECT id,id,'tmdb',kind,tmdb_id::text FROM subjects`
}

// SaveMetadataArtworkFixture 将测试图片放到当前关联的来源，而非媒体选图副本。
func SaveMetadataArtworkFixture(db *gorm.DB, metadataID, artworkType, assetID string) error {
	var subject model.TMDbSubject
	if err := db.Table("tmdb_subjects s").Select("s.*").Joins("JOIN metadata_identifiers b ON b.provider='tmdb' AND b.entity_kind=s.kind AND b.external_id=s.tmdb_id::text").Where("b.metadata_id=?", metadataID).Take(&subject).Error; err != nil {
		return err
	}
	artwork := model.TMDbArtwork{SubjectID: subject.ID, Type: artworkType, AssetID: &assetID, Status: "ready"}
	return db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "subject_id"}, {Name: "type"}}, DoUpdates: clause.AssignmentColumns([]string{"asset_id", "status"})}).Create(&artwork).Error
}
