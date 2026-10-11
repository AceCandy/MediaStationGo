package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 快照消费者的过渡投影：来源按 TMDb 身份关联，尚未导入时才读旧表。
const tmdbSnapshotSourceSQL = `(
 SELECT mid.id, mid.created_at, s.updated_at, mid.metadata_id, 'tmdb'::text AS provider,
 p.body || COALESCE(b.body,'{}'::jsonb) AS payload, false AS degraded,
 LEAST(p.fetched_at,COALESCE(b.fetched_at,p.fetched_at)) AS fetched_at
 FROM metadata_identifiers mid JOIN tmdb_subjects s ON s.kind=mid.entity_kind AND s.tmdb_id=CASE WHEN mid.external_id ~ '^[1-9][0-9]{0,9}$' THEN mid.external_id::bigint END
 JOIN LATERAL (SELECT body,fetched_at FROM tmdb_payloads WHERE subject_id=s.id AND component IN ('detail','base','season_episode') AND language IN ('zh-CN','und') ORDER BY quality DESC,fetched_at DESC LIMIT 1) p ON true
 LEFT JOIN LATERAL (SELECT body,fetched_at FROM tmdb_payloads WHERE subject_id=s.id AND component IN ('base','season_episode') AND language IN ('zh-CN','und') AND fetched_at>p.fetched_at ORDER BY fetched_at DESC LIMIT 1) b ON true
 WHERE mid.provider='tmdb'
 UNION ALL
 SELECT old.id,old.created_at,old.updated_at,old.metadata_id,old.provider,old.payload,old.degraded,old.fetched_at
 FROM metadata_provider_snapshots old
 WHERE old.provider<>'tmdb' OR NOT EXISTS (
 SELECT 1 FROM metadata_identifiers mid JOIN tmdb_subjects s ON s.kind=mid.entity_kind AND s.tmdb_id=CASE WHEN mid.external_id ~ '^[1-9][0-9]{0,9}$' THEN mid.external_id::bigint END
 JOIN tmdb_payloads p ON p.subject_id=s.id AND p.component IN ('detail','base','season_episode') AND p.language IN ('zh-CN','und')
 WHERE mid.metadata_id=old.metadata_id AND mid.provider='tmdb')
)`

func (r *MetadataRepository) saveTMDbSnapshot(ctx context.Context, metadataID string, raw json.RawMessage, at time.Time, degraded bool) error {
	return r.saveTMDbSnapshotOrigin(ctx, metadataID, raw, at, degraded, "legacy")
}

