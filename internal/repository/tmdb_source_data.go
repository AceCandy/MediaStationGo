package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// 按本地来源标识读取资料中心正文，不保存本地副本。
const tmdbSourceDataSQL = `(
 SELECT mid.id, mid.created_at, s.updated_at, mid.metadata_id, 'tmdb'::text AS provider,
 p.body || COALESCE(b.body,'{}'::jsonb) AS payload, (p.component='summary') AS degraded,
 LEAST(p.fetched_at,COALESCE(b.fetched_at,p.fetched_at)) AS fetched_at
 FROM metadata_identifiers mid JOIN tmdb_subjects s ON s.kind=mid.entity_kind AND s.tmdb_id=CASE WHEN mid.external_id ~ '^[1-9][0-9]{0,9}$' THEN mid.external_id::bigint END
 JOIN LATERAL (SELECT body,fetched_at,component FROM tmdb_payloads WHERE subject_id=s.id AND component IN ('detail','base','season_episode','summary') AND language IN ('zh-CN','und') ORDER BY quality DESC,fetched_at DESC LIMIT 1) p ON true
 LEFT JOIN LATERAL (SELECT body,fetched_at,component FROM tmdb_payloads WHERE subject_id=s.id AND component IN ('base','season_episode') AND language IN ('zh-CN','und') AND fetched_at>p.fetched_at ORDER BY fetched_at DESC LIMIT 1) b ON true
 WHERE mid.provider='tmdb'
)`

// 一次读取当前本地父链与标识；标识按主键排序选择。
type tmdbLocalIdentity struct {
	ID, Kind                             string
	ParentID, ParentRecordID, ParentRoot *string
	SeasonNum, EpisodeNum, ParentSeason  int
	ExternalID, SeriesID, SeasonID       *string
}

const tmdbLocalIdentitySQL = `SELECT item.id,item.kind,item.parent_id,item.season_num,item.episode_num,
 parent.id AS parent_record_id,parent.parent_id AS parent_root,parent.season_num AS parent_season,
 own.external_id,root.external_id AS series_id,season.external_id AS season_id
 FROM metadata_items item LEFT JOIN metadata_items parent ON parent.id=item.parent_id
 LEFT JOIN LATERAL (SELECT external_id FROM metadata_identifiers WHERE metadata_id=item.id
 AND provider='tmdb' AND entity_kind=item.kind ORDER BY id LIMIT 1) own ON true
 LEFT JOIN LATERAL (SELECT external_id FROM metadata_identifiers
 WHERE metadata_id=CASE WHEN item.kind='episode' THEN parent.parent_id ELSE item.parent_id END
 AND provider='tmdb' AND entity_kind='series' ORDER BY id LIMIT 1) root ON true
 LEFT JOIN LATERAL (SELECT external_id FROM metadata_identifiers WHERE metadata_id=item.parent_id
 AND provider='tmdb' AND entity_kind='season' ORDER BY id LIMIT 1) season ON true
 WHERE item.id IN ?`

func (r *MetadataRepository) tmdbLocalIdentity(ctx context.Context, metadataID string) (tmdbLocalIdentity, error) {
	var row tmdbLocalIdentity
	tx := r.db.WithContext(ctx)
	result := tx.Raw(tmdbLocalIdentitySQL, []string{metadataID}).Scan(&row)
	if result.Error != nil {
		return row, result.Error
	}
	if result.RowsAffected == 0 {
		return row, gorm.ErrRecordNotFound
	}
	return row, nil
}

