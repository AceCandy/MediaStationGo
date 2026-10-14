package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TMDbCatalogRepository struct{ db *gorm.DB }

// TMDbSourceRequest 只接受上层解析过的身份与无凭据请求路径。
type TMDbSourceRequest struct {
	Key, ConfigKey, Path, Query, Kind, Component, Language, Origin string
	TMDbID, SeriesID, Season, Episode                              int
	QueryResult                                                    bool
	Automatic                                                      bool
}

type tmdbObject map[string]json.RawMessage

func tmdbString(o tmdbObject, k string) string {
	var s string
	_ = json.Unmarshal(o[k], &s)
	return strings.TrimSpace(s)
}
func tmdbInt(o tmdbObject, k string) int { var n int; _ = json.Unmarshal(o[k], &n); return n }

// 坐标必须为接口明确返回的整数，不能把空值当作0。
func tmdbCoordinateMatches(o tmdbObject, key string, expected int) bool {
	raw, ok := o[key]
	if !ok {
		return false
	}
	var n *int
	return json.Unmarshal(raw, &n) == nil && n != nil && *n == expected
}

func tmdbJSON(v any) string    { b, _ := json.Marshal(v); return string(b) }
func tmdbHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func tmdbArray(o tmdbObject, k string) ([]tmdbObject, bool) {
	var a []tmdbObject
	b, ok := o[k]
	if !ok || len(b) == 0 || b[0] != '[' {
		return nil, false
	}
	err := json.Unmarshal(b, &a)
	return a, err == nil
}
func tmdbFields(o tmdbObject) string {
	out := tmdbObject{}
	for _, k := range []string{"poster_path", "backdrop_path", "still_path", "runtime", "episode_run_time", "original_language", "genres", "origin_country", "production_countries", "spoken_languages", "external_ids", "imdb_id", "homepage", "number_of_seasons", "number_of_episodes", "last_air_date", "birthday", "deathday", "place_of_birth", "also_known_as", "gender"} {
		if v, ok := o[k]; ok {
			out[k] = v
		}
	}
	return tmdbJSON(out)
}
func mergeTMDbFields(old, new string, fillOnly bool) string {
	var a, b tmdbObject
	_ = json.Unmarshal([]byte(old), &a)
	_ = json.Unmarshal([]byte(new), &b)
	if a == nil {
		a = tmdbObject{}
	}
	for k, v := range b {
		if (k == "poster_path" || k == "backdrop_path" || k == "still_path") && (string(v) == "null" || string(v) == `""`) && len(a[k]) > 0 {
			continue
		}
		if !fillOnly || len(a[k]) == 0 || string(a[k]) == "null" || string(a[k]) == `""` {
			a[k] = v
		}
	}
	return tmdbJSON(a)
}

