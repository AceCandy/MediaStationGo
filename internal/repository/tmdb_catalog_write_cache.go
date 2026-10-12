package repository

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

type tmdbWriteCacheKey struct{}

// tmdbWriteCache 只在持有来源写入锁的单个事务内有效，回滚后随事务丢弃。
type tmdbWriteCache struct {
	conn     gorm.ConnPool
	people   map[int]model.TMDbPersonProfile
	subjects map[string]model.TMDbSubject
}

func tmdbSourceSeason(tx *gorm.DB, seriesID, number int) (model.TMDbSubject, error) {
	var row model.TMDbSubject
	err := tx.Table("tmdb_subjects AS season").Select("season.*").
		Joins("JOIN tmdb_subjects AS series ON series.id=season.parent_id").
		Where("season.kind='season' AND season.number=? AND series.kind='series' AND series.tmdb_id=?", number, seriesID).Take(&row).Error
	return row, err
}

func tmdbTransactionCache(tx *gorm.DB) *tmdbWriteCache {
	cache, _ := tx.Statement.Context.Value(tmdbWriteCacheKey{}).(*tmdbWriteCache)
	if cache == nil || cache.conn != tx.Statement.ConnPool {
		return nil
	}
	return cache
}

func preloadTMDbCreditPeople(tx *gorm.DB, rows []tmdbObject) error {
	cache := tmdbTransactionCache(tx)
	if cache == nil {
		return nil
	}
	ids := []int{}
	seen := map[int]bool{}
	for _, row := range rows {
		id := tmdbInt(row, "id")
		if _, loaded := cache.people[id]; id > 0 && !loaded && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	for start := 0; start < len(ids); start += 500 {
		batch := ids[start:min(start+500, len(ids))]
		var profiles []model.TMDbPersonProfile
		if err := tx.Where("tmdb_id IN ?", batch).Find(&profiles).Error; err != nil {
			return err
		}
		for _, id := range batch {
			cache.people[id] = model.TMDbPersonProfile{}
		}
		for _, profile := range profiles {
			cache.people[profile.TMDbID] = profile
		}
	}
	return nil
}

// 单份来源响应在事务和写锁内保存，缓存随事务丢弃。
func withTMDbWriteTransaction(ctx context.Context, db *gorm.DB, save func(*gorm.DB) error) error {
	scoped := db.WithContext(ctx)
	if tmdbTransactionCache(scoped) != nil {
		return save(scoped)
	}
	return scoped.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('tmdb_catalog_write'))").Error; err != nil {
			return err
		}
		cache := &tmdbWriteCache{conn: tx.Statement.ConnPool, people: map[int]model.TMDbPersonProfile{}, subjects: map[string]model.TMDbSubject{}}
		return save(tx.WithContext(context.WithValue(tx.Statement.Context, tmdbWriteCacheKey{}, cache)))
	})
}
