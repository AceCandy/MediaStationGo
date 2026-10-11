package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

var ErrDoubanSubjectMissing = errors.New("cached douban subject not found")
var ErrDoubanSubjectReadOnly = errors.New("cached douban subject only supports reading")

// DoubanSubjectDetail 展示共享资料及原始响应，不投影本地影片字段。
type DoubanSubjectDetail struct {
	repository.DoubanSubjectSummary
	OriginalName       string              `json:"original_name"`
	Overview           string              `json:"overview"`
	ReleaseDate        string              `json:"release_date"`
	Genres             []string            `json:"genres"`
	Languages          []string            `json:"languages"`
	Countries          []string            `json:"countries"`
	Directors          []string            `json:"directors"`
	Writers            []string            `json:"writers"`
	Actors             []string            `json:"actors"`
	Aliases            []string            `json:"aliases"`
	Durations          []string            `json:"durations"`
	ReleaseDates       []string            `json:"release_dates"`
	IMDbID             string              `json:"imdb_id"`
	RatingCount        int                 `json:"rating_count"`
	RatingDistribution []DoubanRatingShare `json:"rating_distribution"`
	Credits            []MediaCredit       `json:"credits"`
	Awards             []DoubanAward       `json:"awards"`
	Honors             []DoubanHonor       `json:"honors"`
	RawJSON            json.RawMessage     `json:"raw_json,omitempty"`
}

// DoubanSubjectRefreshResult 同时报告本次降级状态和实际保留的资料。
type DoubanSubjectRefreshResult struct {
	Subject               *DoubanSubjectDetail `json:"subject"`
	Degraded              bool                 `json:"degraded"`
	SupplementUnavailable bool                 `json:"supplement_unavailable"`
}

func (d *DoubanProvider) ListSubjects(ctx context.Context, keyword, kind string, page, pageSize int) ([]repository.DoubanSubjectSummary, int64, error) {
	rows, total, err := d.dataRepo.Douban.ListSubjects(ctx, keyword, kind, page, pageSize)
	for i := range rows {
		rows[i].PosterURL = doubanSubjectPosterURL(rows[i].ID, rows[i].HasPayload)
	}
	return rows, total, err
}

func (d *DoubanProvider) SubjectDetail(ctx context.Context, id string) (*DoubanSubjectDetail, error) {
	subject, err := d.dataRepo.Douban.FindSubjectByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if subject == nil {
		return nil, ErrDoubanSubjectMissing
	}
	detail := &DoubanSubjectDetail{DoubanSubjectSummary: repository.DoubanSubjectSummary{
		ID: subject.ID, DoubanID: subject.DoubanID, EntityKind: subject.EntityKind,
		Rating: subject.Rating, Quality: subject.Quality, HasPayload: subject.Payload != nil,
		FetchedAt: subject.FetchedAt, RichFetchedAt: subject.RichFetchedAt, LastAttemptAt: subject.LastAttemptAt, NextRetryAt: subject.NextRetryAt,
		PosterURL: doubanSubjectPosterURL(subject.ID, subject.Payload != nil),
	}}
	if subject.Payload != nil {
		detail.RawJSON = json.RawMessage(*subject.Payload)
		match, err := doubanMatchFromRawJSON(subject.DoubanID, detail.RawJSON)
		if err == nil {
			detail.Title, detail.OriginalName, detail.Overview = match.Title, match.OriginalName, match.Overview
			detail.ReleaseDate = match.ReleaseDate
			if match.Year > 0 {
				detail.Year = strconv.Itoa(match.Year)
			}
			detail.Genres, detail.Languages, detail.Countries = match.Genres, match.Languages, match.Countries
		}
		populateDoubanRichDetail(detail, subject)
	}
	return detail, nil
}

// RefreshSubject 只更新现存共享条目，复用强制获取、限速和资料保护，不进行本地补齐。
func (d *DoubanProvider) RefreshSubject(ctx context.Context, id string) (*DoubanSubjectRefreshResult, error) {
	return d.refreshSubject(ctx, id, true)
}

// RefreshSubjectBase 强制刷新单条基础资料，不请求降级接口或网页补充。
func (d *DoubanProvider) RefreshSubjectBase(ctx context.Context, id string) (*DoubanSubjectRefreshResult, error) {
	return d.refreshSubject(ctx, id, false)
}

func (d *DoubanProvider) refreshSubject(ctx context.Context, id string, full bool) (*DoubanSubjectRefreshResult, error) {
	subject, err := d.SubjectDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if subject.EntityKind != model.MetadataKindMovie && subject.EntityKind != model.MetadataKindSeries {
		return nil, ErrDoubanSubjectReadOnly
	}
	degraded := false
	if full {
		_, degraded, err = d.GetEnrichmentMatchByID(WithDoubanFresh(ctx), subject.DoubanID, subject.EntityKind)
	} else {
		_, err = d.refreshBaseDetail(WithDoubanFresh(ctx), subject.DoubanID, subject.EntityKind)
	}
	if err != nil {
		return nil, err
	}
	available := false
	if full && !degraded {
		available, err = d.refreshRichSubject(ctx, subject.DoubanID, subject.EntityKind, true)
		if err != nil {
			return nil, err
		}
	}
	subject, err = d.SubjectDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	return &DoubanSubjectRefreshResult{Subject: subject, Degraded: degraded, SupplementUnavailable: full && !degraded && !available}, nil
}