// SaveResponse 原始组件、来源投影和关系在一个事务提交；不会写本地 metadata。
func (r *TMDbCatalogRepository) SaveResponse(ctx context.Context, req TMDbSourceRequest, raw []byte, at time.Time, token string) error {
	var o tmdbObject
	if err := json.Unmarshal(raw, &o); err != nil || o == nil {
		return errors.New("invalid TMDb response")
	}
	return withTMDbWriteTransaction(ctx, r.db, func(tx *gorm.DB) error {
		saveSubject := saveTMDbSubject
		if req.Component == "inventory" {
			if req.Kind != "series" && req.Kind != "season" {
				return errors.New("invalid TMDb inventory kind")
			}
			field := "seasons"
			if req.Kind == "season" {
				field = "episodes"
			}
			if rows, ok := tmdbArray(o, field); !ok || rows == nil {
				return errors.New("TMDb inventory missing")
			}
			saveSubject = saveTMDbInventorySubject
		}

		if token != "" {
			var j model.TMDbRefreshJob
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ? AND token = ? AND lease_until > ?", req.Key, token, time.Now()).First(&j).Error; err != nil {
				return err
			}
		}
		if req.QueryResult {
			rows, ok := tmdbArray(o, "results")
			if !ok {
				return errors.New("TMDb results missing")
			}
			for _, v := range rows {
				kind := req.Kind
				if m := tmdbString(v, "media_type"); m != "" {
					kind = m
				}
				if kind == "tv" {
					kind = "series"
				}
				if tmdbInt(v, "id") <= 0 {
					continue
				}
				if kind == "person" {
					if _, err := saveTMDbPerson(tx, v, 1, at); err != nil {
						return err
					}
				} else if kind == "movie" || kind == "series" {
					if _, err := saveSubject(tx, kind, nil, nil, v, 1, at); err != nil {
						return err
					}
				}
			}
			ttl := 6 * time.Hour
			if strings.HasPrefix(req.Path, "/search/") {
				ttl = time.Hour
			}
			cache := model.TMDbQueryCache{Key: req.Key, Body: string(raw), FetchedAt: at, ExpiresAt: at.Add(ttl)}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"body", "fetched_at", "expires_at", "updated_at"}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "tmdb_query_cache.fetched_at <= excluded.fetched_at"}}}}).Create(&cache).Error; err != nil {
				return err
			}
			if token != "" {
				return tx.Model(&model.TMDbRefreshJob{}).Where("key=? AND token=?", req.Key, token).Updates(map[string]any{"status": "ready", "explicit_pending": false, "requested_by": "", "token": "", "lease_until": nil, "next_attempt_at": nil, "last_error": "", "attempts": 0, "last_success_at": at}).Error
			}
			return nil
		}
		query, _ := url.ParseQuery(req.Query)
		for _, field := range strings.Split(query.Get("append_to_response"), ",") {
			if field != "credits" && field != "external_ids" {
				continue
			}
			var component tmdbObject
			if json.Unmarshal(o[field], &component) != nil || component == nil {
				return errors.New("TMDb requested component missing")
			}
			if field == "credits" {
				_, castOK := tmdbArray(component, "cast")
				_, crewOK := tmdbArray(component, "crew")
				if !castOK || !crewOK {
					return errors.New("TMDb credits incomplete")
				}
			}
		}
		// 季活跃度取已返回的最近播出集，不以首播日推断完结。
		if req.Kind == "season" {
			if episodes, ok := tmdbArray(o, "episodes"); ok {
				latest := ""
				for _, v := range episodes {
					d := tmdbString(v, "air_date")
					if _, e := time.Parse("2006-01-02", d); e == nil && d > latest {
						latest = d
					}
				}
				o["last_air_date"], _ = json.Marshal(latest)
			}
		}
		if req.Component == "credits" {
			_, castOK := tmdbArray(o, "cast")
			_, crewOK := tmdbArray(o, "crew")
			if tmdbInt(o, "id") != req.TMDbID || !castOK || !crewOK {
				return errors.New("TMDb credits identity or scope incomplete")
			}
		}
		quality := 2
		if req.Component == "detail" {
			quality = 3
		}
		if req.Component == "summary" {
			quality = 1
		}
		projectionQuality := quality
		if req.Language != "zh-CN" && req.Language != "und" {
			projectionQuality = 0
		}
		var subject *model.TMDbSubject
		var person *model.TMDbPersonProfile
		var err error
		if req.Kind == "person" {
			if req.Component == "filmography" {
				if tmdbInt(o, "id") != req.TMDbID {
					return errors.New("TMDb filmography identity mismatch")
				}
				var p model.TMDbPersonProfile
				err = tx.Where("tmdb_id = ?", req.TMDbID).First(&p).Error
				person = &p
			} else {
				if tmdbInt(o, "id") != req.TMDbID {
					return errors.New("TMDb person identity mismatch")
				}
				person, err = saveTMDbPerson(tx, o, projectionQuality, at)
			}
		} else {
			var parent *string
			var number *int
			if req.Kind == "season" || req.Kind == "episode" {
				seriesObj := tmdbObject{"id": json.RawMessage(strconv.Itoa(req.SeriesID))}
				root, e := saveSubject(tx, "series", nil, nil, seriesObj, 0, at)
				if e != nil {
					return e
				}
				parent = &root.ID
				number = &req.Season
				if req.Kind == "episode" {
					season, e := tmdbSourceSeason(tx, req.SeriesID, req.Season)
					if e != nil {
						return fmt.Errorf("TMDb episode parent: %w", e)
					}
					parent = &season.ID
					number = &req.Episode
				}
				if req.Kind == "season" && !tmdbCoordinateMatches(o, "season_number", req.Season) {
					return errors.New("TMDb season coordinate mismatch")
				}
				if req.Kind == "episode" && (!tmdbCoordinateMatches(o, "season_number", req.Season) || !tmdbCoordinateMatches(o, "episode_number", req.Episode)) {
					return errors.New("TMDb episode coordinate mismatch")
				}
			}
			if req.Component == "credits" {
				var s model.TMDbSubject
				err = tx.Where("kind = ? AND tmdb_id = ?", req.Kind, req.TMDbID).First(&s).Error
				subject = &s
			} else {
				if req.TMDbID > 0 && tmdbInt(o, "id") != req.TMDbID {
					return errors.New("TMDb subject identity mismatch")
				}
				subject, err = saveSubject(tx, req.Kind, parent, number, o, projectionQuality, at)
			}
		}
		if err != nil {
			return err
		}
		payload := model.TMDbPayload{Component: req.Component, Language: req.Language, Body: string(raw), Origin: req.Origin, Quality: quality, FetchedAt: at}
		if payload.Origin == "" {
			payload.Origin = "api"
		}
		if payload.Language == "" {
			payload.Language = "und"
		}
		if subject != nil {
			payload.SubjectID = &subject.ID
		}
		if person != nil {
			payload.PersonProfileID = &person.ID
		}
		saved, err := saveTMDbPayload(tx, &payload)
		if err != nil {
			return err
		}
		if !saved {
			if token != "" {
				next := payload.FetchedAt.Add(tmdbTTL(subject, req.Component, payload.FetchedAt))
				updates := map[string]any{"subject_id": payload.SubjectID, "person_profile_id": payload.PersonProfileID, "payload_id": payload.ID, "status": "ready", "explicit_pending": false, "requested_by": "", "token": "", "lease_until": nil, "last_success_at": payload.FetchedAt, "next_refresh_at": next, "next_attempt_at": nil, "last_error": ""}
				if payload.FetchedAt.Equal(at) {
					updates["attempts"] = 0
				}
				return tx.Model(&model.TMDbRefreshJob{}).Where("key=? AND token=?", req.Key, token).Updates(updates).Error
			}
			return nil
		}
		primary := req.Language == "zh-CN" || req.Language == "und"
		if primary && req.Component != "inventory" {
			if req.Component == "filmography" {
				if err = saveTMDbCredits(tx, nil, person, o, "person", at, true); err != nil {
					return err
				}
			} else if req.Component == "credits" {
				if err = saveTMDbCredits(tx, subject, nil, o, "subject", at, false); err != nil {
					return err
				}
			} else if subject != nil {
				var credits tmdbObject
				if json.Unmarshal(o["credits"], &credits) == nil && credits != nil {
					if err = saveTMDbCredits(tx, subject, nil, credits, "subject", at, false); err != nil {
						return err
					}
				}
			}
		}
		if subject != nil && (req.Kind == "series" || req.Kind == "season") {
			field, kind := "seasons", "season"
			if req.Kind == "season" {
				field, kind = "episodes", "episode"
			}
			if rows, ok := tmdbArray(o, field); ok {
				for _, v := range rows {
					n := tmdbInt(v, kind+"_number")
					if tmdbInt(v, "id") <= 0 || (kind == "episode" && n <= 0) {
						continue
					}
					childQuality := 1
					if kind == "episode" {
						childQuality = 2
					}
					if !primary {
						childQuality = 0
					}
					child, e := saveSubject(tx, kind, &subject.ID, &n, v, childQuality, at)
					if e != nil {
						return e
					}
					if kind == "episode" && req.Component != "inventory" {
						p := model.TMDbPayload{SubjectID: &child.ID, Component: "season_episode", Language: payload.Language, Body: tmdbJSON(v), Origin: "season", Quality: 1, FetchedAt: at}
						ok, e := saveTMDbPayload(tx, &p)
						if e != nil {
							return e
						}
						if ok && primary {
							c := tmdbObject{}
							if b, present := v["guest_stars"]; present {
								c["cast"] = b
							}
							if b, present := v["crew"]; present {
								c["crew"] = b
							}
							if e = saveTMDbCredits(tx, child, nil, c, "subject", at, false); e != nil {
								return e
							}
						}
					}
				}
			}
		}
		if token != "" {
			ttl := tmdbTTL(subject, req.Component, at)
			updates := map[string]any{"subject_id": payload.SubjectID, "person_profile_id": payload.PersonProfileID, "payload_id": payload.ID, "status": "ready", "explicit_pending": false, "requested_by": "", "token": "", "lease_until": nil, "last_success_at": at, "next_refresh_at": at.Add(ttl), "next_attempt_at": nil, "attempts": 0, "last_error": ""}
			if err = tx.Model(&model.TMDbRefreshJob{}).Where("key = ? AND token = ?", req.Key, token).Updates(updates).Error; err != nil {
				return err
			}

			if req.Component == "detail" && subject != nil && req.Kind != "episode" {
				q, _ := url.ParseQuery(req.Query)
				q.Del("append_to_response")
				query := q.Encode()
				base := model.TMDbRefreshJob{Key: tmdbHash(req.ConfigKey + req.Path + "?" + query), ConfigKey: req.ConfigKey, Path: req.Path, Query: query, SubjectID: &subject.ID, PayloadID: &payload.ID, Scope: "base", Language: req.Language, Status: "ready", LastSuccessAt: &at}
				next := at.Add(tmdbTTL(subject, "base", at))
				base.NextRefreshAt = &next
				if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"payload_id", "last_success_at", "next_refresh_at"}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "tmdb_refresh_jobs.last_success_at IS NULL OR tmdb_refresh_jobs.last_success_at <= excluded.last_success_at"}}}}).Create(&base).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func saveTMDbSubject(tx *gorm.DB, kind string, parent *string, number *int, o tmdbObject, quality int, at time.Time) (*model.TMDbSubject, error) {
	id := tmdbInt(o, "id")
	if id <= 0 {
		return nil, errors.New("invalid TMDb ID")
	}
	if kind != "movie" && kind != "series" && kind != "season" && kind != "episode" {
		return nil, errors.New("invalid TMDb kind")
	}
	if kind == "season" || kind == "episode" {
		if parent == nil || number == nil || *number < 0 || (kind == "episode" && *number == 0) {
			return nil, errors.New("invalid TMDb parent coordinate")
		}
	}
	var row model.TMDbSubject
	cache := tmdbTransactionCache(tx)
	key := kind + "/" + strconv.Itoa(id)
	loaded := false
	if cache != nil {
		row, loaded = cache.subjects[key]
	}
	var err error
	if !loaded {
		err = tx.Where("kind = ? AND tmdb_id = ?", kind, id).First(&row).Error
	}
	exists := err == nil && row.ID != ""
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if exists && parent != nil && (row.ParentID == nil || *row.ParentID != *parent || row.Number == nil || *row.Number != *number) {
		return nil, errors.New("TMDb parent conflict")
	}
	if exists && row.FetchedAt != nil && row.FetchedAt.After(at) {
		if cache != nil {
			cache.subjects[key] = row
		}
		return &row, nil
	}
	old := row
	old.Fields = mergeTMDbFields(old.Fields, "{}", false)
	if old.Rating != nil {
		rating := *old.Rating
		old.Rating = &rating
	}
	fill := exists && quality < 2 && quality < row.Quality
	row.Kind, row.TMDbID = kind, id
	if parent != nil {
		row.ParentID, row.Number = parent, number
	}
	set := func(dst *string, key string) {
		if value := tmdbString(o, key); value != "" && (!fill || *dst == "") {
			*dst = value
		}
	}
	set(&row.Title, "title")
	set(&row.Title, "name")
	if model.HasChineseText(old.Title) && !model.HasChineseText(row.Title) {
		row.Title = old.Title
	}
	set(&row.OriginalName, "original_title")
	set(&row.OriginalName, "original_name")
	set(&row.Overview, "overview")
	if !fill && (kind == model.MetadataKindSeason || kind == model.MetadataKindEpisode) {
		var translated struct {
			Translations []model.TMDbTranslation `json:"translations"`
		}
		_ = json.Unmarshal(o["translations"], &translated)
		n := 0
		if number != nil {
			n = *number
		}
		row.Title = model.TMDbEntityTitle(tmdbString(o, "name"), translated.Translations, kind, n)
		row.Overview = model.TMDbEntityOverview(tmdbString(o, "overview"), translated.Translations)
	}
	set(&row.Status, "status")
	for _, k := range []string{"release_date", "first_air_date", "air_date"} {
		set(&row.ReleaseDate, k)
	}
	if !fill {
		if v, ok := o["vote_average"]; ok {
			_ = json.Unmarshal(v, &row.Rating)
		}
		if _, ok := o["vote_count"]; ok {
			row.VoteCount = tmdbInt(o, "vote_count")
		}
		row.Quality = max(row.Quality, quality)
		if quality > 0 && !at.IsZero() {
			row.FetchedAt = &at
		}
	}
	row.Fields = mergeTMDbFields(row.Fields, tmdbFields(o), fill)
	if old.FetchedAt != nil && row.FetchedAt != nil && old.FetchedAt.Equal(*row.FetchedAt) {
		row.FetchedAt = old.FetchedAt
	}
	if !exists || !reflect.DeepEqual(old, row) {
		if err = tx.Omit("Parent", "NextPeopleAt").Save(&row).Error; err != nil {
			return nil, err
		}
	}
	if cache != nil {
		cache.subjects[key] = row

	}
	for field, typ := range map[string]string{"poster_path": "poster", "backdrop_path": "backdrop", "still_path": "still"} {
		if _, ok := o[field]; !ok {
			continue
		}
		var art model.TMDbArtwork
		e := tx.Where("subject_id = ? AND type = ?", row.ID, typ).First(&art).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, e
		}
		path := tmdbString(o, field)
		if e == nil && (fill || path == "" && art.SourcePath != "") {
			continue
		}
		if art.ID != "" && art.SourcePath == path {
			continue
		}
		art.SubjectID, art.Type, art.SourcePath = row.ID, typ, path
		art.Status = "pending"
		art.RetryAt = nil
		if path == "" {
			art.Status = "no_image"
		}
		if e = tx.Omit("Subject", "Asset").Save(&art).Error; e != nil {
			return nil, e
		}
	}
	return &row, nil
}

