package repository

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HongGuoGroupInput 使用源作品 ID 和人工季号，不推测标题中的季关系。
type HongGuoGroupInput struct {
	SourceID     string `json:"source_id"`
	SeasonNumber int    `json:"season_number"`
}

func (r *HongGuoRepository) SaveGroup(ctx context.Context, id, title string, inputs []HongGuoGroupInput) (*model.HongGuoGroup, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(inputs) == 0 || len(inputs) > 1000 {
		return nil, errors.New("聚合标题和成员必填，成员不得超过 1000")
	}
	seasons := map[int]bool{}
	sourceIDs := map[string]bool{}
	for _, in := range inputs {
		if !hongguo.ValidID(in.SourceID) || in.SeasonNumber < 1 || in.SeasonNumber > 1000 || seasons[in.SeasonNumber] || sourceIDs[in.SourceID] {
			return nil, errors.New("作品 ID 或季号无效、重复")
		}
		seasons[in.SeasonNumber] = true
		sourceIDs[in.SourceID] = true
	}
	ordered := append([]HongGuoGroupInput(nil), inputs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].SourceID < ordered[j].SourceID })
	group := model.HongGuoGroup{Title: title}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id != "" {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, "id = ?", id).Error; err != nil {
				return err
			}
			group.Title = title
			if err := tx.Model(&group).Update("title", title).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&group).Error; err != nil {
			return err
		}
		members := make([]model.HongGuoGroupMember, 0, len(ordered))
		for _, in := range ordered {
			var work model.HongGuoWork
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", in.SourceID).First(&work).Error; err != nil {
				return err
			}
			if work.Kind != model.MetadataKindSeries {
				return errors.New("电影不能作为聚合季")
			}
			var count int64
			if err := tx.Model(&model.HongGuoGroupMember{}).Where("work_id = ? AND group_id <> ?", work.ID, group.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("作品已属于其他聚合，请先解除原关系")
			}
			members = append(members, model.HongGuoGroupMember{WorkID: work.ID, GroupID: group.ID, SeasonNumber: in.SeasonNumber})
		}
		if err := tx.Where("group_id = ?", group.ID).Delete(&model.HongGuoGroupMember{}).Error; err != nil {
			return err
		}
		return tx.CreateInBatches(&members, 500).Error
	})
	return &group, err
}

func (r *HongGuoRepository) DeleteGroup(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.HongGuoGroup{}).Error
}

type HongGuoGroupDetail struct {
	model.HongGuoGroup
	Members []HongGuoGroupWork `json:"members"`
}

type HongGuoGroupWork struct {
	model.HongGuoWork
	SeasonNumber int `json:"season_number"`
}

func (r *HongGuoRepository) Group(ctx context.Context, id string) (*HongGuoGroupDetail, error) {
	group := &HongGuoGroupDetail{Members: []HongGuoGroupWork{}}
	if err := r.db.WithContext(ctx).First(&group.HongGuoGroup, "id = ?", id).Error; err != nil {
		return nil, err
	}
	err := r.db.WithContext(ctx).Table("hongguo_works AS w").Joins("JOIN hongguo_group_members AS gm ON gm.work_id = w.id").Where("gm.group_id = ?", id).Select("w.*, gm.season_number").Order("gm.season_number").Scan(&group.Members).Error
	return group, err
}
