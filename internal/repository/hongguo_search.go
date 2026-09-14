package repository

import (
	"context"
	"encoding/json"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// SearchResults 按官网顺序合并本地资料与图片标识，搜索本身不创建目录或详情。
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
	return rows, nil
}