func saveTMDbPerson(tx *gorm.DB, o tmdbObject, quality int, at time.Time) (*model.TMDbPersonProfile, error) {
	id := tmdbInt(o, "id")
	if id <= 0 {
		return nil, errors.New("invalid TMDb person ID")
	}
	var row model.TMDbPersonProfile
	cache := tmdbTransactionCache(tx)
	loaded := false
	if cache != nil {
		row, loaded = cache.people[id]
	}
	var err error
	if !loaded {
		err = tx.Where("tmdb_id = ?", id).First(&row).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if row.ID == "" {
		var ident model.PersonIdentifier
		err = tx.Unscoped().Where("provider = 'tmdb' AND external_id = ?", strconv.Itoa(id)).First(&ident).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			p := model.Person{Name: tmdbString(o, "name"), OriginalName: tmdbString(o, "original_name"), Source: "tmdb"}
			if p.Name == "" {
				p.Name = fmt.Sprintf("TMDb %d", id)
			}
			if p.OriginalName == "" {
				p.OriginalName = p.Name
			}
			p.NormalizedName = strings.ToLower(p.Name)
			if err = tx.Create(&p).Error; err != nil {
				return nil, err
			}
			ident = model.PersonIdentifier{PersonID: p.ID, Provider: "tmdb", ExternalID: strconv.Itoa(id)}
			res := tx.Omit("Person").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "provider"}, {Name: "external_id"}}, DoNothing: true}).Create(&ident)
			if res.Error != nil {
				return nil, res.Error
			}
			if res.RowsAffected == 0 {
				if err = tx.Unscoped().Delete(&p).Error; err != nil {
					return nil, err
				}
				ident = model.PersonIdentifier{}
				if err = tx.Unscoped().Where("provider = 'tmdb' AND external_id = ?", strconv.Itoa(id)).First(&ident).Error; err != nil {
					return nil, err
				}
			}
		} else if err != nil {
			return nil, err
		}
		if err = tx.Unscoped().Model(&model.Person{}).Where("id = ? AND deleted_at IS NOT NULL", ident.PersonID).Update("deleted_at", nil).Error; err != nil {
			return nil, err
		}
		if err = tx.Unscoped().Model(&ident).Where("deleted_at IS NOT NULL").Update("deleted_at", nil).Error; err != nil {
			return nil, err
		}
		row.TMDbID, row.PersonID = id, ident.PersonID
	}
	if row.FetchedAt != nil && row.FetchedAt.After(at) {
		if cache != nil {
			cache.people[id] = row
		}
		return &row, nil
	}
	old := row
	old.Fields = mergeTMDbFields(old.Fields, "{}", false)
	fill := quality < row.Quality
	for key, dst := range map[string]*string{"name": &row.Name, "original_name": &row.OriginalName, "biography": &row.Biography, "known_for_department": &row.Department} {
		if _, ok := o[key]; ok && (!fill || *dst == "") {
			*dst = tmdbString(o, key)
		}
	}
	if _, ok := o["profile_path"]; ok && (!fill || row.ProfilePath == "") {
		path := tmdbString(o, "profile_path")
		if path != row.ProfilePath {
			row.ProfilePath = path
			row.ImageRetryAt = nil
			row.ImageStatus = "pending"
			if path == "" {
				row.ImageStatus = "no_image"
			}
		}
	}
	row.Fields = mergeTMDbFields(row.Fields, tmdbFields(o), fill)
	if !fill {
		row.Quality = quality
		row.FetchedAt = &at
	}
	if old.FetchedAt != nil && row.FetchedAt != nil && old.FetchedAt.Equal(*row.FetchedAt) {
		row.FetchedAt = old.FetchedAt
	}
	if old.ID == "" || !reflect.DeepEqual(old, row) {
		omit := []string{"Person", "NextPeopleAt"}
		if old.ID != "" {
			// 下载结果独立更新，来源缓存不能把预读的旧图片状态写回。
			omit = append(omit, "ImageKey", "ImageSourcePath")
			if old.ProfilePath == row.ProfilePath {
				omit = append(omit, "ImageStatus", "ImageRetryAt")
			}
		}
		if err = tx.Omit(omit...).Save(&row).Error; err != nil {
			return nil, err
		}
	}
	if cache != nil {
		cache.people[id] = row
	}
	return &row, nil
}

