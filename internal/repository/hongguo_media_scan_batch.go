package repository

import (
	"context"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

type hongGuoScanEpisodeKey struct {
	workID string
	number int
}
type hongGuoScanLookup struct {
	libraries map[string]bool
	works     map[string]model.HongGuoWork
	episodes  map[hongGuoScanEpisodeKey]model.HongGuoEpisode
}

// UpsertHongGuoScanBatch 在短事务内复用本批资料；失败还原输入，调用方可逐条重试。
// 返回每项是否实际新增，供并发写入下准确计数；搜索仅在提交后发布。
func (r *MediaRepository) UpsertHongGuoScanBatch(ctx context.Context, media []*model.Media) ([]bool, error) {
	if len(media) == 0 {
		return nil, nil
	}
	if len(media) > 100 {
		return nil, errors.New("hongguo scan batch exceeds 100 files")
	}
	originals := make([]model.Media, len(media))
	sources := make([]string, 0, len(media))
	for i, m := range media {
		if m == nil || m.CatalogSource != model.TaskSystemHongGuo {
			return nil, errors.New("hongguo scan batch requires hongguo media")
		}
		originals[i] = *m
		sources = append(sources, m.LookupCatalogID)
	}
	created := make([]bool, len(media))
	refreshes := make([]func(), 0, len(media))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		enabled, err := New(tx).Setting.Get(ctx, "hongguo.enabled")
		if err != nil {
			return err
		}
		if enabled == "false" {
			return errors.New("HongGuoDB 已停用，文件绑定保持不变")
		}
		lookup := &hongGuoScanLookup{libraries: map[string]bool{}, works: map[string]model.HongGuoWork{}, episodes: map[hongGuoScanEpisodeKey]model.HongGuoEpisode{}}
		var works []model.HongGuoWork
		if err := tx.Select("id, source_id").Where("source_id IN ? AND summary_only = false", uniqueNonEmptyStrings(sources)).Find(&works).Error; err != nil {
			return err
		}
		for _, work := range works {
			lookup.works[work.SourceID] = work
		}
		coords := make([][]any, 0, len(media))
		for _, m := range media {
			if work := lookup.works[m.LookupCatalogID]; work.ID != "" && m.SeasonNum == 1 && m.EpisodeNum > 0 {
				coords = append(coords, []any{work.ID, m.EpisodeNum})
			}
		}
		if len(coords) > 0 {
			var episodes []model.HongGuoEpisode
			if err := tx.Select("id, work_id, number").Where("(work_id, number) IN ?", coords).Find(&episodes).Error; err != nil {
				return err
			}
			for _, episode := range episodes {
				lookup.episodes[hongGuoScanEpisodeKey{episode.WorkID, episode.Number}] = episode
			}
		}
		for i, m := range media {
			refresh := func() {}
			if err := r.upsertHongGuoMediaTx(ctx, tx, m, lookup, &refresh, &created[i]); err != nil {
				return err
			}
			refreshes = append(refreshes, refresh)
		}
		return nil
	})
	if err != nil {
		for i, m := range media {
			*m = originals[i]
		}
		return nil, err
	}
	for _, refresh := range refreshes {
		refresh()
	}
	return created, nil
}
