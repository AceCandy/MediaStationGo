package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/clause"
)

// QueueMissingSearchResults 只登记缺失详情的搜索摘要，复用资料刷新队列；重复搜索不覆盖分类或重试状态。
func (r *HongGuoRepository) QueueMissingSearchResults(ctx context.Context, results []HongGuoListWork) error {
	rows := make([]model.HongGuoDiscovery, 0, len(results))
	for _, item := range results {
		if item.Hydrated {
			continue
		}
		if !hongguo.ValidID(item.SourceID) {
			return errors.New("红果搜索作品 ID 无效")
		}
		rows = append(rows, model.HongGuoDiscovery{SourceID: item.SourceID, SourceCategory: item.SourceCategory, Title: item.Title, Overview: item.Overview, EpisodeCount: item.EpisodeCount, UpdateText: item.UpdateText})
	}
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoNothing: true}).CreateInBatches(&rows, 100).Error
}

// SearchResults 只读合并本地资料与图片标识并保留官网顺序；摘要入队由调用方另行执行。
func (r *HongGuoRepository) SearchResults(ctx context.Context, remote []hongguo.Work) ([]HongGuoListWork, error) {
	rows := []HongGuoListWork{}
	if len(remote) == 0 {
		return rows, nil
	}
	ids := make([]string, 0, len(remote))
	for _, work := range remote {
		ids = append(ids, work.SourceID)
	}
	var works []model.HongGuoWork
	if err := r.db.WithContext(ctx).Where("source_id IN ?", ids).Find(&works).Error; err != nil {
		return nil, err
	}
	var discoveries []model.HongGuoDiscovery
	if err := r.db.WithContext(ctx).Select("source_id, source_category").Where("source_id IN ?", ids).Find(&discoveries).Error; err != nil {
		return nil, err
	}
	var artwork []model.HongGuoArtwork
	if err := r.db.WithContext(ctx).Select("id, source_id").Where("source_id IN ?", ids).Find(&artwork).Error; err != nil {
		return nil, err
	}
	local := map[string]model.HongGuoWork{}
	for _, work := range works {
		local[work.SourceID] = work
	}
	categories := map[string]string{}
	for _, work := range discoveries {
		categories[work.SourceID] = work.SourceCategory
	}
	images := map[string]string{}
	for _, image := range artwork {
		if image.SourceID != nil {
			images[*image.SourceID] = image.ID
		}
	}
	for _, work := range remote {
		row := HongGuoListWork{HongGuoWork: model.HongGuoWork{SourceID: work.SourceID, SourceCategory: categories[work.SourceID], Title: work.Title, Overview: work.Overview, EpisodeCount: work.EpisodeCount, UpdateText: work.UpdateText}, ArtworkID: images[work.SourceID], TagList: work.Tags}
		if saved, ok := local[work.SourceID]; ok {
			row.HongGuoWork, row.Hydrated = saved, true
			if err := json.Unmarshal([]byte(saved.Tags), &row.TagList); err != nil {
				return nil, err
			}
		}
		if row.SourceCategory == "comic" {
			continue
		}
		if row.TagList == nil {
			row.TagList = []string{}
		}
		rows = append(rows, row)
	}
	return rows, r.loadListBadges(ctx, rows)
}