func (r *MetadataRepository) saveTMDbSourceData(ctx context.Context, metadataID string, raw json.RawMessage, at time.Time, degraded bool) error {
	item, err := r.tmdbLocalIdentity(ctx, metadataID)
	if err != nil {
		return err
	}
	var obj tmdbObject
	if json.Unmarshal(raw, &obj) != nil || tmdbInt(obj, "id") <= 0 {
		return errors.New("invalid TMDb source identity")
	}
	if item.ExternalID == nil {
		return gorm.ErrRecordNotFound
	}
	tmdbID, err := strconv.Atoi(*item.ExternalID)
	if err != nil || tmdbID != tmdbInt(obj, "id") {
		return errors.New("TMDb source binding changed")
	}
	// 统一入口已保存合法父链和正文时，本地应用不要求父项也已绑定，也不刷新来源时间。
	var n int64
	if err = r.db.WithContext(ctx).Table("tmdb_payloads p").Joins("JOIN tmdb_subjects s ON s.id=p.subject_id").Where("s.kind=? AND s.tmdb_id=? AND p.origin IN ('api','season') AND p.body=?::jsonb", item.Kind, tmdbID, string(raw)).Count(&n).Error; err != nil {
		return err
	}
	req := TMDbSourceRequest{Kind: item.Kind, TMDbID: tmdbID, Component: "detail", Language: "und", Origin: "response", Season: -1, Episode: -1}
	if degraded {
		req.Component = "summary"
	}
	if item.Kind == "episode" && !degraded {
		if _, ok := obj["external_ids"]; !ok {
			if _, ok = obj["credits"]; !ok && len(obj["videos"]) == 0 {
				req.Component = "season_episode"
			}
		}
	}
	if item.Kind == "season" || item.Kind == "episode" {
		parentID := item.ParentID
		if parentID == nil {
			return errors.New("TMDb parent missing")
		}
		if item.ParentRecordID == nil {
			return gorm.ErrRecordNotFound
		}
		req.Season = item.SeasonNum
		if item.Kind == "episode" {
			req.Episode = item.EpisodeNum
			req.Season = item.ParentSeason
			if item.ParentRoot == nil {
				return errors.New("TMDb series parent missing")
			}
			if item.SeriesID == nil {
				return gorm.ErrRecordNotFound
			}
			req.SeriesID, _ = strconv.Atoi(*item.SeriesID)
			// 来源季已由接口确认时，不要求本地父季提前取得自己的 TMDb 标识。
			sourceParent, parentErr := tmdbSourceSeason(r.db.WithContext(ctx), req.SeriesID, req.Season)
			err = parentErr
			if err == nil {
				if n > 0 {
					// 中心已保存并确认的季内集正文用来源父链校验，保持原始正文和成功时间。
					var existing model.TMDbSubject
					return r.db.WithContext(ctx).Where("kind='episode' AND tmdb_id=? AND parent_id=? AND number=?", tmdbID, sourceParent.ID, req.Episode).Take(&existing).Error
				}
				if !tmdbCoordinateMatches(obj, "season_number", req.Season) || !tmdbCoordinateMatches(obj, "episode_number", req.Episode) {
					return errors.New("TMDb episode coordinate mismatch")
				}
				return (&TMDbCatalogRepository{db: r.db}).SaveResponse(ctx, req, raw, at, "")
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// 无来源父季时，只用本地已验证的 ID 建骨架，不猜测季 ID。
			if item.SeasonID == nil {
				return gorm.ErrRecordNotFound
			}
			seasonID, _ := strconv.Atoi(*item.SeasonID)
			sreq := TMDbSourceRequest{Kind: "season", TMDbID: seasonID, SeriesID: req.SeriesID, Season: req.Season, Component: "summary", Language: "und", Origin: "binding"}
			skeleton := json.RawMessage(fmt.Sprintf(`{"id":%d,"season_number":%d}`, seasonID, req.Season))
			if err = (&TMDbCatalogRepository{db: r.db}).SaveResponse(ctx, sreq, skeleton, at, ""); err != nil {
				return err
			}
		} else {
			if item.SeriesID == nil {
				return gorm.ErrRecordNotFound
			}
			req.SeriesID, _ = strconv.Atoi(*item.SeriesID)
		}
	}
	if n > 0 {
		if req.Kind == "season" {
			var existing model.TMDbSubject
			return r.db.WithContext(ctx).Table("tmdb_subjects AS season").Select("season.*").
				Joins("JOIN tmdb_subjects AS series ON series.id=season.parent_id").
				Where("season.kind='season' AND season.tmdb_id=? AND season.number=? AND series.tmdb_id=?", tmdbID, req.Season, req.SeriesID).Take(&existing).Error
		}
		return nil
	}
	return (&TMDbCatalogRepository{db: r.db}).SaveResponse(ctx, req, raw, at, "")
}

// TMDbDetailAppend 保持手动补全、定期刷新和传输缓存的组件身份一致。
func TMDbDetailAppend(person bool, path string) string {
	fields := []string{"credits", "external_ids", "translations", "videos"}
	if person {
		fields = []string{"external_ids"}
	} else if strings.Count(path, "/") == 2 {
		fields = append(fields, "alternative_titles", "keywords")
		if strings.HasPrefix(path, "/tv/") {
			fields = append(fields, "content_ratings")
		}
	}
	sort.Strings(fields)
	return strings.Join(fields, ",")
}