func (r *MetadataRepository) saveTMDbSnapshotOrigin(ctx context.Context, metadataID string, raw json.RawMessage, at time.Time, degraded bool, origin string) error {
	var item model.MetadataItem
	if err := r.db.WithContext(ctx).First(&item, "id = ?", metadataID).Error; err != nil {
		return err
	}
	var obj tmdbObject
	if json.Unmarshal(raw, &obj) != nil || tmdbInt(obj, "id") <= 0 {
		return errors.New("invalid TMDb snapshot identity")
	}
	var id model.MetadataIdentifier
	if err := r.db.WithContext(ctx).Where("metadata_id = ? AND provider = 'tmdb' AND entity_kind = ?", metadataID, item.Kind).First(&id).Error; err != nil {
		return err
	}
	tmdbID, err := strconv.Atoi(id.ExternalID)
	if err != nil || tmdbID != tmdbInt(obj, "id") {
		return errors.New("TMDb snapshot binding changed")
	}
	// 统一入口已保存合法父链和正文时，本地应用不要求父项也已绑定，也不刷新来源时间。
	var n int64
	if err = r.db.WithContext(ctx).Table("tmdb_payloads p").Joins("JOIN tmdb_subjects s ON s.id=p.subject_id").Where("s.kind=? AND s.tmdb_id=? AND p.origin IN ('api','season') AND p.body=?::jsonb", item.Kind, tmdbID, string(raw)).Count(&n).Error; err != nil {
		return err
	}
	req := TMDbSourceRequest{Kind: item.Kind, TMDbID: tmdbID, Component: "detail", Language: "und", Origin: origin, Season: -1, Episode: -1}
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
		var parent model.MetadataItem
		if err = r.db.WithContext(ctx).First(&parent, "id = ?", *parentID).Error; err != nil {
			return err
		}
		req.Season = item.SeasonNum
		if item.Kind == "episode" {
			req.Episode = item.EpisodeNum
			req.Season = parent.SeasonNum
			if parent.ParentID == nil {
				return errors.New("TMDb series parent missing")
			}
			var rootID model.MetadataIdentifier
			if err = r.db.WithContext(ctx).Where("metadata_id = ? AND provider = 'tmdb' AND entity_kind = 'series'", *parent.ParentID).First(&rootID).Error; err != nil {
				return err
			}
			req.SeriesID, _ = strconv.Atoi(rootID.ExternalID)
			// 来源季已由接口确认时，不要求本地父季提前取得自己的 TMDb 标识。
			var sourceParent model.TMDbSubject
			err = r.db.WithContext(ctx).Table("tmdb_subjects AS season").Select("season.*").
				Joins("JOIN tmdb_subjects AS series ON series.id=season.parent_id").
				Where("season.kind='season' AND season.number=? AND series.kind='series' AND series.tmdb_id=?", req.Season, req.SeriesID).Take(&sourceParent).Error
			if err == nil {
				if tmdbInt(obj, "season_number") != req.Season || tmdbInt(obj, "episode_number") != req.Episode {
					return errors.New("TMDb episode coordinate mismatch")
				}
				if n > 0 {
					var existing model.TMDbSubject
					return r.db.WithContext(ctx).Where("kind='episode' AND tmdb_id=? AND parent_id=? AND number=?", tmdbID, sourceParent.ID, req.Episode).Take(&existing).Error
				}
				return (&TMDbCatalogRepository{db: r.db}).SaveResponse(ctx, req, raw, at, "")
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// 无来源父季时，只用本地已验证的 ID 建骨架，不猜测季 ID。
			var sid model.MetadataIdentifier
			if err = r.db.WithContext(ctx).Where("metadata_id = ? AND provider = 'tmdb' AND entity_kind = 'season'", parent.ID).First(&sid).Error; err != nil {
				return err
			}
			seasonID, _ := strconv.Atoi(sid.ExternalID)
			sreq := TMDbSourceRequest{Kind: "season", TMDbID: seasonID, SeriesID: req.SeriesID, Season: req.Season, Component: "summary", Language: "und", Origin: "legacy"}
			skeleton := json.RawMessage(fmt.Sprintf(`{"id":%d,"season_number":%d}`, seasonID, req.Season))
			if err = (&TMDbCatalogRepository{db: r.db}).SaveResponse(ctx, sreq, skeleton, at, ""); err != nil {
				return err
			}
		} else {
			var root model.MetadataIdentifier
			if err = r.db.WithContext(ctx).Where("metadata_id = ? AND provider = 'tmdb' AND entity_kind = 'series'", parent.ID).First(&root).Error; err != nil {
				return err
			}
			req.SeriesID, _ = strconv.Atoi(root.ExternalID)
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

// MigrateLegacyBatch 无网络导入，失败条目返回 ID，旧快照始终保留。
func (r *TMDbCatalogRepository) MigrateLegacyBatch(ctx context.Context, after string, limit int) (string, int, []string, error) {
	var rows []model.MetadataProviderSnapshot
	if err := r.db.WithContext(ctx).Where("provider='tmdb' AND id > ?", after).Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return after, 0, nil, err
	}
	done := 0
	failed := []string{}
	for _, row := range rows {
		if ctx.Err() != nil {
			return after, done, failed, ctx.Err()
		}
		after = row.ID
		err := (&MetadataRepository{db: r.db}).saveTMDbSnapshot(ctx, row.MetadataID, json.RawMessage(row.Payload), row.FetchedAt, row.Degraded)
		if err != nil {
			failed = append(failed, row.MetadataID)
		} else {
			done++
		}
	}
	if len(rows) < limit {
		after = ""
	}
	return after, done, failed, nil
}

func (r *TMDbCatalogRepository) SeedLegacyPeople(ctx context.Context, after string, limit int) (string, int, error) {
	var rows []struct{ ID, PersonID, ExternalID, Name, OriginalName, ProfileImageKey, ProfileImageSourceURL string }
	err := r.db.WithContext(ctx).Raw(`SELECT i.id,i.person_id,i.external_id,p.name,p.original_name,p.profile_image_key,p.profile_image_source_url FROM person_identifiers i JOIN people p ON p.id=i.person_id WHERE i.provider='tmdb' AND i.external_id ~ '^[1-9][0-9]{0,9}$' AND i.id>? ORDER BY i.id LIMIT ?`, after, limit).Scan(&rows).Error
	if err != nil {
		return after, 0, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('tmdb_catalog_write'))").Error; err != nil {
			return err
		}
		for _, v := range rows {
			var count int64
			id, _ := strconv.Atoi(v.ExternalID)
			if err := tx.Model(&model.TMDbPersonProfile{}).Where("tmdb_id=?", id).Count(&count).Error; err != nil {
				return err
			}
			imagePath := ""
			if u, e := url.Parse(v.ProfileImageSourceURL); e == nil && strings.EqualFold(u.Hostname(), "image.tmdb.org") && strings.HasPrefix(u.Path, "/t/p/") {
				imagePath = "/" + path.Base(u.Path)
			}
			if count > 0 {
				if imagePath != "" && v.ProfileImageKey != "" {
					if e := tx.Model(&model.TMDbPersonProfile{}).Where("tmdb_id=? AND person_id=? AND image_key='' AND profile_path=?", id, v.PersonID, imagePath).Updates(map[string]any{"image_key": v.ProfileImageKey, "image_source_path": imagePath, "image_status": "ready"}).Error; e != nil {
						return e
					}
				}
				continue
			}
			p := model.TMDbPersonProfile{TMDbID: id, PersonID: v.PersonID, Name: v.OriginalName, OriginalName: v.OriginalName, Quality: 1, Fields: "{}", ProfilePath: imagePath}
			if imagePath != "" && v.ProfileImageKey != "" {
				p.ImageKey, p.ImageSourcePath, p.ImageStatus = v.ProfileImageKey, imagePath, "ready"
			}
			if p.Name == "" {
				p.Name = v.Name
			}
			if err := tx.Omit("Person").Create(&p).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if len(rows) == limit {
		after = rows[len(rows)-1].ID
	} else {
		after = ""
	}
	return after, len(rows), err
}

// ReuseLegacyArtwork 只复用来源路径一致的现有资产，不下载或复制文件。
func (r *TMDbCatalogRepository) ReuseLegacyArtwork(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec(`UPDATE tmdb_artworks a SET asset_id=old.asset_id, downloaded_path=a.source_path,status='ready'
 FROM tmdb_subjects s JOIN metadata_identifiers mid ON mid.provider='tmdb' AND mid.entity_kind=s.kind AND s.tmdb_id=CASE WHEN mid.external_id ~ '^[1-9][0-9]{0,9}$' THEN mid.external_id::bigint END
 JOIN metadata_artworks old ON old.metadata_id=mid.metadata_id AND old.source_provider='tmdb'
 JOIN artwork_assets asset ON asset.id=old.asset_id
 WHERE a.subject_id=s.id AND a.type=old.artwork_type AND a.asset_id IS NULL AND a.source_path<>''
 AND right(old.source_url,length(a.source_path))=a.source_path`).Error
}

// SeedLegacySubjects 保留仅有绑定、没有旧快照的来源身份；本地文字明确标记为历史投影。
func (r *TMDbCatalogRepository) SeedLegacySubjects(ctx context.Context, after string, limit int) (string, int, []string, error) {
	var rows []struct {
		ID, MetadataID, ExternalID, Kind, Title string
		SeasonNum, EpisodeNum                   int
	}
	err := r.db.WithContext(ctx).Raw(`SELECT i.id,i.metadata_id,i.external_id,m.kind,m.title,m.season_num,m.episode_num FROM metadata_identifiers i JOIN metadata_items m ON m.id=i.metadata_id WHERE i.provider='tmdb' AND i.entity_kind=m.kind AND i.id>? AND NOT EXISTS(SELECT 1 FROM tmdb_subjects s WHERE s.kind=i.entity_kind AND s.tmdb_id=CASE WHEN i.external_id ~ '^[1-9][0-9]{0,9}$' THEN i.external_id::bigint END) ORDER BY i.id LIMIT ?`, after, limit).Scan(&rows).Error
	if err != nil {
		return after, 0, nil, err
	}
	bad := []string{}
	done := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return after, done, bad, ctx.Err()
		}
		after = row.ID
		id, e := strconv.Atoi(row.ExternalID)
		if e != nil || id <= 0 {
			bad = append(bad, row.MetadataID)
			continue
		}
		o := map[string]any{"id": id, "name": row.Title, "season_number": row.SeasonNum, "episode_number": row.EpisodeNum}
		raw, _ := json.Marshal(o)
		if e = (&MetadataRepository{db: r.db}).saveTMDbSnapshotOrigin(ctx, row.MetadataID, raw, time.Time{}, true, "legacy_projection"); e != nil {
			bad = append(bad, row.MetadataID)
		} else {
			done++
		}
	}
	if len(rows) < limit {
		after = ""
	}
	return after, done, bad, nil
}