func saveTMDbPayload(tx *gorm.DB, p *model.TMDbPayload) (bool, error) {
	q := tx.Where("component = ? AND language = ?", p.Component, p.Language)
	if p.SubjectID != nil {
		q = q.Where("subject_id = ?", *p.SubjectID)
	} else {
		q = q.Where("person_profile_id = ?", *p.PersonProfileID)
	}
	var stored struct {
		model.TMDbPayload
		SameBody bool
	}
	err := q.Model(&model.TMDbPayload{}).Select("tmdb_payloads.*, body = ?::jsonb AS same_body", p.Body).First(&stored).Error
	old := stored.TMDbPayload
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if err == nil {
		if old.FetchedAt.After(p.FetchedAt) || old.Quality > p.Quality {
			*p = old
			return false, nil
		}
		if stored.SameBody && old.FetchedAt.Equal(p.FetchedAt) && old.Quality == p.Quality && old.Origin == p.Origin {
			*p = old
			return false, nil
		}
		p.PermanentBase = old.PermanentBase
	}
	return true, tx.Omit("Subject", "PersonProfile").Save(p).Error
}

func saveTMDbCredits(tx *gorm.DB, s *model.TMDbSubject, p *model.TMDbPersonProfile, o tmdbObject, origin string, at time.Time, complete bool) error {
	cast, castOK := tmdbArray(o, "cast")
	crew, crewOK := tmdbArray(o, "crew")
	if complete && (!castOK || !crewOK) {
		return errors.New("TMDb filmography is incomplete")
	}
	for _, group := range []struct {
		category string
		rows     []tmdbObject
		loaded   bool
	}{{"cast", cast, castOK}, {"crew", crew, crewOK}} {
		if !group.loaded {
			continue
		}
		// 关系不按语言分表；旧正文仍可入库，但不能覆盖较新的有效列表（含空列表）。
		newer := tx.Model(&model.TMDbPayload{}).Where("language IN ('zh-CN','und') AND fetched_at > ?", at)
		if s != nil {
			newer = newer.Where("subject_id = ?", s.ID)
		} else {
			newer = newer.Where("person_profile_id = ?", p.ID)
		}
		seasonField := group.category
		if group.category == "cast" {
			seasonField = "guest_stars"
		}
		var n int64
		if err := newer.Where(`((component IN ('credits','filmography') AND jsonb_typeof(body -> ?) = 'array')
			OR jsonb_typeof(body -> 'credits' -> ?) = 'array'
			OR (component = 'season_episode' AND jsonb_typeof(body -> ?) = 'array'))`, group.category, group.category, seasonField).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if origin == "subject" {
			if err := preloadTMDbCreditPeople(tx, group.rows); err != nil {
				return err
			}
		}
		q := tx.Where("origin = ? AND category = ?", origin, group.category)
		if s != nil {
			q = q.Where("subject_id = ?", s.ID)
		} else {
			q = q.Where("person_profile_id = ?", p.ID)
		}
		if err := q.Delete(&model.TMDbCredit{}).Error; err != nil {
			return err
		}
		credits := make([]model.TMDbCredit, 0, len(group.rows))
		for _, v := range group.rows {
			subject, person := s, p
			var err error
			if origin == "subject" {
				person, err = saveTMDbPerson(tx, v, 1, at)
			} else {
				kind := tmdbString(v, "media_type")
				if kind == "tv" {
					kind = "series"
				}
				if kind != "movie" && kind != "series" {
					continue
				}
				subject, err = saveTMDbSubject(tx, kind, nil, nil, v, 1, at)
			}
			if err != nil {
				return err
			}
			c := model.TMDbCredit{SubjectID: subject.ID, PersonProfileID: person.ID, Origin: origin, Category: group.category, CreditID: tmdbString(v, "credit_id"), Character: tmdbString(v, "character"), Department: tmdbString(v, "department"), Job: tmdbString(v, "job"), SortOrder: tmdbInt(v, "order")}
			c.CreditKey = tmdbHash(tmdbJSON([]any{c.Category, c.CreditID, c.Character, c.Department, c.Job, tmdbInt(v, "episode_id"), tmdbString(v, "role_id")}))
			credits = append(credits, c)
		}
		if len(credits) > 0 {
			if err := tx.Omit("Subject", "PersonProfile").Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&credits, 500).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func tmdbTTL(s *model.TMDbSubject, scope string, now time.Time) time.Duration {
	if scope == "filmography" || s == nil {
		return 30 * 24 * time.Hour
	}

	active := false
	unknown := false
	switch s.Kind {
	case "series":
		active = s.Status == "Returning Series" || s.Status == "In Production" || s.Status == "Planned"
		unknown = !active && s.Status != "Ended" && s.Status != "Canceled"
	case "season":
		var f tmdbObject
		_ = json.Unmarshal([]byte(s.Fields), &f)
		d, e := time.Parse("2006-01-02", tmdbString(f, "last_air_date"))
		unknown = e != nil
		active = e == nil && d.After(now.AddDate(0, 0, -30))
	default:
		d, e := time.Parse("2006-01-02", s.ReleaseDate)
		unknown = e != nil
		active = e == nil && d.After(now.AddDate(0, 0, -90))
	}
	if unknown {
		return 7 * 24 * time.Hour
	}

	if scope == "detail" || scope == "credits" {
		if active {
			return 7 * 24 * time.Hour
		}
		return 90 * 24 * time.Hour
	}
	if active {
		return 24 * time.Hour
	}
	if s.Kind == "season" && s.ReleaseDate == "" {
		return 7 * 24 * time.Hour
	}
	return 30 * 24 * time.Hour
}