// SeedMaintenanceJobs 只为已经成功取得的组件建维护槽，不排远程任务，不延长原成功时间。
func (r *TMDbCatalogRepository) SeedMaintenanceJobs(ctx context.Context, configKey, after string, limit int) (string, error) {
	var rows []model.TMDbPayload
	if err := r.db.WithContext(ctx).Where("id>? AND component IN ('base','detail','filmography') AND language IN ('zh-CN','und')", after).Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return after, err
	}
	for _, p := range rows {
		var sub *model.TMDbSubject
		pathValue := ""
		if p.SubjectID != nil {
			v, e := r.Subject(ctx, *p.SubjectID)
			if e != nil {
				return after, e
			}
			sub = v
			switch v.Kind {
			case "movie":
				pathValue = fmt.Sprintf("/movie/%d", v.TMDbID)
			case "series":
				pathValue = fmt.Sprintf("/tv/%d", v.TMDbID)
			case "season":
				parent, e := r.Subject(ctx, *v.ParentID)
				if e != nil {
					return after, e
				}
				pathValue = fmt.Sprintf("/tv/%d/season/%d", parent.TMDbID, *v.Number)
			default:
				continue
			}
		} else if p.PersonProfileID != nil {
			var v model.TMDbPersonProfile
			if e := r.db.WithContext(ctx).First(&v, "id=?", *p.PersonProfileID).Error; e != nil {
				return after, e
			}
			pathValue = fmt.Sprintf("/person/%d", v.TMDbID)
			if p.Component == "filmography" {
				pathValue += "/combined_credits"
			}
		} else {
			continue
		}
		scopes := []string{p.Component}
		if p.Component == "detail" && sub != nil {
			scopes = append(scopes, "base")
		}
		for _, scope := range scopes {
			q := url.Values{"language": {"zh-CN"}}
			if scope == "detail" {
				q.Set("append_to_response", TMDbDetailAppend(sub == nil, pathValue))
			}
			query := q.Encode()
			next := p.FetchedAt.Add(tmdbTTL(sub, scope, p.FetchedAt))
			j := model.TMDbRefreshJob{Key: tmdbHash(configKey + pathValue + "?" + query), ConfigKey: configKey, Path: pathValue, Query: query, Scope: scope, Language: "zh-CN", SubjectID: p.SubjectID, PersonProfileID: p.PersonProfileID, PayloadID: &p.ID, Status: "ready", LastSuccessAt: &p.FetchedAt, NextRefreshAt: &next}
			if e := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).Create(&j).Error; e != nil {
				return after, e
			}
		}
	}
	if len(rows) == limit {
		after = rows[len(rows)-1].ID
	} else {
		after = ""
	}
	return after, nil
}

// TMDbDetailAppend 保持手动补全、历史维护和传输缓存的组件身份一致。
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
